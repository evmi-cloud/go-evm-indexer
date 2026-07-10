package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// pluginCacheDir is where source repos are cloned and .so files are built.
var pluginCacheDir = filepath.Join(os.TempDir(), "evmi-plugins")

// ImportConfigPlugins imports the plugins declared in the server config as Plugin
// rows on startup: each is created if a plugin with the same name does not exist,
// then installed (built) if not already installed. Intended for git-hosted
// plugins that should be available out of the box.
func ImportConfigPlugins(db *evmi_database.EvmiDatabase, plugins []types.ConfigPlugin, logger zerolog.Logger) {
	for _, cp := range plugins {
		if cp.Name == "" || cp.GitUrl == "" {
			logger.Warn().Msg("skipping config plugin without a name or gitUrl")
			continue
		}

		var existing evmi_database.Plugin
		err := db.Conn.Where("name = ?", cp.Name).First(&existing).Error
		if err == nil {
			if existing.Status != string(evmi_database.InstalledPluginStatus) {
				installConfigPlugin(db, existing.ID, cp.Name, logger)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Error().Msg("import config plugins: " + err.Error())
			continue
		}

		plugin := evmi_database.Plugin{
			Name:         cp.Name,
			Description:  cp.Description,
			GitUrl:       cp.GitUrl,
			RelativePath: cp.RelativePath,
			Status:       string(evmi_database.NotInstalledPluginStatus),
		}
		if err := db.Conn.Create(&plugin).Error; err != nil {
			logger.Error().Str("plugin", cp.Name).Msg("config plugin create failed: " + err.Error())
			continue
		}
		logger.Info().Str("plugin", cp.Name).Msg("imported plugin from config")
		installConfigPlugin(db, plugin.ID, cp.Name, logger)
	}
}

func installConfigPlugin(db *evmi_database.EvmiDatabase, id uint, name string, logger zerolog.Logger) {
	if err := InstallPlugin(db, id, logger); err != nil {
		logger.Error().Str("plugin", name).Msg("config plugin install failed: " + err.Error())
	}
}

// VerifyPlugins is run at startup to make sure every installed plugin's shared
// object is still on disk (the build cache is typically ephemeral across
// restarts / container recreations). For a plugin whose .so is missing:
//   - if it has a git source, it is rebuilt (reinstalled);
//   - otherwise it cannot be rebuilt, so it is marked FAILED.
//
// Plugins that were never installed (NOT_INSTALLED) or already FAILED are left
// untouched.
func VerifyPlugins(db *evmi_database.EvmiDatabase, logger zerolog.Logger) {
	var plugins []evmi_database.Plugin
	if err := db.Conn.Find(&plugins).Error; err != nil {
		logger.Error().Msg("verify plugins: " + err.Error())
		return
	}

	for _, p := range plugins {
		// Only plugins that are supposed to be usable (INSTALLED, or INSTALLING
		// left stale by a crash during a previous install).
		if p.Status != string(evmi_database.InstalledPluginStatus) &&
			p.Status != string(evmi_database.InstallingPluginStatus) {
			continue
		}

		if p.SoPath != "" && fileExists(p.SoPath) {
			continue // still present
		}

		fields := map[string]interface{}{"plugin": p.Name, "id": p.ID}
		if p.GitUrl != "" {
			logger.Warn().Fields(fields).Msg("plugin shared object missing; reinstalling from git source")
			if err := InstallPlugin(db, p.ID, logger); err != nil {
				logger.Error().Fields(fields).Msg("plugin reinstall failed: " + err.Error())
			}
			continue
		}

		logger.Warn().Fields(fields).Msg("plugin shared object missing and no git source; marking failed")
		db.Conn.Model(&evmi_database.Plugin{}).Where("id = ?", p.ID).Updates(map[string]interface{}{
			"status":  string(evmi_database.FailedPluginStatus),
			"so_path": "",
			"error":   "shared object missing on startup and no git source to rebuild",
		})
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// InstallPlugin resolves and compiles a plugin's source into a shared object,
// recording the outcome (Status, SoPath, Error) on the Plugin row. It is the
// single place plugin building happens; exporters only load already-installed
// plugins.
func InstallPlugin(db *evmi_database.EvmiDatabase, pluginID uint, logger zerolog.Logger) error {
	var p evmi_database.Plugin
	if err := db.Conn.First(&p, pluginID).Error; err != nil {
		return err
	}

	db.Conn.Model(&p).Updates(map[string]interface{}{
		"status": string(evmi_database.InstallingPluginStatus),
		"error":  "",
	})

	soPath, err := buildPluginSharedObject(p, logger)
	if err != nil {
		db.Conn.Model(&p).Updates(map[string]interface{}{
			"status": string(evmi_database.FailedPluginStatus),
			"error":  err.Error(),
		})
		return err
	}

	db.Conn.Model(&p).Updates(map[string]interface{}{
		"status":        string(evmi_database.InstalledPluginStatus),
		"so_path":       soPath,
		"config_schema": extractConfigSchema(soPath, logger),
		"error":         "",
	})
	return nil
}

// extractConfigSchema loads the built plugin and, if it implements Configurable,
// returns its declared config schema as JSON. Returns nil (no schema) otherwise.
func extractConfigSchema(soPath string, logger zerolog.Logger) datatypes.JSON {
	instance, err := openPlugin(soPath)
	if err != nil {
		logger.Warn().Str("so", soPath).Msg("could not open plugin to read config schema: " + err.Error())
		return nil
	}
	configurable, ok := instance.(pluginsdk.Configurable)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(configurable.ConfigSchema())
	if err != nil {
		return nil
	}
	return datatypes.JSON(raw)
}

// buildPluginSharedObject resolves the plugin source to a .so path.
//
// Resolution order:
//   - LocalPath ending in ".so": used directly (prebuilt).
//   - GitUrl set (any git repo): cloned into the cache, then built.
//   - LocalPath (a directory): used as the module root, then built.
//
// The package built is RelativePath, relative to the module root.
func buildPluginSharedObject(p evmi_database.Plugin, logger zerolog.Logger) (string, error) {
	if strings.HasSuffix(p.LocalPath, ".so") {
		if _, err := os.Stat(p.LocalPath); err != nil {
			return "", fmt.Errorf("plugin .so not found at %s: %w", p.LocalPath, err)
		}
		return p.LocalPath, nil
	}

	var moduleRoot string
	switch {
	case p.GitUrl != "":
		root, err := cloneRepo(p.GitUrl, p.ID, logger)
		if err != nil {
			return "", err
		}
		moduleRoot = root
	case p.LocalPath != "":
		moduleRoot = p.LocalPath
	default:
		return "", errors.New("plugin has no source: set LocalPath or GitUrl")
	}

	outPath := filepath.Join(pluginCacheDir, fmt.Sprintf("plugin-%d.so", p.ID))
	if err := buildPlugin(moduleRoot, p.RelativePath, outPath, logger); err != nil {
		return "", err
	}
	return outPath, nil
}

// loadInstalledPlugin opens the compiled shared object of an installed plugin and
// instantiates its exporter.
func loadInstalledPlugin(db *evmi_database.EvmiDatabase, pluginID uint) (pluginsdk.Exporter, error) {
	if pluginID == 0 {
		return nil, errors.New("exporter has no plugin assigned")
	}

	var p evmi_database.Plugin
	if err := db.Conn.First(&p, pluginID).Error; err != nil {
		return nil, err
	}
	if p.Status != string(evmi_database.InstalledPluginStatus) || p.SoPath == "" {
		return nil, fmt.Errorf("plugin %q (id %d) is not installed", p.Name, pluginID)
	}
	return openPlugin(p.SoPath)
}

// cloneRepo shallow-clones url into a per-plugin cache directory. If the
// directory already exists it is reused as-is (no pull) to keep builds
// reproducible for a given revision.
func cloneRepo(url string, pluginID uint, logger zerolog.Logger) (string, error) {
	dest := filepath.Join(pluginCacheDir, fmt.Sprintf("src-%d", pluginID))
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(pluginCacheDir, 0o755); err != nil {
		return "", err
	}

	logger.Info().Str("url", url).Str("dest", dest).Msg("cloning plugin repo")
	cmd := exec.Command("git", "clone", "--depth", "1", url, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone failed: %v: %s", err, string(out))
	}
	return dest, nil
}

// buildPlugin compiles the package at relativePath (relative to moduleRoot) into
// a Go plugin at outPath using the host toolchain. The toolchain and module
// dependency versions MUST match the ones the EVMI server was built with, or
// plugin.Open will reject the resulting .so.
func buildPlugin(moduleRoot string, relativePath string, outPath string, logger zerolog.Logger) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	pkg := "./" + filepath.Clean(relativePath)
	if relativePath == "" {
		pkg = "."
	}

	logger.Info().Str("moduleRoot", moduleRoot).Str("pkg", pkg).Str("out", outPath).Msg("building plugin")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outPath, pkg)
	cmd.Dir = moduleRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("plugin build failed: %v: %s", err, string(out))
	}
	return nil
}

// openPlugin opens a compiled plugin and instantiates its exporter via the
// exported New symbol.
func openPlugin(soPath string) (pluginsdk.Exporter, error) {
	p, err := plugin.Open(soPath)
	if err != nil {
		return nil, fmt.Errorf("plugin.Open(%s): %w", soPath, err)
	}

	sym, err := p.Lookup("New")
	if err != nil {
		return nil, fmt.Errorf("plugin %s does not export New(): %w", soPath, err)
	}

	factory, ok := sym.(func() pluginsdk.Exporter)
	if !ok {
		return nil, fmt.Errorf("plugin %s: New has wrong signature, expected func() exporter.Exporter", soPath)
	}

	instance := factory()
	if instance == nil {
		return nil, fmt.Errorf("plugin %s: New() returned nil", soPath)
	}
	return instance, nil
}

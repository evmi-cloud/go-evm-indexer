package exporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"sort"
	"strings"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ListGitRefs returns the branch and tag names of a remote git repository via
// `git ls-remote` (no clone). Branches and tags are returned sorted; annotated-tag
// peel entries (refs/tags/x^{}) are collapsed. It is read-only and bounded by a
// timeout so a slow/hostile remote can't hang the server.
func ListGitRefs(url string) (branches []string, tags []string, err error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil, errors.New("git url is required")
	}
	// Never let the url be parsed as a git flag.
	if strings.HasPrefix(url, "-") {
		return nil, nil, errors.New("invalid git url")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", "--tags", "--", url)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, errors.New("git ls-remote timed out")
		}
		return nil, nil, fmt.Errorf("git ls-remote failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	tagSet := map[string]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ref := fields[1]
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			branches = append(branches, strings.TrimPrefix(ref, "refs/heads/"))
		case strings.HasPrefix(ref, "refs/tags/"):
			name := strings.TrimSuffix(strings.TrimPrefix(ref, "refs/tags/"), "^{}")
			tagSet[name] = struct{}{}
		}
	}
	for name := range tagSet {
		tags = append(tags, name)
	}
	sort.Strings(branches)
	sort.Strings(tags)
	return branches, tags, nil
}

// Plugin storage paths (configurable via the server config, see Configure):
//   - buildBaseDir  holds an ephemeral per-plugin work dir where the git repo is
//     cloned and the .so is compiled: <buildBaseDir>/<pluginName>.
//   - installBaseDir is where the finished .so is copied so it survives the tmp
//     build dir being cleaned: <installBaseDir>/<pluginName>.so.
var (
	buildBaseDir   = filepath.Join(os.TempDir(), "evmi")
	installBaseDir = "/evmi/plugins"
)

// Configure overrides the plugin build/install base directories from the server
// config. Empty values keep the defaults. Call once at startup, before any
// install/verify.
func Configure(buildDir, installDir string) {
	if strings.TrimSpace(buildDir) != "" {
		buildBaseDir = buildDir
	}
	if strings.TrimSpace(installDir) != "" {
		installBaseDir = installDir
	}
}

// pluginSlug is a filesystem-safe, stable directory/file name for a plugin,
// derived from its name (falling back to its id).
func pluginSlug(p evmi_database.Plugin) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, strings.TrimSpace(p.Name))
	name = strings.Trim(name, "-.")
	if name == "" {
		name = fmt.Sprintf("plugin-%d", p.ID)
	}
	return name
}

// copyFile copies src to dst (creating dst's directory), used to move a freshly
// built .so out of the ephemeral build dir into the persistent install dir.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

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
			Name:        cp.Name,
			Description: cp.Description,
			GitUrl:      cp.GitUrl,
			GitRef:      cp.GitRef,
			Status:      string(evmi_database.NotInstalledPluginStatus),
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
// object is still on disk (the build dir is ephemeral, and the install dir may be
// too unless persisted). For a plugin whose .so is missing:
//   - if it has a git source, it is rebuilt (reinstalled);
//   - otherwise (a malformed row with no GitUrl) it is marked FAILED.
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

	// Idempotent: if the plugin is already installed and its .so is present on
	// disk, there is nothing to do. Changing the source (UpdatePlugin) resets
	// Status and SoPath, so this never blocks a legitimate rebuild.
	if p.Status == string(evmi_database.InstalledPluginStatus) && p.SoPath != "" && fileExists(p.SoPath) {
		logger.Info().Str("plugin", p.Name).Str("so", p.SoPath).Msg("plugin already installed; skipping build")
		return nil
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

// buildPluginSharedObject clones the plugin's git repository at GitRef, builds the
// repo root into a plugin, and copies the resulting .so to
// <installBaseDir>/<pluginName>.so (returned). Git is the only supported source,
// and the plugin's `main` package must live at the repo root.
func buildPluginSharedObject(p evmi_database.Plugin, logger zerolog.Logger) (string, error) {
	if strings.TrimSpace(p.GitUrl) == "" {
		return "", errors.New("plugin has no source: a git repository (GitUrl) is required")
	}

	slug := pluginSlug(p)
	// Ephemeral per-plugin work dir: /tmp/evmi/<pluginName>.
	buildDir := filepath.Join(buildBaseDir, slug)

	moduleRoot, err := cloneRepo(p.GitUrl, p.GitRef, buildDir, logger)
	if err != nil {
		return "", err
	}

	builtSo := filepath.Join(buildDir, "built.so")
	if err := buildPlugin(moduleRoot, builtSo, logger); err != nil {
		return "", err
	}

	// Copy the built .so into the persistent install dir so it survives the tmp
	// build dir being cleaned: /evmi/plugins/<pluginName>.so.
	installedSo := filepath.Join(installBaseDir, slug+".so")
	if err := copyFile(builtSo, installedSo); err != nil {
		return "", fmt.Errorf("copy plugin .so to %s: %w", installedSo, err)
	}
	logger.Info().Str("plugin", p.Name).Str("so", installedSo).Msg("plugin installed")
	return installedSo, nil
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

// cloneRepo shallow-clones url into dest, at the given ref (branch or tag; empty
// = the repo's default branch). Any existing clone at dest is removed first so a
// changed url/ref always takes effect (install is an explicit action, and
// VerifyPlugins only reaches here when the .so is already missing).
func cloneRepo(url string, ref string, dest string, logger zerolog.Logger) (string, error) {
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		// --branch accepts both branch names and tags.
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dest)

	logger.Info().Str("url", url).Str("ref", ref).Str("dest", dest).Msg("cloning plugin repo")
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone failed: %v: %s", err, string(out))
	}
	return dest, nil
}

// buildPlugin compiles the module at moduleRoot (the plugin repo root, which is
// the deterministic build target — the plugin's `main` package must live there)
// into a Go plugin at outPath. The toolchain and module dependency versions MUST
// match the ones the EVMI server was built with, or plugin.Open rejects the .so.
func buildPlugin(moduleRoot string, outPath string, logger zerolog.Logger) error {
	// Ensure the output directory exists (the .so is written here).
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	logger.Info().Str("moduleRoot", moduleRoot).Str("out", outPath).Msg("building plugin")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outPath, ".")
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

package exporter

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
)

// pluginCacheDir is where source repos are cloned and .so files are built.
var pluginCacheDir = filepath.Join(os.TempDir(), "evmi-plugins")

// loadExporterPlugin resolves, builds if needed, and loads the plugin declared
// on the given exporter, returning an instantiated (but not yet Init'd) plugin.
//
// Resolution order:
//   - PluginLocalPath ending in ".so": loaded directly.
//   - PluginGithubUrl set: cloned into the cache, then built.
//   - PluginLocalPath (a directory): used as the module root, then built.
//
// The package built is PluginRelativePath, relative to the module root.
func loadExporterPlugin(exp evmi_database.EvmiExporter, logger zerolog.Logger) (pluginsdk.Exporter, error) {
	soPath, err := resolvePluginSharedObject(exp, logger)
	if err != nil {
		return nil, err
	}
	return openPlugin(soPath)
}

func resolvePluginSharedObject(exp evmi_database.EvmiExporter, logger zerolog.Logger) (string, error) {
	// Prebuilt shared object.
	if strings.HasSuffix(exp.PluginLocalPath, ".so") {
		if _, err := os.Stat(exp.PluginLocalPath); err != nil {
			return "", fmt.Errorf("plugin .so not found at %s: %w", exp.PluginLocalPath, err)
		}
		return exp.PluginLocalPath, nil
	}

	// Determine the module root to build from.
	var moduleRoot string
	switch {
	case exp.PluginGithubUrl != "":
		root, err := cloneRepo(exp.PluginGithubUrl, exp.ID, logger)
		if err != nil {
			return "", err
		}
		moduleRoot = root
	case exp.PluginLocalPath != "":
		moduleRoot = exp.PluginLocalPath
	default:
		return "", errors.New("exporter has no plugin source: set PluginLocalPath or PluginGithubUrl")
	}

	outPath := filepath.Join(pluginCacheDir, fmt.Sprintf("exporter-%d.so", exp.ID))
	if err := buildPlugin(moduleRoot, exp.PluginRelativePath, outPath, logger); err != nil {
		return "", err
	}
	return outPath, nil
}

// cloneRepo shallow-clones url into a per-exporter cache directory. If the
// directory already exists it is reused as-is (no pull) to keep builds
// reproducible for a given exporter revision.
func cloneRepo(url string, exporterID uint, logger zerolog.Logger) (string, error) {
	dest := filepath.Join(pluginCacheDir, fmt.Sprintf("src-%d", exporterID))
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.MkdirAll(pluginCacheDir, 0o755); err != nil {
		return "", err
	}

	logger.Info().Str("url", url).Str("dest", dest).Msg("cloning exporter plugin repo")
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

	logger.Info().Str("moduleRoot", moduleRoot).Str("pkg", pkg).Str("out", outPath).Msg("building exporter plugin")
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

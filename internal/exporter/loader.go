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
	"runtime"
	"sort"
	"strings"
	"time"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/types"
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
//     cloned and the plugin binary is compiled: <buildBaseDir>/<pluginName>.
//   - installBaseDir is where the finished binary is copied so it survives the
//     tmp build dir being cleaned: <installBaseDir>/<pluginName>.
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
// built plugin binary out of the ephemeral build dir into the persistent install
// dir. dst is created executable — it is launched as a subprocess.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// O_TRUNC (not just O_CREATE): reinstalling overwrites a shorter binary in
	// place, and 0o755 makes it executable for the plugin subprocess launch.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// An overwritten file keeps its old mode, so set it explicitly.
	return os.Chmod(dst, 0o755)
}

// ImportConfigPlugins imports the plugins declared in the server config as Plugin
// rows on startup: each is created if a plugin with the same name does not exist,
// then installed (built) if not already installed. Intended for git-hosted
// plugins that should be available out of the box.
//
// An entry with `catalog: true` names a repository rather than a single plugin:
// its catalog file is read and every plugin it declares is imported (see
// expandConfigPlugins), which is how one shared plugin repo is provisioned in a
// single config line.
func ImportConfigPlugins(db *evmi_database.EvmiDatabase, plugins []types.ConfigPlugin, logger zerolog.Logger) {
	for _, cp := range expandConfigPlugins(plugins, logger) {
		if cp.Name == "" || cp.GitUrl == "" {
			logger.Warn().Msg("skipping config plugin without a name or gitUrl")
			continue
		}
		subPath, err := ValidatePluginPath(cp.Path)
		if err != nil {
			logger.Error().Str("plugin", cp.Name).Msg("config plugin has an invalid path: " + err.Error())
			continue
		}

		var existing evmi_database.Plugin
		err = db.Conn.Where("name = ?", cp.Name).First(&existing).Error
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
			Path:        subPath,
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

// expandConfigPlugins resolves the `catalog: true` entries of the config plugin
// list into one entry per plugin the repository's catalog file declares (name,
// description and path come from the catalog; git url and ref from the config
// entry). Ordinary entries pass through untouched.
//
// A repo whose catalog cannot be read is skipped with an error rather than
// failing the boot: plugin import is best-effort, and the operator can still add
// the plugins by hand over the API.
func expandConfigPlugins(plugins []types.ConfigPlugin, logger zerolog.Logger) []types.ConfigPlugin {
	out := make([]types.ConfigPlugin, 0, len(plugins))
	for _, cp := range plugins {
		if !cp.Catalog {
			out = append(out, cp)
			continue
		}
		if cp.GitUrl == "" {
			logger.Warn().Msg("skipping config plugin catalog without a gitUrl")
			continue
		}

		entries, file, err := FetchPluginCatalog(cp.GitUrl, cp.GitRef, logger)
		if err != nil {
			logger.Error().Str("gitUrl", cp.GitUrl).Msg("reading plugin catalog: " + err.Error())
			continue
		}
		if len(entries) == 0 {
			logger.Warn().Str("gitUrl", cp.GitUrl).Msg("plugin catalog declares no plugin (is " + strings.Join(pluginCatalogFiles, " or ") + " present?)")
			continue
		}

		logger.Info().Str("gitUrl", cp.GitUrl).Str("catalog", file).Msg(fmt.Sprintf("importing %d plugins from catalog", len(entries)))
		for _, e := range entries {
			out = append(out, types.ConfigPlugin{
				Name:        e.Name,
				Description: e.Description,
				GitUrl:      cp.GitUrl,
				GitRef:      cp.GitRef,
				Path:        e.Path,
			})
		}
	}
	return out
}

func installConfigPlugin(db *evmi_database.EvmiDatabase, id uint, name string, logger zerolog.Logger) {
	if err := InstallPlugin(db, id, logger); err != nil {
		logger.Error().Str("plugin", name).Msg("config plugin install failed: " + err.Error())
	}
}

// VerifyPlugins is run at startup to make sure every installed plugin's binary is
// still on disk (the build dir is ephemeral, and the install dir may be too
// unless persisted). For a plugin whose binary is missing:
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

		if p.BinaryPath != "" && fileExists(p.BinaryPath) {
			continue // still present
		}

		fields := map[string]interface{}{"plugin": p.Name, "id": p.ID}
		if p.GitUrl != "" {
			logger.Warn().Fields(fields).Msg("plugin binary missing; reinstalling from git source")
			if err := InstallPlugin(db, p.ID, logger); err != nil {
				logger.Error().Fields(fields).Msg("plugin reinstall failed: " + err.Error())
			}
			continue
		}

		logger.Warn().Fields(fields).Msg("plugin binary missing and no git source; marking failed")
		db.Conn.Model(&evmi_database.Plugin{}).Where("id = ?", p.ID).Updates(map[string]interface{}{
			"status":      string(evmi_database.FailedPluginStatus),
			"binary_path": "",
			"error":       "plugin binary missing on startup and no git source to rebuild",
		})
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// InstallPlugin resolves and compiles a plugin's source into an executable,
// recording the outcome (Status, BinaryPath, Error) on the Plugin row. It is the
// single place plugin building happens; exporters only launch already-installed
// plugins.
func InstallPlugin(db *evmi_database.EvmiDatabase, pluginID uint, logger zerolog.Logger) error {
	var p evmi_database.Plugin
	if err := db.Conn.First(&p, pluginID).Error; err != nil {
		return err
	}

	// Idempotent: if the plugin is already installed and its binary is present on
	// disk, there is nothing to do. Changing the source (UpdatePlugin) resets
	// Status and BinaryPath, so this never blocks a legitimate rebuild.
	if p.Status == string(evmi_database.InstalledPluginStatus) && p.BinaryPath != "" && fileExists(p.BinaryPath) {
		// Backfill a config schema the row does not have yet: a plugin installed
		// before schema extraction existed (or before it started declaring
		// Configurable) keeps an empty column forever otherwise, since the skip
		// below never reaches extractConfigSchema -- and the exporter form then
		// stays a raw JSON box instead of a typed one. Cheap: it only probes when
		// the schema is actually missing.
		if len(p.ConfigSchema) == 0 {
			if schema := extractConfigSchema(p.BinaryPath, p.Name, logger); schema != nil {
				db.Conn.Model(&p).Update("config_schema", schema)
				logger.Info().Str("plugin", p.Name).Msg("backfilled plugin config schema")
			}
		}
		logger.Info().Str("plugin", p.Name).Str("binary", p.BinaryPath).Msg("plugin already installed; skipping build")
		return nil
	}

	db.Conn.Model(&p).Updates(map[string]interface{}{
		"status": string(evmi_database.InstallingPluginStatus),
		"error":  "",
	})

	binaryPath, err := buildPluginBinary(p, logger)
	if err != nil {
		db.Conn.Model(&p).Updates(map[string]interface{}{
			"status": string(evmi_database.FailedPluginStatus),
			"error":  err.Error(),
		})
		return err
	}

	db.Conn.Model(&p).Updates(map[string]interface{}{
		"status":        string(evmi_database.InstalledPluginStatus),
		"binary_path":   binaryPath,
		"config_schema": extractConfigSchema(binaryPath, p.Name, logger),
		"error":         "",
	})
	return nil
}

// extractConfigSchema runs the freshly built plugin once and, if it implements
// Configurable, returns its declared config schema as JSON. Returns nil (no
// schema) otherwise. The probe process is always killed before returning.
func extractConfigSchema(binaryPath string, name string, logger zerolog.Logger) datatypes.JSON {
	process, err := startPlugin(binaryPath, name, logger)
	if err != nil {
		logger.Warn().Str("binary", binaryPath).Msg("could not start plugin to read config schema: " + err.Error())
		return nil
	}
	defer process.Kill()

	fields, declared, err := process.ConfigSchema()
	if err != nil {
		logger.Warn().Str("binary", binaryPath).Msg("could not read plugin config schema: " + err.Error())
		return nil
	}
	if !declared {
		return nil
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil
	}
	return datatypes.JSON(raw)
}

// buildPluginBinary clones the plugin's git repository at GitRef, builds the
// package at Path (the repo root when empty) into an executable, and copies it to
// <installBaseDir>/<pluginName> (returned). Git is the only supported source.
func buildPluginBinary(p evmi_database.Plugin, logger zerolog.Logger) (string, error) {
	if strings.TrimSpace(p.GitUrl) == "" {
		return "", errors.New("plugin has no source: a git repository (GitUrl) is required")
	}
	// Re-validated here (not just at the API edge) because this path also runs for
	// rows written by the config importer and by older releases.
	subPath, err := ValidatePluginPath(p.Path)
	if err != nil {
		return "", err
	}

	slug := pluginSlug(p)
	// Ephemeral per-plugin work dir: /tmp/evmi/<pluginName>.
	buildDir := filepath.Join(buildBaseDir, slug)

	repoRoot, err := cloneRepo(p.GitUrl, p.GitRef, buildDir, logger)
	if err != nil {
		return "", err
	}

	// The build target is the plugin's own directory inside the clone: `go build`
	// resolves the enclosing module from there, so this works both for a monorepo
	// with a single go.mod at the root and for a repo whose plugins each carry
	// their own go.mod.
	pluginDir := repoRoot
	if subPath != "" {
		pluginDir = filepath.Join(repoRoot, filepath.FromSlash(subPath))
		info, err := os.Stat(pluginDir)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("plugin path %q not found in repository %s", subPath, p.GitUrl)
		}
	}

	built := filepath.Join(buildDir, "built"+exeSuffix())
	if err := buildPlugin(pluginDir, built, logger); err != nil {
		return "", err
	}

	// Copy the built binary into the persistent install dir so it survives the tmp
	// build dir being cleaned: /evmi/plugins/<pluginName>.
	installed := filepath.Join(installBaseDir, slug+exeSuffix())
	if err := copyFile(built, installed); err != nil {
		return "", fmt.Errorf("copy plugin binary to %s: %w", installed, err)
	}
	logger.Info().Str("plugin", p.Name).Str("binary", installed).Msg("plugin installed")
	return installed, nil
}

// exeSuffix is the executable extension for the host OS.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// launchInstalledPlugin starts the subprocess of an installed plugin. The caller
// owns the returned process and MUST Kill it.
func launchInstalledPlugin(db *evmi_database.EvmiDatabase, pluginID uint, logger zerolog.Logger) (*pluginProcess, error) {
	if pluginID == 0 {
		return nil, errors.New("exporter has no plugin assigned")
	}

	var p evmi_database.Plugin
	if err := db.Conn.First(&p, pluginID).Error; err != nil {
		return nil, err
	}
	if p.Status != string(evmi_database.InstalledPluginStatus) || p.BinaryPath == "" {
		return nil, fmt.Errorf("plugin %q (id %d): %w", p.Name, pluginID, errPluginNotInstalled)
	}
	return startPlugin(p.BinaryPath, p.Name, logger)
}

// cloneRepo clones url into dest, at the given ref (branch, tag or full commit
// id; empty = the repo's default branch). Branches and tags are shallow-cloned;
// a commit id costs the full history (see the clone args below). Any existing clone at dest is removed first so a
// changed url/ref always takes effect (install is an explicit action, and
// VerifyPlugins only reaches here when the binary is already missing).
func cloneRepo(url string, ref string, dest string, logger zerolog.Logger) (string, error) {
	return cloneRepoContext(context.Background(), url, ref, dest, logger)
}

// cloneRepoContext is cloneRepo bounded by a context — used by the catalog fetch,
// which is reachable from the API and so must not let a slow or unreachable
// remote pin a goroutine forever.
func cloneRepoContext(ctx context.Context, url string, ref string, dest string, logger zerolog.Logger) (string, error) {
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	args := []string{"clone", "--depth", "1"}
	switch {
	case isCommitID(ref):
		// A commit id cannot go through --branch (git resolves only branches and
		// tags there) and cannot be fetched shallowly from every server: clone the
		// history and check the commit out afterwards. Plugin repos are small, and
		// a commit is the only truly immutable pin — a tag can be moved.
		args = []string{"clone"}
	case ref != "":
		// --branch accepts both branch names and tags.
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dest)

	logger.Info().Str("url", url).Str("ref", ref).Str("dest", dest).Msg("cloning plugin repo")
	cmd := exec.CommandContext(ctx, "git", args...)
	// There is no terminal to answer git's credential prompt on a server: without
	// this a private repo hangs instead of failing.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", errors.New("git clone timed out")
		}
		return "", fmt.Errorf("git clone failed: %v: %s", err, string(out))
	}

	if isCommitID(ref) {
		checkout := exec.CommandContext(ctx, "git", "-C", dest, "checkout", "--quiet", ref)
		checkout.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := checkout.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git checkout %s failed: %v: %s", ref, err, string(out))
		}
	}
	return dest, nil
}

// isCommitID reports whether ref is a full 40-hex-character commit id. Only the
// full form is treated as a commit — a short prefix could collide with a branch
// or tag name, and a pin should not be ambiguous anyway.
func isCommitID(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	for _, r := range ref {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// buildPlugin compiles the package in pluginDir (the clone root, or the
// subdirectory named by Plugin.Path) into an ordinary executable at outPath. The
// build runs *in* that directory, so the go tool resolves whichever module
// encloses it — the plugin's `main` package must live there.
//
// A plain `go build`: the plugin runs as a separate process and speaks gRPC, so
// its Go toolchain and dependency versions are its own business.
func buildPlugin(pluginDir string, outPath string, logger zerolog.Logger) error {
	// Ensure the output directory exists (the binary is written here).
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	logger.Info().Str("pluginDir", pluginDir).Str("out", outPath).Msg("building plugin")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = pluginDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("plugin build failed: %v: %s", err, string(out))
	}
	return nil
}

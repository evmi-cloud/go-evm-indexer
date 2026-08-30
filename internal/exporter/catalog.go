package exporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// A single git repository can host several plugins, each in its own
// subdirectory (Plugin.Path). Such a "plugin repo" may describe what it holds in
// a catalog file at one of these paths (first one found wins), so operators can
// browse it instead of having to know the layout:
//
//	{
//	  "plugins": [
//	    { "name": "erc20-balances", "description": "…", "path": "exporters/erc20" },
//	    { "name": "webhook",        "description": "…", "path": "exporters/webhook" }
//	  ]
//	}
//
// A bare JSON array of the same entries is accepted too.
var pluginCatalogFiles = []string{
	"evmi-plugins.json",
	".evmi/plugins.json",
}

// PluginCatalogEntry is one plugin declared by a repository's catalog file. The
// fields mirror the Plugin row a user would otherwise fill in by hand.
type PluginCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Path is the subdirectory of the repository holding this plugin's `main`
	// package, relative to the repo root (empty = the root).
	Path string `json:"path"`
}

// pluginCatalog is the object form of a catalog file.
type pluginCatalog struct {
	Plugins []PluginCatalogEntry `json:"plugins"`
}

// ValidatePluginPath normalizes a plugin's in-repo package path: it must be
// relative and stay inside the clone. Returns the cleaned slash-separated form
// ("" for the repo root), so the stored value is canonical whatever the user
// typed ("./exporters/foo/", "exporters\foo").
func ValidatePluginPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", errors.New("plugin path must be relative to the repository root")
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "", nil
	}
	// path.Clean leaves a leading "../" in place, which is the only way out of the
	// clone once the path is relative.
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("plugin path must not escape the repository root")
	}
	return cleaned, nil
}

// parsePluginCatalog decodes a catalog file, accepting either the object form
// ({"plugins": [...]}) or a bare array of entries. Entries are validated: a name
// is required and the path must stay inside the repo.
func parsePluginCatalog(data []byte) ([]PluginCatalogEntry, error) {
	var entries []PluginCatalogEntry

	trimmed := strings.TrimLeft(string(data), " \t\r\n")
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("invalid plugin catalog: %w", err)
		}
	} else {
		var catalog pluginCatalog
		if err := json.Unmarshal(data, &catalog); err != nil {
			return nil, fmt.Errorf("invalid plugin catalog: %w", err)
		}
		entries = catalog.Plugins
	}

	out := make([]PluginCatalogEntry, 0, len(entries))
	for i, e := range entries {
		e.Name = strings.TrimSpace(e.Name)
		if e.Name == "" {
			return nil, fmt.Errorf("invalid plugin catalog: entry %d has no name", i)
		}
		cleaned, err := ValidatePluginPath(e.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid plugin catalog: entry %q: %w", e.Name, err)
		}
		e.Path = cleaned
		out = append(out, e)
	}
	return out, nil
}

// readPluginCatalog looks for a catalog file inside an already-cloned repo and
// parses it. It returns (nil, "", nil) when the repo declares no catalog — not
// an error, since a catalog is optional.
func readPluginCatalog(root string) ([]PluginCatalogEntry, string, error) {
	for _, name := range pluginCatalogFiles {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", err
		}
		entries, err := parsePluginCatalog(data)
		if err != nil {
			return nil, name, err
		}
		return entries, name, nil
	}
	return nil, "", nil
}

// FetchPluginCatalog clones a repository (shallow, into a temp dir that is always
// removed) and returns the plugins its catalog file declares, along with the
// catalog path that was found. A repo without a catalog yields no entries and no
// error — the caller then falls back to the operator naming a path by hand.
//
// This is read-only and side-effect free: nothing is built and no row is
// created, so it is safe to call from the UI while a user fills a form.
func FetchPluginCatalog(gitUrl string, gitRef string, logger zerolog.Logger) ([]PluginCatalogEntry, string, error) {
	gitUrl = strings.TrimSpace(gitUrl)
	if gitUrl == "" {
		return nil, "", errors.New("git url is required")
	}
	// Never let the url be parsed as a git flag.
	if strings.HasPrefix(gitUrl, "-") {
		return nil, "", errors.New("invalid git url")
	}

	tmp, err := os.MkdirTemp("", "evmi-catalog-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(tmp)

	// Bounded: this runs while a user waits on a form, and the remote is arbitrary.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dest := filepath.Join(tmp, "repo")
	if _, err := cloneRepoContext(ctx, gitUrl, strings.TrimSpace(gitRef), dest, logger); err != nil {
		return nil, "", err
	}
	return readPluginCatalog(dest)
}

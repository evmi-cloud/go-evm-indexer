package exporter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/rs/zerolog"
)

// A path is normalized to its cleaned, slash-separated form; anything that could
// point outside the clone is rejected.
func TestValidatePluginPath(t *testing.T) {
	ok := map[string]string{
		"":                     "",
		"   ":                  "",
		".":                    "",
		"./":                   "",
		"exporters/foo":        "exporters/foo",
		"exporters/foo/":       "exporters/foo",
		"./exporters/foo":      "exporters/foo",
		"exporters\\foo":       "exporters/foo",
		"exporters/bar/../foo": "exporters/foo",
	}
	for in, want := range ok {
		got, err := ValidatePluginPath(in)
		if err != nil {
			t.Errorf("ValidatePluginPath(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ValidatePluginPath(%q) = %q, want %q", in, got, want)
		}
	}

	for _, bad := range []string{"/etc", "../outside", "..", "foo/../../etc", "..\\outside"} {
		if got, err := ValidatePluginPath(bad); err == nil {
			t.Errorf("ValidatePluginPath(%q) = %q, want an error", bad, got)
		}
	}
}

// The object form and the bare-array form describe the same catalog, and paths
// are normalized on the way in.
func TestParsePluginCatalogBothForms(t *testing.T) {
	object := []byte(`{"plugins":[
		{"name":"erc20","description":"balances","path":"./exporters/erc20/"},
		{"name":"root"}
	]}`)
	array := []byte(`[
		{"name":"erc20","description":"balances","path":"./exporters/erc20/"},
		{"name":"root"}
	]`)

	for _, data := range [][]byte{object, array} {
		entries, err := parsePluginCatalog(data)
		if err != nil {
			t.Fatalf("parsePluginCatalog: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(entries))
		}
		if entries[0].Name != "erc20" || entries[0].Path != "exporters/erc20" || entries[0].Description != "balances" {
			t.Errorf("entry 0 = %+v", entries[0])
		}
		if entries[1].Name != "root" || entries[1].Path != "" {
			t.Errorf("entry 1 = %+v", entries[1])
		}
	}
}

// A malformed catalog is an error rather than a silently empty list: an operator
// pointing at a repo with a broken catalog must be told.
func TestParsePluginCatalogRejectsInvalid(t *testing.T) {
	for name, data := range map[string][]byte{
		"not json":      []byte(`nope`),
		"no name":       []byte(`{"plugins":[{"path":"exporters/x"}]}`),
		"escaping path": []byte(`{"plugins":[{"name":"x","path":"../../etc"}]}`),
	} {
		if _, err := parsePluginCatalog(data); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// readPluginCatalog finds the catalog wherever it is declared, and reports "no
// catalog" (not an error) for a repo that declares none.
func TestReadPluginCatalog(t *testing.T) {
	// Root file.
	root := t.TempDir()
	write(t, filepath.Join(root, "evmi-plugins.json"), `{"plugins":[{"name":"a","path":"a"}]}`)
	entries, file, err := readPluginCatalog(root)
	if err != nil || file != "evmi-plugins.json" || len(entries) != 1 {
		t.Fatalf("root catalog: entries=%v file=%q err=%v", entries, file, err)
	}

	// Dotted directory fallback.
	nested := t.TempDir()
	if err := os.MkdirAll(filepath.Join(nested, ".evmi"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(nested, ".evmi", "plugins.json"), `[{"name":"b","path":"b"}]`)
	entries, file, err = readPluginCatalog(nested)
	if err != nil || file != ".evmi/plugins.json" || len(entries) != 1 || entries[0].Name != "b" {
		t.Fatalf("nested catalog: entries=%v file=%q err=%v", entries, file, err)
	}

	// No catalog at all.
	entries, file, err = readPluginCatalog(t.TempDir())
	if err != nil || file != "" || entries != nil {
		t.Fatalf("no catalog: entries=%v file=%q err=%v", entries, file, err)
	}
}

func write(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newPluginRepo creates a local git repository laid out as a multi-plugin repo:
// one Go module at the root, two `main` packages in subdirectories, and a catalog
// declaring them. It returns the repo path, usable directly as a git url.
func newPluginRepo(t *testing.T, catalog string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("clones and builds a plugin; skipped with -short")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module evmi.test/plugins\n\ngo 1.21\n")
	for _, name := range []string{"one", "two"} {
		pkg := filepath.Join(dir, "exporters", name)
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		// Dependency-free so the build needs no module downloads.
		write(t, filepath.Join(pkg, "main.go"), "package main\n\nfunc main() {}\n")
	}
	if catalog != "" {
		write(t, filepath.Join(dir, "evmi-plugins.json"), catalog)
	}

	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

// FetchPluginCatalog clones the repo and reads its catalog — the path the UI and
// the config importer use to discover what a plugin repo holds.
func TestFetchPluginCatalog(t *testing.T) {
	repo := newPluginRepo(t, `{"plugins":[
		{"name":"one","description":"first","path":"exporters/one"},
		{"name":"two","path":"exporters/two"}
	]}`)

	entries, file, err := FetchPluginCatalog(repo, "", zerolog.Nop())
	if err != nil {
		t.Fatalf("FetchPluginCatalog: %v", err)
	}
	if file != "evmi-plugins.json" {
		t.Errorf("catalog file = %q", file)
	}
	if len(entries) != 2 || entries[0].Path != "exporters/one" || entries[1].Name != "two" {
		t.Fatalf("entries = %+v", entries)
	}

	// A repo without a catalog is not an error — the path is then given by hand.
	entries, file, err = FetchPluginCatalog(newPluginRepo(t, ""), "", zerolog.Nop())
	if err != nil || file != "" || len(entries) != 0 {
		t.Fatalf("uncatalogued repo: entries=%v file=%q err=%v", entries, file, err)
	}
}

// Two plugins of the same repository build independently from their own
// subdirectory, and a path that isn't in the repo fails with a clear error.
func TestBuildPluginBinaryFromSubPath(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	repo := newPluginRepo(t, "")

	prevBuild, prevInstall := buildBaseDir, installBaseDir
	buildBaseDir, installBaseDir = filepath.Join(t.TempDir(), "build"), filepath.Join(t.TempDir(), "install")
	defer func() { buildBaseDir, installBaseDir = prevBuild, prevInstall }()

	for _, name := range []string{"one", "two"} {
		p := evmi_database.Plugin{Name: name, GitUrl: repo, Path: "exporters/" + name}
		binary, err := buildPluginBinary(p, zerolog.Nop())
		if err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		if !fileExists(binary) {
			t.Fatalf("build %s: %q does not exist", name, binary)
		}
		if filepath.Base(binary) != name+exeSuffix() {
			t.Errorf("build %s: installed as %q", name, binary)
		}
	}

	_, err := buildPluginBinary(evmi_database.Plugin{Name: "missing", GitUrl: repo, Path: "exporters/nope"}, zerolog.Nop())
	if err == nil || !strings.Contains(err.Error(), "not found in repository") {
		t.Errorf("unknown path error = %v, want a 'not found in repository' error", err)
	}
}

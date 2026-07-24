package exporter

import (
	"os"
	"path/filepath"
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
)

func TestPluginSlug(t *testing.T) {
	mk := func(name string, id uint) evmi_database.Plugin {
		p := evmi_database.Plugin{Name: name}
		p.ID = id
		return p
	}
	cases := []struct {
		p    evmi_database.Plugin
		want string
	}{
		{mk("clear-defi", 1), "clear-defi"},
		{mk("Clear DeFi", 1), "Clear-DeFi"},
		{mk("a/b c", 1), "a-b-c"},
		{mk("--x--", 1), "x"},
		{mk("", 5), "plugin-5"},
		{mk("...", 7), "plugin-7"},
	}
	for _, c := range cases {
		if got := pluginSlug(c.p); got != c.want {
			t.Errorf("pluginSlug(%q) = %q, want %q", c.p.Name, got, c.want)
		}
	}
}

func TestConfigure(t *testing.T) {
	origBuild, origInstall := buildBaseDir, installBaseDir
	defer func() { buildBaseDir, installBaseDir = origBuild, origInstall }()

	// Empty values keep the current (default) settings.
	Configure("", "")
	if buildBaseDir != origBuild || installBaseDir != origInstall {
		t.Fatalf("empty Configure changed dirs: %q %q", buildBaseDir, installBaseDir)
	}

	Configure("/tmp/evmi", "/evmi/plugins")
	if buildBaseDir != "/tmp/evmi" || installBaseDir != "/evmi/plugins" {
		t.Fatalf("Configure not applied: %q %q", buildBaseDir, installBaseDir)
	}
}

func TestCopyFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.so")
	if err := os.WriteFile(src, []byte("SO"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Destination dir does not exist yet — copyFile must create it.
	dst := filepath.Join(t.TempDir(), "nested", "plugins", "p.so")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "SO" {
		t.Fatalf("copied content = %q, err %v", b, err)
	}
}

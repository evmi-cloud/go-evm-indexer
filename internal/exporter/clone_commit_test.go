package exporter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestIsCommitID(t *testing.T) {
	cases := map[string]bool{
		"cb6160051c1c721d2a7a4ed22d476d7b111933af": true,
		"CB6160051C1C721D2A7A4ED22D476D7B111933AF": true,
		"cb61600": false, // short prefixes could collide with branch names
		"main":    false,
		"v1.0.0":  false,
		"":        false,
		"cb6160051c1c721d2a7a4ed22d476d7b111933ag": false, // not hex
	}
	for ref, want := range cases {
		if got := isCommitID(ref); got != want {
			t.Errorf("isCommitID(%q) = %v, want %v", ref, got, want)
		}
	}
}

// A full commit id must be clonable as a gitRef: `git clone --branch` only
// resolves branches and tags, so the commit path clones then checks out.
func TestCloneRepoByCommitID(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "first")
	pinned := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-q", "-am", "second")

	dest := filepath.Join(t.TempDir(), "clone")
	repoRoot, err := cloneRepo(dir, pinned, dest, zerolog.Nop())
	if err != nil {
		t.Fatalf("cloneRepo by commit id: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(repoRoot, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// The clone must sit at the pinned commit, not at the branch head.
	if string(got) != "one" {
		t.Fatalf("checked-out content = %q, want %q", got, "one")
	}
}

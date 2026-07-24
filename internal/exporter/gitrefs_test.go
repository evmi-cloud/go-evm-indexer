package exporter

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListGitRefs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "init")
	git("branch", "dev")
	git("tag", "v1.0.0")
	git("tag", "-a", "v2.0.0", "-m", "annotated") // annotated tag → also emits a ^{} peel line

	branches, tags, err := ListGitRefs(dir)
	if err != nil {
		t.Fatalf("ListGitRefs: %v", err)
	}

	if !contains(branches, "dev") {
		t.Errorf("branches %v missing dev", branches)
	}
	if len(branches) < 2 { // dev + the default branch (main/master)
		t.Errorf("expected >= 2 branches, got %v", branches)
	}
	if !contains(tags, "v1.0.0") || !contains(tags, "v2.0.0") {
		t.Errorf("tags %v missing v1.0.0/v2.0.0", tags)
	}
	// The annotated tag's ^{} peel entry must be collapsed, not duplicated.
	if countOf(tags, "v2.0.0") != 1 {
		t.Errorf("annotated tag not de-duplicated: %v", tags)
	}
}

func TestListGitRefsInvalid(t *testing.T) {
	if _, _, err := ListGitRefs(""); err == nil {
		t.Error("empty url should error")
	}
	if _, _, err := ListGitRefs("--upload-pack=evil"); err == nil {
		t.Error("flag-like url should be rejected")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func countOf(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}

package grpc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewWebUIHandler(t *testing.T) {
	// No build present -> nil handler (caller skips mounting).
	if h := newWebUIHandler(filepath.Join(t.TempDir(), "missing")); h != nil {
		t.Fatal("expected nil handler for a directory without index.html")
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html>root</html>")
	writeFile(t, filepath.Join(dir, "_next", "app.js"), "console.log(1)")
	writeFile(t, filepath.Join(dir, "dashboard", "index.html"), "<html>dashboard</html>")

	h := newWebUIHandler(dir)
	if h == nil {
		t.Fatal("expected a handler when index.html exists")
	}

	cases := []struct {
		path string
		want string
	}{
		{"/", "<html>root</html>"},                  // root index
		{"/_next/app.js", "console.log(1)"},         // real asset
		{"/dashboard/", "<html>dashboard</html>"},   // route dir with its own index
		{"/does/not/exist", "<html>root</html>"},    // SPA fallback to index
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", c.path, rec.Code)
			continue
		}
		if got := rec.Body.String(); got != c.want {
			t.Errorf("%s: body %q, want %q", c.path, got, c.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

package grpc

import (
	"net/http"
	"os"
	"path/filepath"
)

// webuiDir is the directory the built web UI is served from.
func webuiDir() string {
	if dir := os.Getenv("EVMI_WEBUI_DIR"); dir != "" {
		return dir
	}
	return "public"
}

// newWebUIHandler serves the statically-exported web UI from dir. Existing files
// are served directly; unknown paths fall back to index.html so client-side
// routing works. It returns nil when dir has no index.html, so the caller can
// skip mounting it (e.g. in a dev checkout with no build).
func newWebUIHandler(dir string) http.Handler {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return nil
	}

	fileServer := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean with a leading slash so ".." cannot escape dir.
		rel := filepath.Clean("/" + r.URL.Path)
		target := filepath.Join(dir, rel)

		if info, err := os.Stat(target); err == nil {
			// For a directory, only let the file server handle it when it has its
			// own index.html; otherwise fall through to the SPA index.
			if info.IsDir() {
				if _, err := os.Stat(filepath.Join(target, "index.html")); err != nil {
					http.ServeFile(w, r, index)
					return
				}
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// Next static export writes "<route>/index.html"; also try "<route>.html".
		if _, err := os.Stat(target + ".html"); err == nil {
			http.ServeFile(w, r, target+".html")
			return
		}

		http.ServeFile(w, r, index)
	})
}

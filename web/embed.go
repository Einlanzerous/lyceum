// Package web embeds the built Vite SPA (the dist/ directory) so the Go binary
// can serve the reader same-origin with the JSON API — the production model
// chosen in LYCM-207. This eliminates any CORS surface: in dev the Vite proxy
// gives the same single-origin behaviour (LYCM-201), and in prod this handler
// does.
//
// The embed pattern always resolves because a placeholder dist/.gitkeep is
// checked in (the `all:` prefix includes dotfiles); a real bundle is produced
// by `make build-web` (npm run build) before `go build`. When only the
// placeholder is present, Handler serves a clear "not built" message rather
// than a stale page.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// dist returns the embedded bundle rooted at dist/.
func dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Only possible if the embed directive and this path disagree, which is
		// a compile-time-adjacent programming error, not a runtime condition.
		panic("web: dist subtree missing: " + err.Error())
	}
	return sub
}

// Handler serves the embedded SPA. Existing files (index.html, hashed assets)
// are served directly; any other path falls back to index.html so client-side
// routing handles deep links like /reader/1 on reload. It is meant to be
// registered as the catch-all "/" route, with the specific API routes taking
// precedence via the ServeMux.
func Handler() http.Handler {
	root := dist()
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "." || clean == "" {
			serveIndex(w, r, root)
			return
		}
		// Serve the file if it exists in the bundle; otherwise SPA-fallback.
		if f, err := root.Open(clean); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, root)
	})
}

// serveIndex writes index.html as the SPA entry document. It is marked
// no-cache: the document is tiny and must always reflect the current asset
// hashes, while the hashed assets themselves are safely cacheable.
func serveIndex(w http.ResponseWriter, r *http.Request, root fs.FS) {
	data, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "web UI not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

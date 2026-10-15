// Package web embeds the frontend build output and serves the SPA static files.
//
// The frontend is built with Bun (frontend/scripts/build.ts) and output to
// internal/web/dist, then embedded into a single binary via go:embed.
//
// When the frontend is not built, dist contains only .gitkeep, so it falls
// back to placeholder/index.html to guarantee go build compiles in all cases
// (SPA routes point to an "unbuilt" notice page).
//
// SPA fallback: any path that is not /api and not a static asset returns
// index.html, which is handled by frontend routing (Vue Router history mode).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

//go:embed placeholder/index.html
var placeholderHTML []byte

// Handler returns an http.Handler that serves the frontend static assets plus
// the SPA fallback.
func Handler() (http.Handler, error) {
	distRoot, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}

	indexHTML := placeholderHTML
	fileServer := http.FileServer(http.FS(distRoot))
	if data, err := fs.ReadFile(distRoot, "index.html"); err == nil {
		indexHTML = data
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			serveIndex(w, indexHTML)
			return
		}
		// Return a real file if it exists; otherwise fall back to index.html
		// (SPA routing).
		if f, err := distRoot.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, indexHTML)
	}), nil
}

// serveIndex returns the SPA entry HTML with content-type set explicitly.
func serveIndex(w http.ResponseWriter, indexHTML []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

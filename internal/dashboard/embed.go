// Package dashboard embeds and serves the built web dashboard assets.
package dashboard

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an HTTP handler that serves the embedded web dashboard,
// delegating API and system routes to the provided apiHandler.
func Handler(apiHandler http.Handler) http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("dashboard: failed to locate embedded dist directory: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	indexFile, err := sub.Open("index.html")
	var indexContent []byte
	if err == nil {
		indexContent, _ = io.ReadAll(indexFile)
		_ = indexFile.Close()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path

		// Pass through API, Conductor, WebSocket, and system routes
		if isAPIRoute(reqPath) {
			if apiHandler != nil {
				apiHandler.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}

		cleanPath := strings.TrimPrefix(path.Clean(reqPath), "/")
		if cleanPath == "" || cleanPath == "." {
			cleanPath = "index.html"
		}

		// Try opening the requested file in the embedded FS
		f, err := sub.Open(cleanPath)
		if err == nil {
			stat, err := f.Stat()
			_ = f.Close()
			if err == nil && !stat.IsDir() {
				// Defense-in-depth security headers
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("X-Frame-Options", "DENY")
				w.Header().Set("Referrer-Policy", "no-referrer")
				w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'")

				// Set caching headers
				if cleanPath == "index.html" {
					w.Header().Set("Cache-Control", "no-cache, must-revalidate")
				} else if strings.HasPrefix(cleanPath, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// If the path has an extension, it was an asset request that was not found
		if path.Ext(cleanPath) != "" {
			http.NotFound(w, r)
			return
		}

		// SPA fallback: non-API route without an extension serves index.html
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexContent)
	})
}

func isAPIRoute(p string) bool {
	return strings.HasPrefix(p, "/v2/") ||
		strings.HasPrefix(p, "/v1/") ||
		strings.HasPrefix(p, "/websocket/") ||
		strings.HasPrefix(p, "/internal/") ||
		strings.HasPrefix(p, "/schemas/") ||
		strings.HasPrefix(p, "/openapi") ||
		p == "/healthz" ||
		p == "/docs"
}

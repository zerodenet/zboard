package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func staticFallbackHandler(webDir string) http.Handler {
	webDir = filepath.Clean(webDir)
	fileServer := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Neither entry documents nor failures may outlive a deployment. In
		// particular, a cached missing chunk can strand an otherwise healthy UI.
		w.Header().Set("Cache-Control", "no-store")
		cleaned := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(cleaned, "/api/") || cleaned == "/api" || strings.HasPrefix(cleaned, "/media/") || cleaned == "/media" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		candidate := filepath.Join(webDir, strings.TrimPrefix(cleaned, "/"))
		if fileExists(candidate) {
			if strings.HasPrefix(cleaned, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=14400")
			}
		} else {
			if strings.HasPrefix(cleaned, "/assets/") || cleaned == "/assets" || isFrontendResource(cleaned) {
				http.NotFound(w, r)
				return
			}
			cleaned = "/"
		}
		request := r.Clone(r.Context())
		request.URL.Path = cleaned
		fileServer.ServeHTTP(w, request)
	})
}

func isFrontendResource(urlPath string) bool {
	switch strings.ToLower(path.Ext(urlPath)) {
	case ".js", ".mjs", ".css", ".map", ".json", ".ico", ".png", ".jpg", ".jpeg", ".svg", ".webp", ".woff", ".woff2":
		return true
	}
	return false
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}

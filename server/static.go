package server

import (
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) setupStaticFiles() {
	fs := http.FileServer(http.Dir("web/"))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only serve for non-API paths
		path := filepath.Join("web", filepath.Clean("/"+r.URL.Path))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// SPA fallback: serve index.html
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, "web/index.html")
			return
		}
		// Prevent stale JS/CSS after server upgrades
		if filepath.Ext(path) == ".js" || filepath.Ext(path) == ".css" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fs.ServeHTTP(w, r)
	})
}

package server

import (
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) setupStaticFiles() {
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only serve for non-API paths
		path := filepath.Join("web", filepath.Clean("/"+r.URL.Path))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// SPA fallback: serve index.html
			http.ServeFile(w, r, "web/index.html")
			return
		}
		http.FileServer(http.Dir("web/")).ServeHTTP(w, r)
	})
}

package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
)

func (s *Server) setupStaticFiles() {
	if s.webFS == nil {
		return
	}
	fileServer := http.FileServer(http.FS(s.webFS))

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "."
		}

		fi, err := fs.Stat(s.webFS, name)
		if err != nil || fi.IsDir() {
			// SPA fallback: any unknown path or directory serves index.html
			w.Header().Set("Cache-Control", "no-cache")
			s.serveIndex(w, r)
			return
		}

		if strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".css") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.webFS, "index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if s.prefix != "" {
		inject := []byte(`<script>window.XIRC_PREFIX='` + s.prefix + `';</script>`)
		data = bytes.Replace(data, []byte("</head>"), append(inject, []byte("</head>")...), 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

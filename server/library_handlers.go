package server

import (
	"encoding/json"
	"net/http"

	"github.com/RealDtx/maxwell-irc/library"
)

// GET /api/library — the current categories.yaml config.
// PUT /api/library — validate (regexes, templates, kinds, media_root exists),
// save, and swap the in-memory copy.
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	if s.library == nil {
		writeError(w, http.StatusInternalServerError, "library not initialized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg := s.library.Get()
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg library.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.library.Set(cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.library.Get())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /api/library/detect — a proposed config built from what's actually on
// disk under the current media_root. Not saved.
func (s *Server) handleLibraryDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.library == nil {
		writeError(w, http.StatusInternalServerError, "library not initialized")
		return
	}
	writeJSON(w, http.StatusOK, library.Detect(s.library.Get().MediaRoot))
}

// POST /api/library/preview {filenames: []string} — the category, parsed
// fields, and resolved path each filename would land in, without touching
// disk. Used by the settings "Try a file name" box and the downloads table's
// "Auto → …" label.
func (s *Server) handleLibraryPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.library == nil {
		writeError(w, http.StatusInternalServerError, "library not initialized")
		return
	}
	var req struct {
		Filenames []string `json:"filenames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := s.library.Get()
	results := make([]library.PreviewResult, 0, len(req.Filenames))
	for _, f := range req.Filenames {
		results = append(results, library.Preview(&cfg, f))
	}
	writeJSON(w, http.StatusOK, results)
}

// GET /api/library/kinds — {kind: [field names]}, so the UI can show
// path-template hints per kind.
func (s *Server) handleLibraryKinds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, library.KindFields())
}

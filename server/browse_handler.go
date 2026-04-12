package server

import (
	"net/http"
	"os"
	"path/filepath"
)

type browseEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

type browseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	Entries []browseEntry `json:"entries"`
}

// GET /api/browse?path=/srv/downloads
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	raw := r.URL.Query().Get("path")
	if raw == "" {
		raw = "/"
	}

	// Sanitise: clean and resolve symlinks to prevent traversal
	clean := filepath.Clean(raw)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "path not found")
		} else {
			writeError(w, http.StatusBadRequest, "invalid path")
		}
		return
	}

	entries, err := readDirEntries(resolved)
	if err != nil {
		if os.IsPermission(err) {
			writeError(w, http.StatusForbidden, "permission denied")
		} else {
			writeError(w, http.StatusInternalServerError, "could not read directory")
		}
		return
	}

	parent := filepath.Dir(resolved)
	if parent == resolved {
		parent = "" // at filesystem root
	}

	writeJSON(w, http.StatusOK, browseResponse{
		Path:    resolved,
		Parent:  parent,
		Entries: entries,
	})
}

func readDirEntries(path string) ([]browseEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}

	var entries []browseEntry
	for _, info := range infos {
		if info.Name() == "" || info.Name()[0] == '.' {
			continue // skip hidden
		}
		entries = append(entries, browseEntry{
			Name:  info.Name(),
			IsDir: info.IsDir(),
		})
	}
	return entries, nil
}

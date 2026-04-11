package server

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type fileEntry struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type filesResponse struct {
	Dir   string      `json:"dir"`
	Files []fileEntry `json:"files"`
}

// GET /api/files?dir=/srv/downloads
// Only serves directories configured as destination_dir in routing rules.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeError(w, http.StatusBadRequest, "dir required")
		return
	}

	// Security: only serve dirs that are configured routing destinations
	allowed, err := s.isAllowedDir(dir)
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "not a configured destination directory")
		return
	}

	clean := filepath.Clean(dir)
	f, err := os.Open(clean)
	if err != nil {
		if os.IsPermission(err) {
			writeError(w, http.StatusForbidden, "permission denied")
		} else {
			writeError(w, http.StatusNotFound, "directory not found")
		}
		return
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read directory")
		return
	}

	var files []fileEntry
	for _, info := range infos {
		if info.IsDir() {
			continue
		}
		files = append(files, fileEntry{
			Name:     info.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC(),
		})
	}
	if files == nil {
		files = []fileEntry{}
	}
	writeJSON(w, http.StatusOK, filesResponse{Dir: clean, Files: files})
}

func (s *Server) isAllowedDir(dir string) (bool, error) {
	rules, err := s.store.GetAllFileRoutingRules()
	if err != nil {
		return false, err
	}
	cleanDir := filepath.Clean(dir)
	for _, r := range rules {
		if filepath.Clean(r.DestinationDir) == cleanDir {
			return true, nil
		}
	}
	return false, nil
}

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RealDtx/maxwell-irc/config"
)

// SetupState carries wizard data from main() into the HTTP server.
// It is also used by the CLI wizard path (main.go calls ApplyMappings directly).
type SetupState struct {
	mu           sync.Mutex
	Required     bool
	BadDirs      []string
	MediaDir     string // original cfg.Storage.MediaDir
	DownloadsDir string // original cfg.Storage.DownloadsDir
	TempDir      string // original cfg.Storage.TempDir; follows DownloadsDir when inside it
	HomeDir      string // os.UserHomeDir() result
	ConfigPath   string // path to config.yaml for writing back
}

// Complete marks setup as done and clears bad dirs.
func (s *SetupState) Complete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Required = false
	s.BadDirs = nil
}

// Mapping is one old→new directory substitution.
type Mapping struct {
	OldDir string `json:"old_dir"`
	NewDir string `json:"new_dir"`
}

// DefaultSuggestions returns a suggested replacement path for each bad dir.
// The downloads dir gets ~/Downloads; the media dir gets ~/Videos.
func DefaultSuggestions(badDirs []string, downloadsDir, homeDir string) map[string]string {
	result := make(map[string]string, len(badDirs))
	for _, d := range badDirs {
		if d == downloadsDir {
			result[d] = filepath.Join(homeDir, "Downloads")
		} else {
			result[d] = filepath.Join(homeDir, "Videos")
		}
	}
	return result
}

// DirLabel names which storage dir this is, for the CLI wizard prompt.
func DirLabel(dir string, state *SetupState) string {
	if dir == state.DownloadsDir {
		return "downloads_dir"
	}
	return "media_dir"
}

// ApplyMappings rewrites config.yaml's media_dir/downloads_dir. The write is
// atomic (temp file + rename), so a failure leaves config.yaml untouched.
func ApplyMappings(mappings []Mapping, state *SetupState) error {
	newMediaDir := state.MediaDir
	newDownloadsDir := state.DownloadsDir
	for _, m := range mappings {
		if m.OldDir == state.MediaDir {
			newMediaDir = m.NewDir
		}
		if m.OldDir == state.DownloadsDir {
			newDownloadsDir = m.NewDir
		}
	}

	storage := map[string]string{"media_dir": newMediaDir, "downloads_dir": newDownloadsDir}
	// The default temp dir lives under downloads (/srv/downloads/.tmp); left
	// behind, it keeps downloads disabled after the remap.
	if rel, err := filepath.Rel(state.DownloadsDir, state.TempDir); err == nil && state.TempDir != "" && rel != ".." && !strings.HasPrefix(rel, "../") {
		storage["temp_dir"] = filepath.Join(newDownloadsDir, rel)
	}
	if err := config.SaveKeys(state.ConfigPath, map[string]any{"storage": storage}); err != nil {
		return err
	}

	state.Complete()
	return nil
}

// GET /api/setup/status
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.setup == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"required": false, "bad_dirs": nil})
		return
	}
	s.setup.mu.Lock()
	resp := map[string]interface{}{
		"required": s.setup.Required,
		"bad_dirs": s.setup.BadDirs,
	}
	s.setup.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/setup/defaults
func (s *Server) handleSetupDefaults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.setup == nil {
		writeJSON(w, http.StatusOK, map[string]string{"videos_dir": "", "downloads_dir": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"videos_dir":    filepath.Join(s.setup.HomeDir, "Videos"),
		"downloads_dir": filepath.Join(s.setup.HomeDir, "Downloads"),
	})
}

// POST /api/setup/complete
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.setup == nil {
		writeError(w, http.StatusBadRequest, "setup not required")
		return
	}
	var body struct {
		Mappings []Mapping `json:"mappings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Mappings) == 0 {
		writeError(w, http.StatusBadRequest, "mappings must not be empty")
		return
	}
	if err := ApplyMappings(body.Mappings, s.setup); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"restart_required": true})
}

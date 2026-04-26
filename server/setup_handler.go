package server

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

// SetupState carries wizard data from main() into the HTTP server.
// It is also used by the CLI wizard path (main.go calls ApplyMappings directly).
type SetupState struct {
	mu           sync.Mutex
	Required     bool
	BadDirs      []string
	MediaDir     string // original cfg.Storage.MediaDir
	DownloadsDir string // original cfg.Storage.DownloadsDir
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
// Dirs that serve the catch-all rule (*) get ~/Downloads; others get ~/Videos.
func DefaultSuggestions(badDirs []string, rules []db.FileRoutingRule, homeDir string) map[string]string {
	hasCatchAll := map[string]bool{}
	for _, r := range rules {
		if r.Pattern == "*" {
			hasCatchAll[r.DestinationDir] = true
		}
	}
	result := make(map[string]string, len(badDirs))
	for _, d := range badDirs {
		if hasCatchAll[d] {
			result[d] = filepath.Join(homeDir, "Downloads")
		} else {
			result[d] = filepath.Join(homeDir, "Videos")
		}
	}
	return result
}

// PatternsForDir returns all patterns whose destination_dir equals dir.
func PatternsForDir(rules []db.FileRoutingRule, dir string) []string {
	var out []string
	for _, r := range rules {
		if r.DestinationDir == dir {
			out = append(out, r.Pattern)
		}
	}
	return out
}

// ApplyMappings rewrites config.yaml and updates routing rules in the DB.
// Config write is done first atomically; if DB updates fail, config is best-effort rolled back.
func ApplyMappings(mappings []Mapping, store db.Store, state *SetupState) error {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		return err
	}

	// Compute new config values
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

	oldToNew := make(map[string]string, len(mappings))
	for _, m := range mappings {
		oldToNew[m.OldDir] = m.NewDir
	}

	// Persist config first (atomic rename; nothing changes on failure).
	if err := config.WriteStorageDirs(state.ConfigPath, newMediaDir, newDownloadsDir); err != nil {
		return err
	}

	// Update routing rules; on failure, best-effort roll back config.
	for i := range rules {
		if newDir, ok := oldToNew[rules[i].DestinationDir]; ok {
			rules[i].DestinationDir = newDir
			if dbErr := store.UpdateFileRoutingRule(&rules[i]); dbErr != nil {
				if rbErr := config.WriteStorageDirs(state.ConfigPath, state.MediaDir, state.DownloadsDir); rbErr != nil {
					log.Printf("warning: config rollback failed after DB error: %v", rbErr)
				}
				return dbErr
			}
		}
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
	if err := ApplyMappings(body.Mappings, s.store, s.setup); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"restart_required": true})
}

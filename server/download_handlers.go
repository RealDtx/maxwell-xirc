package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/routing"
)

func (s *Server) handleGetDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get("status")

	downloads, err := s.store.GetDownloads(status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if downloads == nil {
		downloads = []db.Download{}
	}
	writeJSON(w, http.StatusOK, downloads)
}

func (s *Server) handleRequestDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if c := s.capabilities().Downloads; !c.OK {
		writeError(w, http.StatusServiceUnavailable, "downloads disabled: "+c.Reason)
		return
	}

	var req struct {
		ServerID    int64  `json:"server_id"`
		Channel     string `json:"channel"`
		BotNick     string `json:"bot_nick"`
		PackNumber  int    `json:"pack_number"`
		Filename    string `json:"filename"`
		Filesize    int64  `json:"filesize"`
		StatsOnly   bool   `json:"stats_only"`
		AutoExtract *bool  `json:"auto_extract"`
		AutoSubdir  *bool  `json:"auto_subdir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ServerID == 0 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if irc.HasLineBreak(req.Channel, req.BotNick) {
		writeError(w, http.StatusBadRequest, "channel/bot_nick must not contain line breaks")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	autoExtract := true
	if req.AutoExtract != nil {
		autoExtract = *req.AutoExtract
	}
	// Default auto_subdir to the library's auto-organize setting rather than
	// always-on, so turning that off in Settings also changes new downloads.
	autoSubdir := true
	if s.library != nil {
		autoSubdir = s.library.Get().AutoOrganize
	}
	if req.AutoSubdir != nil {
		autoSubdir = *req.AutoSubdir
	}

	dl, err := s.engine.Queue().Add(req.ServerID, req.Channel, req.BotNick, req.PackNumber, req.Filename, req.Filesize, req.StatsOnly, autoExtract, autoSubdir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Dispatch via the queue pump so max_concurrent and the one-request-per-bot
	// rule are respected; the download stays "queued" until a slot is free.
	s.engine.TryDispatchQueued()

	writeJSON(w, http.StatusCreated, dl)
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DownloadID == 0 {
		writeError(w, http.StatusBadRequest, "download_id is required")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}
	s.engine.CancelTransfer(req.DownloadID) // interrupts active TCP transfer if running
	if err := s.engine.Queue().Cancel(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.engine.TryDispatchQueued() // cancelling may have freed a slot

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DownloadID == 0 {
		writeError(w, http.StatusBadRequest, "download_id is required")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}
	if err := s.engine.Queue().Retry(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.engine.TryDispatchQueued() // start the retried download if a slot is free

	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleMoveDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DownloadID == 0 {
		writeError(w, http.StatusBadRequest, "download_id is required")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	if err := s.engine.Queue().MoveToFront(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}

func (s *Server) handleDeleteDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}

	for _, id := range req.IDs {
		dl, err := s.store.GetDownload(id)
		if err != nil {
			log.Printf("delete: fetching download %d: %v", id, err)
			continue
		}
		if dl.DestinationPath != "" {
			partPath := dl.DestinationPath + ".part"
			if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
				log.Printf("delete: removing part file %s: %v", partPath, err)
			}
		}
	}

	n, err := s.store.DeleteDownloads(req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) handleClearDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}

	dls, dlsErr := s.store.GetDownloads(req.Status)
	if dlsErr != nil {
		log.Printf("clear: fetching downloads by status %q: %v", req.Status, dlsErr)
	} else {
		for _, dl := range dls {
			if dl.DestinationPath != "" {
				partPath := dl.DestinationPath + ".part"
				if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
					log.Printf("clear: removing part file %s: %v", partPath, err)
				}
			}
		}
	}

	n, err := s.store.DeleteDownloadsByStatus(req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) handleSetAutoExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DownloadID  int64 `json:"download_id"`
		AutoExtract bool  `json:"auto_extract"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DownloadID == 0 {
		writeError(w, http.StatusBadRequest, "download_id is required")
		return
	}
	dl, err := s.store.GetDownload(req.DownloadID)
	if err != nil || dl == nil {
		writeError(w, http.StatusNotFound, "download not found")
		return
	}
	dl.AutoExtract = req.AutoExtract
	if err := s.store.UpdateDownload(dl); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"auto_extract": req.AutoExtract})
}

func clampSubdirDepth(d int) int {
	if d < 0 {
		return 0
	}
	if d > routing.MaxSubdirDepth {
		return routing.MaxSubdirDepth
	}
	return d
}

// configuredRoots returns the distinct destination-directory roots: the
// library's media root, every enabled category's dir, and storage's
// downloads dir — deduplicated. These are the valid roots for the download
// targets endpoint, the file manager, storage stats, and isValidTargetDir.
func (s *Server) configuredRoots() []string {
	seen := make(map[string]bool)
	var dirs []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if s.library != nil {
		cfg := s.library.Get()
		add(cfg.MediaRoot)
		for _, cat := range cfg.Categories {
			if !cat.Enabled || cat.Dir == "" {
				continue
			}
			dir := cat.Dir
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cfg.MediaRoot, dir)
			}
			add(dir)
		}
	}
	add(s.downloadsDir)
	return dirs
}

// advertisedRoots is configuredRoots minus the ones isValidTargetDir would
// reject — typically a configured category folder that doesn't exist yet —
// so list endpoints never offer a folder that selecting then refuses.
// Listing never creates folders.
func (s *Server) advertisedRoots() []string {
	var dirs []string
	for _, d := range s.configuredRoots() {
		if ok, _ := s.isValidTargetDir(d); ok {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// GET /api/downloads/targets — every configured root, plus their non-hidden
// subfolders down to the library's search_depth levels, sorted.
func (s *Server) handleGetDownloadTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	depth := routing.DefaultSubdirDepth
	if s.library != nil {
		depth = s.library.Get().SearchDepth
	}
	depth = clampSubdirDepth(depth)
	dirs := s.advertisedRoots()
	seen := make(map[string]bool)
	targets := []string{}
	for _, dir := range dirs {
		for _, t := range append([]string{dir}, routing.ListSubdirs(dir, depth)...) {
			if !seen[t] {
				seen[t] = true
				targets = append(targets, t)
			}
		}
	}
	sort.Strings(targets)
	writeJSON(w, http.StatusOK, targets)
}

// isValidTargetDir is the trust boundary for handleSetDownloadTarget: dir must
// be an existing directory at or below a configured root, at most
// MaxSubdirDepth levels down, with no hidden path components.
func (s *Server) isValidTargetDir(dir string) (bool, error) {
	roots := s.configuredRoots()
	// Resolve symlinks on both sides so a link can't point outside the roots.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, nil
	}
	for _, root := range roots {
		root, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if rel == "." {
			parts = nil
		}
		if len(parts) > routing.MaxSubdirDepth {
			continue
		}
		hidden := false
		for _, p := range parts {
			if strings.HasPrefix(p, ".") {
				hidden = true
			}
		}
		if hidden {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) handleSetDownloadTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DownloadID int64  `json:"download_id"`
		TargetDir  string `json:"target_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DownloadID == 0 {
		writeError(w, http.StatusBadRequest, "download_id is required")
		return
	}

	targetDir := req.TargetDir
	if targetDir != "" {
		targetDir = filepath.Clean(targetDir)
		valid, err := s.isValidTargetDir(targetDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "target_dir must be \"\" or a folder below a configured root")
			return
		}
	}

	dl, err := s.store.GetDownload(req.DownloadID)
	if err != nil || dl == nil {
		writeError(w, http.StatusNotFound, "download not found")
		return
	}
	dl.TargetDir = targetDir
	if err := s.store.UpdateDownload(dl); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"target_dir": targetDir})
}

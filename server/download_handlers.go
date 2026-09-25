package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/RealDtx/maxwell-irc/db"
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

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	autoExtract := true
	if req.AutoExtract != nil {
		autoExtract = *req.AutoExtract
	}
	autoSubdir := true
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

// downloadTargets returns, sorted, every distinct enabled routing
// destination dir plus its immediate non-hidden subdirectories. This is the
// trust boundary for handleSetDownloadTarget: only paths in this list may be
// stored as a download's target_dir.
func (s *Server) downloadTargets() ([]string, error) {
	rules, err := s.store.GetAllFileRoutingRules()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var targets []string
	for _, rule := range rules {
		if !rule.Enabled || rule.DestinationDir == "" || seen[rule.DestinationDir] {
			continue
		}
		seen[rule.DestinationDir] = true
		targets = append(targets, rule.DestinationDir)

		entries, err := readDirEntries(rule.DestinationDir)
		if err != nil {
			continue // skip dirs that fail to read
		}
		for _, entry := range entries {
			if !entry.IsDir {
				continue
			}
			sub := filepath.Join(rule.DestinationDir, entry.Name)
			if seen[sub] {
				continue
			}
			seen[sub] = true
			targets = append(targets, sub)
		}
	}
	sort.Strings(targets)
	return targets, nil
}

func (s *Server) handleGetDownloadTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	targets, err := s.downloadTargets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if targets == nil {
		targets = []string{}
	}
	writeJSON(w, http.StatusOK, targets)
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
		targets, err := s.downloadTargets()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		valid := false
		for _, t := range targets {
			if t == targetDir {
				valid = true
				break
			}
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "target_dir must be \"\" or match a known routing destination/subfolder")
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

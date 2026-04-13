package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
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
		ServerID   int64  `json:"server_id"`
		Channel    string `json:"channel"`
		BotNick    string `json:"bot_nick"`
		PackNumber int    `json:"pack_number"`
		Filename   string `json:"filename"`
		Filesize   int64  `json:"filesize"`
		StatsOnly  bool   `json:"stats_only"`
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

	dl, err := s.engine.Queue().Add(req.ServerID, req.Channel, req.BotNick, req.PackNumber, req.Filename, req.Filesize, req.StatsOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Send the XDCC request to the bot.
	if dispatchErr := s.engine.Dispatch(dl); dispatchErr != nil {
		log.Printf("download %d queued but dispatch failed: %v", dl.ID, dispatchErr)
		// Return 201 with a warning so the UI can show a toast
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"download":         dl,
			"dispatch_warning": dispatchErr.Error(),
		})
		return
	}

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
	if err := s.engine.Queue().Cancel(req.DownloadID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

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

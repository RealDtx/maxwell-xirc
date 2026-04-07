package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
)

func (s *Server) handleGetDownloads(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		ServerID   int64  `json:"server_id"`
		Channel    string `json:"channel"`
		BotNick    string `json:"bot_nick"`
		PackNumber int    `json:"pack_number"`
		Filename   string `json:"filename"`
		Filesize   int64  `json:"filesize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine == nil {
		writeError(w, http.StatusInternalServerError, "download engine not initialized")
		return
	}

	dl, err := s.engine.Queue().Add(req.ServerID, req.Channel, req.BotNick, req.PackNumber, req.Filename, req.Filesize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, dl)
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine != nil {
		if err := s.engine.Queue().Cancel(req.DownloadID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		// Fallback: operate directly on store
		dl, err := s.store.GetDownload(req.DownloadID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		dl.Status = "cancelled"
		if err := s.store.UpdateDownload(dl); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.engine != nil {
		if err := s.engine.Queue().Retry(req.DownloadID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		// Fallback: operate directly on store
		dl, err := s.store.GetDownload(req.DownloadID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		dl.Status = "queued"
		dl.ErrorMessage = ""
		dl.DownloadedBytes = 0
		dl.StartedAt = nil
		dl.CompletedAt = nil
		if err := s.store.UpdateDownload(dl); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleMoveDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadID int64 `json:"download_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
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

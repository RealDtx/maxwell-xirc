package server

import (
	"net/http"
	"strconv"

	"github.com/RealDtx/maxwell-irc/db"
)

// GET /api/stats/downloads
func (s *Server) handleDownloadStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	summary, err := s.store.GetDownloadStatsSummary()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load stats")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// GET /api/stats/history?offset=0&limit=50
func (s *Server) handleDownloadHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	history, err := s.store.GetDownloadHistory(offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load history")
		return
	}
	if history == nil {
		history = []db.DownloadStat{}
	}
	writeJSON(w, http.StatusOK, history)
}

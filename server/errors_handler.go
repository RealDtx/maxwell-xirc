package server

import (
	"net/http"
	"strconv"

	ircpkg "github.com/RealDtx/maxwell-irc/irc"
)

// GET /api/errors?limit=50
func (s *Server) handleGetErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.errBuf == nil {
		writeJSON(w, http.StatusOK, []ircpkg.ErrorEvent{})
		return
	}
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	errs := s.errBuf.GetErrors(limit)
	if errs == nil {
		errs = []ircpkg.ErrorEvent{}
	}
	writeJSON(w, http.StatusOK, errs)
}

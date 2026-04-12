package server

import (
	"net/http"
	"strconv"
	"strings"
)

// GET /api/irc/{server_id}/names?channel=#chan
func (s *Server) handleIRCNames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/irc/")
	path = strings.TrimSuffix(path, "/names")
	serverID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		writeError(w, http.StatusBadRequest, "channel required")
		return
	}

	nicks, err := s.ircMgr.Names(serverID, channel)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if nicks == nil {
		nicks = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"channel": channel,
		"nicks":   nicks,
	})
}

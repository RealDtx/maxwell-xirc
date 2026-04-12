package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	ircpkg "github.com/maxwell-xirc/xirc/irc"
)

// GET /api/irc/{server_id}/messages?channel=&before=RFC3339&limit=200
func (s *Server) handleIRCMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.msgBuf == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	// Extract server_id from path: /api/irc/{server_id}/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/irc/")
	path = strings.TrimSuffix(path, "/messages")
	serverID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}

	channel := r.URL.Query().Get("channel")
	beforeStr := r.URL.Query().Get("before")
	limitStr := r.URL.Query().Get("limit")

	var before time.Time
	if beforeStr != "" {
		before, err = time.Parse(time.RFC3339Nano, beforeStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before parameter")
			return
		}
	}

	limit := 200
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	msgs := s.msgBuf.GetMessages(serverID, channel, before, limit)
	if msgs == nil {
		msgs = []ircpkg.BufferedMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

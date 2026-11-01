package server

import (
	"encoding/json"
	"errors"
	"github.com/RealDtx/maxwell-irc/db"
	"net/http"
	"strconv"

	"github.com/RealDtx/maxwell-irc/discovery"
)

// SetDiscovery wires the channel-discovery scanner in — set once at startup.
func (s *Server) SetDiscovery(d *discovery.Scanner) { s.discovery = d }

func (s *Server) registerDiscoveryRoutes() {
	s.mux.HandleFunc("/api/discovery", s.handleDiscoveryList)
	s.mux.HandleFunc("/api/discovery/unseen", s.handleDiscoveryUnseen)
	s.mux.HandleFunc("/api/discovery/scan", s.handleDiscoveryScan)
	s.mux.HandleFunc("/api/discovery/cancel", s.discoveryServerAction(func(id int64) error { s.discovery.Cancel(id); return nil }))
	s.mux.HandleFunc("/api/discovery/seen", s.discoveryServerAction(func(id int64) error { return s.store.MarkDiscoveredSeen(id) }))
	s.mux.HandleFunc("/api/discovery/dismiss", s.discoveryFlagAction("dismissed"))
	s.mux.HandleFunc("/api/discovery/added", s.discoveryFlagAction("added"))
}

func (s *Server) discoveryReady(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	if s.discovery == nil {
		writeError(w, http.StatusServiceUnavailable, "discovery not available")
		return false
	}
	return true
}

func (s *Server) handleDiscoveryList(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryReady(w, r, http.MethodGet) {
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("server_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server_id")
		return
	}
	chans, err := s.store.GetDiscoveredChannels(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if chans == nil {
		chans = []db.DiscoveredChannel{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"channels": chans, "scan": s.discovery.Status(id)})
}

func (s *Server) handleDiscoveryUnseen(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryReady(w, r, http.MethodGet) {
		return
	}
	m, err := s.store.CountUnseenDiscovered()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if m == nil {
		m = map[int64]int{}
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDiscoveryScan(w http.ResponseWriter, r *http.Request) {
	if !s.discoveryReady(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ServerID int64 `json:"server_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ServerID == 0 {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	switch err := s.discovery.Scan(req.ServerID); {
	case errors.Is(err, discovery.ErrScanRunning):
		writeError(w, http.StatusConflict, "scan already running")
	case errors.Is(err, discovery.ErrNotConnected):
		writeError(w, http.StatusBadRequest, "server not connected")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusAccepted, s.discovery.Status(req.ServerID))
	}
}

// discoveryServerAction builds a POST {"server_id":N} handler.
func (s *Server) discoveryServerAction(fn func(int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.discoveryReady(w, r, http.MethodPost) {
			return
		}
		var req struct {
			ServerID int64 `json:"server_id"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.ServerID == 0 {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if err := fn(req.ServerID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// discoveryFlagAction builds a POST {"id":N} handler that sets a row flag.
func (s *Server) discoveryFlagAction(flag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.discoveryReady(w, r, http.MethodPost) {
			return
		}
		var req struct {
			ID int64 `json:"id"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == 0 {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if err := s.store.SetDiscoveredFlag(req.ID, flag); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

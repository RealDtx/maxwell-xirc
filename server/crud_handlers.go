package server

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/maxwell-xirc/xirc/db"
)

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetServers(w, r)
	case http.MethodPost:
		s.handleCreateServer(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleServerByID(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/servers/")
	if strings.HasSuffix(suffix, "/channels") {
		// GET /api/servers/{id}/channels
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		idStr := strings.TrimSuffix(suffix, "/channels")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		channels, err := s.store.GetChannels(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if channels == nil {
			channels = []db.Channel{}
		}
		writeJSON(w, http.StatusOK, channels)
		return
	}
	// /api/servers/{id} — PUT or DELETE
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateServer(w, r)
	case http.MethodDelete:
		s.handleDeleteServer(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.handleCreateChannel(w, r)
}

func (s *Server) handleChannelByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateChannel(w, r)
	case http.MethodDelete:
		s.handleDeleteChannel(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.GetServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if servers == nil {
		servers = []db.Server{}
	}
	writeJSON(w, http.StatusOK, servers)
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var srv db.Server
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.CreateServer(&srv); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(srv.ID)
	}
	writeJSON(w, http.StatusOK, srv)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var srv db.Server
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	srv.ID = id
	if err := s.store.UpdateServer(&srv); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(srv.ID)
	}
	writeJSON(w, http.StatusOK, srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.DisconnectServer(id)
	}
	if err := s.store.DeleteServer(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var ch db.Channel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.CreateChannel(&ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(ch.ServerID)
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var ch db.Channel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ch.ID = id
	if err := s.store.UpdateChannel(&ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(ch.ServerID)
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.GetChannel(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ch == nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err := s.store.DeleteChannel(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(ch.ServerID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

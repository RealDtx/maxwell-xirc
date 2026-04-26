package server

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/RealDtx/maxwell-irc/db"
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
	if strings.HasSuffix(suffix, "/realms") {
		// GET /api/servers/{id}/realms
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		idStr := strings.TrimSuffix(suffix, "/realms")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id == 0 {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		realms, err := s.store.GetRealms(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if realms == nil {
			realms = []db.Realm{}
		}
		writeJSON(w, http.StatusOK, realms)
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

func (s *Server) handleRealms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetRealms(w, r)
	case http.MethodPost:
		s.handleCreateRealm(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetRealms(w http.ResponseWriter, r *http.Request) {
	serverIDStr := r.URL.Query().Get("server_id")
	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	realms, err := s.store.GetRealms(serverID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if realms == nil {
		realms = []db.Realm{}
	}
	writeJSON(w, http.StatusOK, realms)
}

func (s *Server) handleRealmByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateRealm(w, r)
	case http.MethodDelete:
		s.handleDeleteRealm(w, r)
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
	if strings.TrimSpace(srv.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "nickname is required")
		return
	}
	if err := s.store.CreateServer(&srv); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(srv.ID)
	}
	writeJSON(w, http.StatusCreated, srv)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var srv db.Server
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(srv.Nickname) == "" {
		writeError(w, http.StatusBadRequest, "nickname is required")
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
	if err != nil || id == 0 {
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

func (s *Server) handleCreateRealm(w http.ResponseWriter, r *http.Request) {
	var realm db.Realm
	if err := json.NewDecoder(r.Body).Decode(&realm); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if realm.ServerID == 0 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	realm.Enabled = true
	if err := s.store.CreateRealm(&realm); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadRealms(realm.ServerID)
	}
	writeJSON(w, http.StatusCreated, realm)
}

func (s *Server) handleUpdateRealm(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/realms/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var realm db.Realm
	if err := json.NewDecoder(r.Body).Decode(&realm); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if realm.ServerID == 0 {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	realm.ID = id
	realm.Enabled = true
	if err := s.store.UpdateRealm(&realm); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadRealms(realm.ServerID)
	}
	writeJSON(w, http.StatusOK, realm)
}

func (s *Server) handleDeleteRealm(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/realms/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	realm, err := s.store.GetRealm(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if realm == nil {
		writeError(w, http.StatusNotFound, "realm not found")
		return
	}
	if err := s.store.DeleteRealm(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadRealms(realm.ServerID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

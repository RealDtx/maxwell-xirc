package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/maxwell-xirc/xirc/db"
)

func (s *Server) handleGetSearchResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	serverIDStr := r.URL.Query().Get("server_id")
	channel := r.URL.Query().Get("channel")

	if query == "" || serverIDStr == "" || channel == "" {
		writeError(w, http.StatusBadRequest, "query, server_id, and channel are required")
		return
	}

	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server_id")
		return
	}

	results, err := s.store.GetSearchResults(query, serverID, channel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if results == nil {
		results = []db.SearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleStartSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServerID int64  `json:"server_id"`
		Channel  string `json:"channel"`
		Query    string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.parser != nil {
		s.parser.StartSearch(req.ServerID, req.Channel, req.Query)
	}

	// Get the search command for this channel
	var searchCmd = "!s"
	if s.store != nil {
		channels, err := s.store.GetChannels(req.ServerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, ch := range channels {
			if ch.Name == req.Channel {
				if ch.SearchCommand != "" {
					searchCmd = ch.SearchCommand
				}
				break
			}
		}
	}

	// Send the search command via IRC
	if s.ircMgr != nil {
		if err := s.ircMgr.SendMessage(req.ServerID, req.Channel, searchCmd+" "+req.Query); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "searching"})
}

// handleSavedSearches handles GET (list) and POST (create) for /api/search/saved
func (s *Server) handleSavedSearches(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetSavedSearches(w, r)
	case http.MethodPost:
		s.handleCreateSavedSearch(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetSavedSearches(w http.ResponseWriter, r *http.Request) {
	searches, err := s.store.GetSavedSearches()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if searches == nil {
		searches = []db.SavedSearch{}
	}
	writeJSON(w, http.StatusOK, searches)
}

func (s *Server) handleCreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	var ss db.SavedSearch
	if err := json.NewDecoder(r.Body).Decode(&ss); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.store.CreateSavedSearch(&ss); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ss)
}

// handleSavedSearchByID handles DELETE /api/search/saved/{id}
func (s *Server) handleSavedSearchByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/search/saved/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := s.store.DeleteSavedSearch(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleParsePatterns handles GET (list) and POST (create) for /api/search/patterns
func (s *Server) handleParsePatterns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetParsePatterns(w, r)
	case http.MethodPost:
		s.handleCreateParsePattern(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetParsePatterns(w http.ResponseWriter, r *http.Request) {
	patterns, err := s.store.GetParsePatterns()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if patterns == nil {
		patterns = []db.ParsePattern{}
	}
	writeJSON(w, http.StatusOK, patterns)
}

func (s *Server) handleCreateParsePattern(w http.ResponseWriter, r *http.Request) {
	var p db.ParsePattern
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p.Enabled = true
	if err := s.store.CreateParsePattern(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// handleParsePatternByID handles PUT /api/search/patterns/{id}
func (s *Server) handleParsePatternByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/search/patterns/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var p db.ParsePattern
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p.ID = id
	if err := s.store.UpdateParsePattern(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, p)
}

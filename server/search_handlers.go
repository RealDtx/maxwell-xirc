package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/parser"
)

func (s *Server) handleGetSearchResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	serverIDStr := r.URL.Query().Get("server_id")
	channel := r.URL.Query().Get("channel")
	sinceStr := r.URL.Query().Get("since") // Unix ms timestamp — only return results created after this

	if query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	var since *time.Time
	if sinceStr != "" {
		ms, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil {
			t := time.UnixMilli(ms)
			since = &t
		}
	}

	var results []db.SearchResult
	var err error

	if serverIDStr == "" || channel == "" {
		results, err = s.store.GetAllSearchResults(query, since)
	} else {
		serverID, parseErr := strconv.ParseInt(serverIDStr, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid server_id")
			return
		}
		results, err = s.store.GetSearchResults(query, serverID, channel)
	}

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
		realms, err := s.store.GetRealms(req.ServerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, r := range realms {
			if r.Name == req.Channel {
				if r.SearchCommand != "" {
					searchCmd = r.SearchCommand
				}
				break
			}
		}
	}

	// Send the search command via IRC
	if s.ircMgr != nil {
		cmd := searchCmd + " " + req.Query
		if err := s.ircMgr.SendMessage(req.ServerID, req.Channel, cmd); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "searching"})
}

func (s *Server) handleStopSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServerID int64  `json:"server_id"`
		Channel  string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.parser != nil {
		s.parser.StopSearch(req.ServerID, req.Channel)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
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

func (s *Server) handleGetUnmatchedSamples(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serverIDStr := r.URL.Query().Get("server_id")
	query := r.URL.Query().Get("query")
	sinceStr := r.URL.Query().Get("since")
	limitStr := r.URL.Query().Get("limit")

	if serverIDStr == "" {
		writeError(w, http.StatusBadRequest, "server_id is required")
		return
	}
	if query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server_id")
		return
	}

	since := time.Now().Add(-1 * time.Hour)
	if sinceStr != "" {
		ms, err := strconv.ParseInt(sinceStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since")
			return
		}
		since = time.UnixMilli(ms)
	}

	limit := 20
	if limitStr != "" {
		v, err := strconv.Atoi(limitStr)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = v
	}
	if limit > 50 {
		limit = 50
	}

	results, err := s.store.GetUnparsedSearchSamples(serverID, query, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []db.SearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleLearnPattern(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RawLine     string              `json:"raw_line"`
		Annotations []parser.Annotation `json:"annotations"`
		Name        string              `json:"name"`
		Preview     bool                `json:"preview"`
		Since       *int64              `json:"since"` // Unix ms timestamp; defaults to 1 hour ago
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := parser.GeneratePatternFromAnnotations(req.RawLine, req.Annotations)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Preview {
		writeJSON(w, http.StatusOK, map[string]string{
			"regex":         result.Regex,
			"field_mapping": result.FieldMapping,
		})
		return
	}

	p := db.ParsePattern{
		Name:         req.Name,
		Regex:        result.Regex,
		FieldMapping: result.FieldMapping,
		Enabled:      true,
		Priority:     50,
	}
	if err := s.store.CreateParsePattern(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	reprocessSince := time.Now().Add(-1 * time.Hour)
	if req.Since != nil {
		reprocessSince = time.UnixMilli(*req.Since)
	}
	unmatched, err := s.store.GetAllUnparsedSince(reprocessSince)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	newlyParsed := 0
	for _, sr := range unmatched {
		match, _, err := parser.MatchLine(sr.RawLine, []db.ParsePattern{p})
		if err != nil {
			if errors.Is(err, parser.ErrNoMatch) {
				continue
			}
			continue
		}

		botNick := sr.BotNick
		if match.BotNick != nil {
			botNick = *match.BotNick
		}

		if err := s.store.MarkSearchResultParsed(sr.ID, botNick, match.PackNumber, match.Filename, match.Filesize, match.DownloadsCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		newlyParsed++

		// TODO: publish irc.EventSearchResult for reparsed results once server has direct EventBus access.
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pattern_id":    p.ID,
		"regex":         result.Regex,
		"field_mapping": result.FieldMapping,
		"newly_parsed":  newlyParsed,
	})
}

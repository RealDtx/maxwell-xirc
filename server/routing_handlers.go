package server

import (
	"encoding/json"
	"net/http"
	"path"
	"strconv"

	"github.com/RealDtx/maxwell-irc/db"
)

// handleRoutingRules handles GET /api/routing/rules and POST /api/routing/rules
func (s *Server) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules, err := s.store.GetAllFileRoutingRules()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rules == nil {
			rules = []db.FileRoutingRule{}
		}
		writeJSON(w, http.StatusOK, rules)

	case http.MethodPost:
		var rule db.FileRoutingRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.store.CreateFileRoutingRule(&rule); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, rule)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleRoutingRuleByID handles PUT /api/routing/rules/{id} and DELETE /api/routing/rules/{id}
func (s *Server) handleRoutingRuleByID(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	switch r.Method {
	case http.MethodPut:
		existing, err := s.store.GetFileRoutingRuleByID(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		var rule db.FileRoutingRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		rule.ID = id
		rule.Builtin = existing.Builtin  // Preserve immutable field
		if err := s.store.UpdateFileRoutingRule(&rule); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rule)

	case http.MethodDelete:
		existing, err := s.store.GetFileRoutingRuleByID(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "rule not found")
			return
		}
		if existing.Builtin && existing.Pattern == "*" {
			writeError(w, http.StatusForbidden, "cannot delete builtin catch-all rule")
			return
		}
		if err := s.store.DeleteFileRoutingRule(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleHooks handles GET /api/hooks and POST /api/hooks
func (s *Server) handleHooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		scope := r.URL.Query().Get("scope")
		scopeIDStr := r.URL.Query().Get("scope_id")

		var scopeID *int64
		if scope != "" && scopeIDStr != "" {
			parsed, err := strconv.ParseInt(scopeIDStr, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid scope_id")
				return
			}
			scopeID = &parsed
		}

		hooks, err := s.store.GetPostHooks(scope, scopeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if hooks == nil {
			hooks = []db.PostHook{}
		}
		writeJSON(w, http.StatusOK, hooks)

	case http.MethodPost:
		var hook db.PostHook
		if err := json.NewDecoder(r.Body).Decode(&hook); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.store.CreatePostHook(&hook); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, hook)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleHookByID handles PUT /api/hooks/{id} and DELETE /api/hooks/{id}
func (s *Server) handleHookByID(w http.ResponseWriter, r *http.Request) {
	idStr := path.Base(r.URL.Path)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var hook db.PostHook
		if err := json.NewDecoder(r.Body).Decode(&hook); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		existing, err := s.store.GetPostHookByID(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "hook not found")
			return
		}
		hook.ID = id
		if err := s.store.UpdatePostHook(&hook); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, hook)

	case http.MethodDelete:
		existing, err := s.store.GetPostHookByID(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "hook not found")
			return
		}
		if err := s.store.DeletePostHook(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

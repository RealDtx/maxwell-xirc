package server

import (
	"net/http"
	"strings"
)

// handleIRCDispatch routes /api/irc/{id}/messages and future parameterised routes
// since Go 1.13 ServeMux doesn't support path parameters.
func (s *Server) handleIRCDispatch(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/irc/")
	switch {
	case strings.HasSuffix(path, "/messages"):
		s.handleIRCMessages(w, r)
	case strings.HasSuffix(path, "/names"):
		s.handleIRCNames(w, r)
	default:
		http.NotFound(w, r)
	}
}

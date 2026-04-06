package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

type Server struct {
	store  db.Store
	ircMgr *irc.Manager
	mux    *http.ServeMux
}

func New(store db.Store, ircMgr *irc.Manager) *Server {
	s := &Server{
		store:  store,
		ircMgr: ircMgr,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)

	// IRC endpoints
	s.mux.HandleFunc("/api/irc/status", s.handleIRCStatus)
	s.mux.HandleFunc("/api/irc/connect", s.handleIRCConnect)
	s.mux.HandleFunc("/api/irc/disconnect", s.handleIRCDisconnect)
	s.mux.HandleFunc("/api/irc/message", s.handleIRCSendMessage)
	s.mux.HandleFunc("/api/irc/raw", s.handleIRCSendRaw)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

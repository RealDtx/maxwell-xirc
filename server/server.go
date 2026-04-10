package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
	"github.com/maxwell-xirc/xirc/queue"
	ws "github.com/maxwell-xirc/xirc/ws"
)

type Server struct {
	store  db.Store
	ircMgr *irc.Manager
	parser *parser.Parser
	engine *queue.Engine
	wsHub  *ws.Hub
	mux    *http.ServeMux
}

func New(store db.Store, ircMgr *irc.Manager, p *parser.Parser, eng *queue.Engine, hub *ws.Hub) *Server {
	s := &Server{
		store:  store,
		ircMgr: ircMgr,
		parser: p,
		engine: eng,
		wsHub:  hub,
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

	// Search endpoints
	s.mux.HandleFunc("/api/search/results", s.handleGetSearchResults)
	s.mux.HandleFunc("/api/search/start", s.handleStartSearch)
	s.mux.HandleFunc("/api/search/saved", s.handleSavedSearches)
	s.mux.HandleFunc("/api/search/saved/", s.handleSavedSearchByID)
	s.mux.HandleFunc("/api/search/patterns", s.handleParsePatterns)
	s.mux.HandleFunc("/api/search/patterns/", s.handleParsePatternByID)

	// Download endpoints
	s.mux.HandleFunc("/api/downloads", s.handleGetDownloads)
	s.mux.HandleFunc("/api/downloads/request", s.handleRequestDownload)
	s.mux.HandleFunc("/api/downloads/cancel", s.handleCancelDownload)
	s.mux.HandleFunc("/api/downloads/retry", s.handleRetryDownload)
	s.mux.HandleFunc("/api/downloads/move", s.handleMoveDownload)

	// Routing rule endpoints
	s.mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	s.mux.HandleFunc("/api/routing/rules/", s.handleRoutingRuleByID)

	// Hook endpoints
	s.mux.HandleFunc("/api/hooks", s.handleHooks)
	s.mux.HandleFunc("/api/hooks/", s.handleHookByID)

	// WebSocket endpoint
	s.mux.HandleFunc("/ws", s.handleWebSocket)
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

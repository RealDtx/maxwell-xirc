package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
	ircpkg "github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
	"github.com/maxwell-xirc/xirc/queue"
	ws "github.com/maxwell-xirc/xirc/ws"
)

type Server struct {
	store  db.Store
	ircMgr *ircpkg.Manager
	parser *parser.Parser
	engine *queue.Engine
	wsHub  *ws.Hub
	msgBuf *ircpkg.MessageBuffer
	errBuf *ircpkg.ErrorBuffer
	setup  *SetupState
	mux    *http.ServeMux
}

func New(store db.Store, ircMgr *ircpkg.Manager, p *parser.Parser, eng *queue.Engine, hub *ws.Hub, msgBuf *ircpkg.MessageBuffer, errBuf *ircpkg.ErrorBuffer, setup *SetupState) *Server {
	s := &Server{
		store:  store,
		ircMgr: ircMgr,
		parser: p,
		engine: eng,
		wsHub:  hub,
		msgBuf: msgBuf,
		errBuf: errBuf,
		setup:  setup,
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

	// IRC endpoints (exact paths first, then prefix for parameterised routes)
	s.mux.HandleFunc("/api/irc/status", s.handleIRCStatus)
	s.mux.HandleFunc("/api/irc/connect", s.handleIRCConnect)
	s.mux.HandleFunc("/api/irc/disconnect", s.handleIRCDisconnect)
	s.mux.HandleFunc("/api/irc/join", s.handleIRCJoin)
	s.mux.HandleFunc("/api/irc/message", s.handleIRCSendMessage)
	s.mux.HandleFunc("/api/irc/raw", s.handleIRCSendRaw)
	s.mux.HandleFunc("/api/irc/", s.handleIRCDispatch) // must be last — prefix match for parameterised routes

	// Search endpoints
	s.mux.HandleFunc("/api/search/results", s.handleGetSearchResults)
	s.mux.HandleFunc("/api/search/start", s.handleStartSearch)
	s.mux.HandleFunc("/api/search/stop", s.handleStopSearch)
	s.mux.HandleFunc("/api/search/saved", s.handleSavedSearches)
	s.mux.HandleFunc("/api/search/saved/", s.handleSavedSearchByID)
	s.mux.HandleFunc("/api/search/patterns", s.handleParsePatterns)
	s.mux.HandleFunc("/api/search/unmatched", s.handleGetUnmatchedSamples)
	s.mux.HandleFunc("/api/search/patterns/learn", s.handleLearnPattern)
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

	// Server/realm CRUD
	s.mux.HandleFunc("/api/servers", s.handleServers)
	s.mux.HandleFunc("/api/servers/", s.handleServerByID)
	s.mux.HandleFunc("/api/realms", s.handleRealms)
	s.mux.HandleFunc("/api/realms/", s.handleRealmByID)

	// Browse & Files endpoints
	s.mux.HandleFunc("/api/browse", s.handleBrowse)
	s.mux.HandleFunc("/api/files", s.handleFiles)

	// Stats endpoints
	s.mux.HandleFunc("/api/stats/downloads", s.handleDownloadStats)
	s.mux.HandleFunc("/api/stats/history", s.handleDownloadHistory)

	// Errors endpoint
	s.mux.HandleFunc("/api/errors", s.handleGetErrors)

	// Storage stats endpoint
	s.mux.HandleFunc("/api/storage", s.handleStorageStats)

	// Setup wizard endpoints
	s.mux.HandleFunc("/api/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("/api/setup/defaults", s.handleSetupDefaults)
	s.mux.HandleFunc("/api/setup/complete", s.handleSetupComplete)

	// WebSocket endpoint
	s.mux.HandleFunc("/ws", s.handleWebSocket)

	// Static file serving (must be last — catch-all)
	s.setupStaticFiles()
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

package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"

	"github.com/RealDtx/maxwell-irc/db"
	ircpkg "github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/library"
	"github.com/RealDtx/maxwell-irc/parser"
	"github.com/RealDtx/maxwell-irc/queue"
	ws "github.com/RealDtx/maxwell-irc/ws"
)

type Server struct {
	store    db.Store
	ircMgr   *ircpkg.Manager
	parser   *parser.Parser
	engine   *queue.Engine
	wsHub    *ws.Hub
	msgBuf   *ircpkg.MessageBuffer
	errBuf   *ircpkg.ErrorBuffer
	setup    *SetupState
	mux      *http.ServeMux
	prefix   string
	webFS    fs.FS
	library  *library.Manager
	auth     *Auth
	setupMu  sync.Mutex // serialises /api/auth/setup and last-admin checks
	caps     capState
	settings settingsState

	extracting sync.Map // target dir → struct{}; guards concurrent extracts
}

// SetLibrary wires the library manager in — set once at startup.
func (s *Server) SetLibrary(m *library.Manager) {
	s.library = m
}

// SetDownloadsDir wires storage.downloads_dir in — set once at startup,
// alongside SetLibrary. It's a thin wrapper over the capability lock that
// guards downloadsDir; setStorageDirs is the live-update path.
func (s *Server) SetDownloadsDir(dir string) {
	s.caps.mu.Lock()
	s.caps.downloadsDir = dir
	s.caps.mu.Unlock()
}

func New(store db.Store, ircMgr *ircpkg.Manager, p *parser.Parser, eng *queue.Engine, hub *ws.Hub, msgBuf *ircpkg.MessageBuffer, errBuf *ircpkg.ErrorBuffer, setup *SetupState, prefix string, webFS fs.FS) *Server {
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
		prefix: prefix,
		webFS:  webFS,
	}
	s.routes()
	return s
}

// SetAuth enables authentication. main() always calls it; tests that don't
// exercise auth leave it unset and get the bare mux.
func (s *Server) SetAuth(a *Auth) {
	s.auth = a
	logAuthConfig(a)
}

func (s *Server) Handler() http.Handler {
	if s.auth == nil {
		return s.mux
	}
	return s.auth.middleware(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)

	// Auth & users (handlers require SetAuth; only registered routes reached via the middleware)
	s.mux.HandleFunc("/api/auth/login", s.handleLogin)
	s.mux.HandleFunc("/api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("/api/auth/me", s.handleMe)
	s.mux.HandleFunc("/api/auth/setup", s.handleAuthSetup)
	s.mux.HandleFunc("/api/users", s.handleUsers)
	s.mux.HandleFunc("/api/users/", s.handleUserByID)

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
	s.mux.HandleFunc("/api/search/patterns/import", s.handleImportPatterns)
	s.mux.HandleFunc("/api/search/patterns/", s.handleParsePatternByID)

	// Index search endpoints (self-collected, offline search catalog)
	s.mux.HandleFunc("/api/index/search", s.handleIndexSearch)
	s.mux.HandleFunc("/api/index/stats", s.handleIndexStats)
	s.mux.HandleFunc("/api/index/stats/detail", s.handleIndexStatsDetail)
	s.mux.HandleFunc("/api/index/clear", s.handleClearIndex)

	// Download endpoints
	s.mux.HandleFunc("/api/downloads", s.handleGetDownloads)
	s.mux.HandleFunc("/api/downloads/request", s.handleRequestDownload)
	s.mux.HandleFunc("/api/downloads/cancel", s.handleCancelDownload)
	s.mux.HandleFunc("/api/downloads/retry", s.handleRetryDownload)
	s.mux.HandleFunc("/api/downloads/move", s.handleMoveDownload)
	s.mux.HandleFunc("/api/downloads/delete", s.handleDeleteDownloads)
	s.mux.HandleFunc("/api/downloads/clear", s.handleClearDownloads)
	s.mux.HandleFunc("/api/downloads/set-auto-extract", s.handleSetAutoExtract)
	s.mux.HandleFunc("/api/downloads/targets", s.handleGetDownloadTargets)
	s.mux.HandleFunc("/api/downloads/set-target", s.handleSetDownloadTarget)

	// Library (categories) endpoints
	s.mux.HandleFunc("/api/library", s.handleLibrary)
	s.mux.HandleFunc("/api/library/detect", s.handleLibraryDetect)
	s.mux.HandleFunc("/api/library/preview", s.handleLibraryPreview)
	s.mux.HandleFunc("/api/library/kinds", s.handleLibraryKinds)

	// Server/realm CRUD
	s.mux.HandleFunc("/api/servers", s.handleServers)
	s.mux.HandleFunc("/api/servers/", s.handleServerByID)
	s.mux.HandleFunc("/api/realms", s.handleRealms)
	s.mux.HandleFunc("/api/realms/", s.handleRealmByID)

	// Browse & Files endpoints
	s.mux.HandleFunc("/api/browse", s.handleBrowse)
	s.mux.HandleFunc("/api/files", s.handleFiles)
	s.mux.HandleFunc("/api/files/raw", s.handleFilesRaw)

	// Stats endpoints
	s.mux.HandleFunc("/api/stats/downloads", s.handleDownloadStats)
	s.mux.HandleFunc("/api/stats/history", s.handleDownloadHistory)

	// Errors endpoint
	s.mux.HandleFunc("/api/errors", s.handleGetErrors)

	// Capabilities endpoint (filesystem permission snapshot)
	s.mux.HandleFunc("/api/capabilities", s.handleCapabilities)
	s.mux.HandleFunc("/api/capabilities/recheck", s.handleCapabilitiesRecheck)

	// Storage stats endpoint
	s.mux.HandleFunc("/api/storage", s.handleStorageStats)

	// Admin settings endpoint
	s.mux.HandleFunc("/api/settings", s.handleSettings)

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
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

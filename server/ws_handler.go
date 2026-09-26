package server

import (
	"log"
	"net/http"

	wsPkg "github.com/RealDtx/maxwell-irc/ws"
	"github.com/gorilla/websocket"
)

// Default CheckOrigin: Origin host must equal Host. Proxies must preserve
// Host (both shipped configs do). Blocks cross-site WebSocket hijacking.
var upgrader = websocket.Upgrader{}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.wsHub == nil {
		http.Error(w, "WebSocket not available", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}

	wsPkg.ServeClient(s.wsHub, conn)
}

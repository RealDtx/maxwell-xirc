package server

import (
	"log"
	"net/http"

	wsPkg "github.com/RealDtx/maxwell-irc/ws"
	"github.com/gorilla/websocket"
)

// Default CheckOrigin: Origin host:port must equal Host. Proxies must
// forward the original Host header including port (nginx: proxy_set_header
// Host $http_host; Apache: ProxyPreserveHost On).
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

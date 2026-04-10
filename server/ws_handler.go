package server

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	wsPkg "github.com/maxwell-xirc/xirc/ws"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Internal network only, behind nginx
	},
}

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

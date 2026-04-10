package ws

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// ServeClient handles a single WebSocket connection.
// It registers with the hub, reads incoming messages (for future use),
// and writes outgoing events from the hub.
func ServeClient(hub *Hub, conn *websocket.Conn) {
	sendCh := make(chan []byte, 256)
	hub.Register(sendCh)

	defer func() {
		hub.Unregister(sendCh)
		conn.Close()
	}()

	// Reader goroutine — reads and discards (or handles future commands)
	go func() {
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()

	// Writer
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-sendCh:
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("ws write error: %v", err)
				return
			}
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

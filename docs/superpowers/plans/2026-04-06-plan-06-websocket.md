# xirc Plan 6: WebSocket & CRUD API Completion — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add WebSocket support for real-time events (IRC messages, download progress, notifications) and complete the remaining CRUD API endpoints for servers and channels management.

**Architecture:** A `ws` package manages WebSocket connections using gorilla/websocket. It subscribes to the event bus and fans out events to all connected browser clients. The server package gets full CRUD endpoints for servers and channels.

**Tech Stack:** Go 1.22+, `github.com/gorilla/websocket`, existing packages

**Depends on:** Plans 1-5 must be complete.

---

## File Structure

```
maxwell-xirc/
├── ws/
│   ├── hub.go               # WebSocket hub: manages client connections
│   ├── hub_test.go
│   ├── client.go            # Single WebSocket client connection
│   └── client_test.go
├── server/
│   ├── ws_handler.go        # WebSocket upgrade endpoint
│   ├── crud_handlers.go     # Server + channel CRUD endpoints
│   └── crud_handlers_test.go
```

---

### Task 1: WebSocket Hub

**Files:**
- Create: `ws/hub.go`
- Create: `ws/hub_test.go`

- [ ] **Step 1: Write failing tests**

Create `ws/hub_test.go`:

```go
package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/maxwell-xirc/xirc/irc"
)

func TestHub_BroadcastsEvents(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	// Create a mock client channel
	clientCh := make(chan []byte, 10)
	hub.Register(clientCh)
	defer hub.Unregister(clientCh)

	// Publish an event
	bus.Publish(irc.Event{
		Type:     irc.EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Data:     "hello",
	})

	select {
	case msg := <-clientCh:
		var ev irc.Event
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if ev.Type != irc.EventIRCMessage {
			t.Errorf("expected type irc_message, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestHub_MultipleClients(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)
	hub.Register(ch1)
	hub.Register(ch2)
	defer hub.Unregister(ch1)
	defer hub.Unregister(ch2)

	bus.Publish(irc.Event{Type: irc.EventNotification, Data: "test"})

	for _, ch := range []chan []byte{ch1, ch2} {
		select {
		case <-ch:
			// Good
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
}

func TestHub_UnregisterStopsReceiving(t *testing.T) {
	bus := irc.NewEventBus()
	hub := NewHub(bus)
	hub.Start()
	defer hub.Stop()

	ch := make(chan []byte, 10)
	hub.Register(ch)
	hub.Unregister(ch)

	bus.Publish(irc.Event{Type: irc.EventNotification, Data: "test"})

	time.Sleep(100 * time.Millisecond)
	select {
	case <-ch:
		t.Error("received after unregister")
	default:
		// Good
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ws && go test -v ./...`

- [ ] **Step 3: Implement WebSocket hub**

Create `ws/hub.go`:

```go
package ws

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/maxwell-xirc/xirc/irc"
)

type Hub struct {
	mu      sync.RWMutex
	bus     *irc.EventBus
	eventCh <-chan irc.Event
	clients map[chan []byte]struct{}
	stopCh  chan struct{}
}

func NewHub(bus *irc.EventBus) *Hub {
	return &Hub{
		bus:     bus,
		clients: make(map[chan []byte]struct{}),
		stopCh:  make(chan struct{}),
	}
}

func (h *Hub) Start() {
	h.eventCh = h.bus.Subscribe()
	go h.loop()
}

func (h *Hub) Stop() {
	close(h.stopCh)
	h.bus.Unsubscribe(h.eventCh)
}

func (h *Hub) Register(ch chan []byte) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) loop() {
	for {
		select {
		case <-h.stopCh:
			return
		case ev, ok := <-h.eventCh:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				log.Printf("failed to marshal event: %v", err)
				continue
			}
			h.broadcast(data)
		}
	}
}

func (h *Hub) broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			// Slow client, drop
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd ws && go test -v ./...`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add ws/hub.go ws/hub_test.go
git commit -m "feat: add WebSocket hub for real-time event broadcasting"
```

---

### Task 2: WebSocket Client Handler

**Files:**
- Create: `ws/client.go`
- Create: `server/ws_handler.go`

- [ ] **Step 1: Implement WebSocket client wrapper**

Create `ws/client.go`:

```go
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
```

- [ ] **Step 2: Create WebSocket upgrade handler**

Create `server/ws_handler.go`:

```go
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
```

- [ ] **Step 3: Update server.go**

Add `wsHub *ws.Hub` field to Server struct. Add `ws/` import. Accept hub in `New()`. Add route: `s.mux.HandleFunc("GET /ws", s.handleWebSocket)`.

- [ ] **Step 4: Install gorilla/websocket dependency**

Run:
```bash
go get github.com/gorilla/websocket
```

- [ ] **Step 5: Commit**

```bash
git add ws/ server/ws_handler.go server/server.go go.mod go.sum
git commit -m "feat: add WebSocket endpoint for real-time browser updates"
```

---

### Task 3: Server & Channel CRUD Endpoints

**Files:**
- Create: `server/crud_handlers.go`
- Create: `server/crud_handlers_test.go`

- [ ] **Step 1: Implement CRUD handlers**

Create `server/crud_handlers.go` with endpoints:

```
GET    /api/servers                 — list all servers
POST   /api/servers                 — create server
PUT    /api/servers/{id}            — update server
DELETE /api/servers/{id}            — delete server
GET    /api/servers/{id}/channels   — list channels for server
POST   /api/channels               — create channel
PUT    /api/channels/{id}           — update channel
DELETE /api/channels/{id}           — delete channel
```

Each handler: decode path params / JSON body → call store method → return JSON. Same pattern as all other handlers.

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/maxwell-xirc/xirc/db"
)

func (s *Server) handleGetServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.GetServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if servers == nil {
		servers = []db.Server{}
	}
	writeJSON(w, http.StatusOK, servers)
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var srv db.Server
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.CreateServer(&srv); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Reload in IRC manager if available
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(srv.ID)
	}
	writeJSON(w, http.StatusOK, srv)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var srv db.Server
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	srv.ID = id
	if err := s.store.UpdateServer(&srv); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(srv.ID)
	}
	writeJSON(w, http.StatusOK, srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.DisconnectServer(id)
	}
	if err := s.store.DeleteServer(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleGetChannels(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	channels, err := s.store.GetChannels(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if channels == nil {
		channels = []db.Channel{}
	}
	writeJSON(w, http.StatusOK, channels)
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var ch db.Channel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.CreateChannel(&ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(ch.ServerID)
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var ch db.Channel
	if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ch.ID = id
	if err := s.store.UpdateChannel(&ch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(ch.ServerID)
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ch, err := s.store.GetChannel(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.DeleteChannel(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.ircMgr != nil {
		s.ircMgr.ReloadServer(ch.ServerID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
```

- [ ] **Step 2: Add routes to server.go**

Add these routes in `routes()`:
```go
	// Server/channel CRUD
	s.mux.HandleFunc("GET /api/servers", s.handleGetServers)
	s.mux.HandleFunc("POST /api/servers", s.handleCreateServer)
	s.mux.HandleFunc("PUT /api/servers/{id}", s.handleUpdateServer)
	s.mux.HandleFunc("DELETE /api/servers/{id}", s.handleDeleteServer)
	s.mux.HandleFunc("GET /api/servers/{id}/channels", s.handleGetChannels)
	s.mux.HandleFunc("POST /api/channels", s.handleCreateChannel)
	s.mux.HandleFunc("PUT /api/channels/{id}", s.handleUpdateChannel)
	s.mux.HandleFunc("DELETE /api/channels/{id}", s.handleDeleteChannel)
```

- [ ] **Step 3: Write tests, run them**

Create `server/crud_handlers_test.go` with standard HTTP test patterns for each endpoint.

Run: `go test ./...`

Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add server/
git commit -m "feat: add server/channel CRUD and WebSocket wiring"
```

---

## End State

After Plan 6: full REST API for all entities, WebSocket for real-time updates, ready for the browser frontend (Plan 7).

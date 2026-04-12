# xirc Plan 9: UI Redesign & Backend Enhancements — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the web UI with Simple/Advanced modes, server/channel views with IRC logs and user lists, file manager, download stats, error overview, mobile layout, and directory picker — backed by new in-memory buffers, extended IRC stats, and a download history table.

**Architecture:** Backend additions subscribe to the existing `irc.EventBus` (message buffer, error buffer) or add new routes to the HTTP server. Frontend is an Alpine.js SPA — new state variables and view templates added to `app.js` and `index.html`. Tasks are ordered: backend first, then frontend.

**Tech Stack:** Go 1.13 (no embed.FS, no r.PathValue(), no method-prefixed routes, use `strings.TrimPrefix` for path params, `ioutil.TempDir` + `defer os.RemoveAll` in tests), Alpine.js v3, plain CSS.

**Depends on:** Plans 1–8 complete.

---

## File Structure

```
maxwell-xirc/
├── internal/exitcodes/exitcodes.go     NEW — exit code constants
├── irc/
│   ├── message_buffer.go               NEW — ring buffer subscribing to EventBus
│   ├── message_buffer_test.go          NEW
│   ├── error_buffer.go                 NEW — ring buffer for EventError events
│   ├── error_buffer_test.go            NEW
│   ├── client.go                       MODIFY — add SendLine() to IRCClient interface
│   ├── rawclient.go                    MODIFY — implement SendLine()
│   ├── connection.go                   MODIFY — track stats, NAMES, PING/PONG, SendRaw
│   ├── events.go                       MODIFY — add EventError, ErrorEvent struct
│   ├── manager.go                      MODIFY — expose Names(), extended GetStatuses()
├── db/
│   ├── models.go                       MODIFY — add DownloadStat, StatsOnly to Download
│   ├── migrations.go                   MODIFY — add download_stats table + stats_only column
│   ├── store.go                        MODIFY — add download stat methods to Store interface
│   ├── sqlite.go                       MODIFY — implement new Store methods
│   ├── mysql.go                        MODIFY — implement new Store methods
├── server/
│   ├── server.go                       MODIFY — new params, new routes
│   ├── irc_messages_handler.go         NEW — GET /api/irc/{id}/messages
│   ├── irc_names_handler.go            NEW — GET /api/irc/{id}/names
│   ├── browse_handler.go               NEW — GET /api/browse
│   ├── files_handler.go                NEW — GET /api/files
│   ├── errors_handler.go               NEW — GET /api/errors
│   ├── stats_handlers.go               NEW — GET /api/stats/downloads, /api/stats/history
├── main.go                             MODIFY — startup check, wire new buffers
├── deploy/xirc.service                 MODIFY — RestartPreventExitStatus=78
├── web/js/api.js                       MODIFY — new API methods
├── web/js/app.js                       MODIFY — new store state + handlers
├── web/index.html                      MODIFY — all new views and components
```

---

## Task 1: Exit Codes + Startup Directory Check

**Files:**
- Create: `internal/exitcodes/exitcodes.go`
- Modify: `main.go`
- Modify: `deploy/xirc.service`

- [ ] **Step 1: Create exit codes package**

Create `internal/exitcodes/exitcodes.go`:

```go
package exitcodes

// ExitTransient is used for errors that are likely to resolve on retry
// (e.g., NFS mount not yet available). systemd will restart the process.
const ExitTransient = 1

// ExitConfig is used for operator configuration errors that will not
// resolve without manual intervention (e.g., wrong directory ownership).
// Paired with RestartPreventExitStatus=78 in the systemd unit.
const ExitConfig = 78
```

- [ ] **Step 2: Add checkDirectories to main.go**

Add this function to `main.go` (before `func main()`):

```go
func checkDirectories(store db.Store) {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		log.Printf("warning: could not load routing rules for dir check: %v", err)
		return
	}

	seen := map[string]bool{}
	for _, r := range rules {
		if r.DestinationDir == "" || seen[r.DestinationDir] {
			continue
		}
		seen[r.DestinationDir] = true

		probe := r.DestinationDir + "/.xirc_write_check"
		f, err := os.Create(probe)
		if err == nil {
			f.Close()
			os.Remove(probe)
			log.Printf("startup: verified writable: %s", r.DestinationDir)
			continue
		}

		// Classify the error
		if os.IsPermission(err) {
			log.Fatalf("FATAL (config): destination dir %q not writable by xirc user — fix ownership or ACL, then restart (exit 78)", r.DestinationDir)
			os.Exit(exitcodes.ExitConfig)
		}
		if os.IsNotExist(err) {
			log.Fatalf("FATAL (config): destination dir %q does not exist — create it and grant access, then restart (exit 78)", r.DestinationDir)
			os.Exit(exitcodes.ExitConfig)
		}
		// Any other I/O error (EIO, ESTALE on NFS, etc.) is transient
		log.Fatalf("FATAL (transient): destination dir %q check failed: %v — will retry on restart (exit 1)", r.DestinationDir, err)
		os.Exit(exitcodes.ExitTransient)
	}
}
```

Add the import `"github.com/maxwell-xirc/xirc/internal/exitcodes"` and call `checkDirectories(store)` in `main()` after `store.Migrate()` and before `ircMgr.LoadFromStore()`.

- [ ] **Step 3: Update systemd service**

In `deploy/xirc.service`, add after `RestartSec=5`:

```ini
StartLimitIntervalSec=120
StartLimitBurst=5
RestartPreventExitStatus=78
```

- [ ] **Step 4: Build and verify**

```bash
make build
```

Expected: builds with no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/exitcodes/exitcodes.go main.go deploy/xirc.service
git commit -m "feat: add exit codes and startup directory permission check"
```

---

## Task 2: IRC Message Buffer

**Files:**
- Create: `irc/message_buffer.go`
- Create: `irc/message_buffer_test.go`
- Create: `server/irc_messages_handler.go`
- Modify: `server/server.go`
- Modify: `main.go`
- Modify: `web/js/api.js`

- [ ] **Step 1: Write failing tests**

Create `irc/message_buffer_test.go`:

```go
package irc

import (
	"testing"
	"time"
)

func TestMessageBuffer_StoresAndRetrieves(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 10)
	buf.Start()

	bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Nick:     "bob",
		Data:     map[string]string{"message": "hello", "type": "privmsg"},
	})

	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", time.Time{}, 10)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Nick != "bob" || msgs[0].Text != "hello" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

func TestMessageBuffer_BoundedByCapacity(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 3)
	buf.Start()

	for i := 0; i < 5; i++ {
		bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: 1,
			Channel:  "#test",
			Nick:     "bot",
			Data:     map[string]string{"message": "msg", "type": "privmsg"},
		})
	}
	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", time.Time{}, 100)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 (capacity), got %d", len(msgs))
	}
}

func TestMessageBuffer_BeforeFilter(t *testing.T) {
	bus := NewEventBus()
	buf := NewMessageBuffer(bus, 100)
	buf.Start()

	now := time.Now()
	time.Sleep(10 * time.Millisecond)

	bus.Publish(Event{
		Type:     EventIRCMessage,
		ServerID: 1,
		Channel:  "#test",
		Nick:     "bob",
		Data:     map[string]string{"message": "after", "type": "privmsg"},
	})
	time.Sleep(50 * time.Millisecond)

	msgs := buf.GetMessages(1, "#test", now, 10)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages before cutoff, got %d", len(msgs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /path/to/repo && go test ./irc/ -run TestMessageBuffer -v
```

Expected: compile error (MessageBuffer not defined).

- [ ] **Step 3: Implement message buffer**

Create `irc/message_buffer.go`:

```go
package irc

import (
	"fmt"
	"sync"
	"time"
)

// BufferedMessage is one IRC message stored in the ring buffer.
type BufferedMessage struct {
	Timestamp time.Time `json:"timestamp"`
	ServerID  int64     `json:"server_id"`
	Channel   string    `json:"channel"`
	Nick      string    `json:"nick"`
	Text      string    `json:"text"`
	MsgType   string    `json:"msg_type"` // "privmsg" or "notice"
}

// MessageBuffer is an in-memory ring buffer per (server_id, channel).
// It subscribes to the EventBus and buffers EventIRCMessage events.
type MessageBuffer struct {
	mu       sync.RWMutex
	bus      *EventBus
	cap      int
	bufs     map[string][]BufferedMessage
	stopCh   chan struct{}
}

// NewMessageBuffer creates a MessageBuffer with the given per-channel capacity.
func NewMessageBuffer(bus *EventBus, capacity int) *MessageBuffer {
	return &MessageBuffer{
		bus:    bus,
		cap:    capacity,
		bufs:   make(map[string][]BufferedMessage),
		stopCh: make(chan struct{}),
	}
}

// Start begins consuming events from the bus in a background goroutine.
func (mb *MessageBuffer) Start() {
	ch := mb.bus.Subscribe()
	go func() {
		defer mb.bus.Unsubscribe(ch)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.Type != EventIRCMessage {
					continue
				}
				data, ok := ev.Data.(map[string]string)
				if !ok {
					continue
				}
				msg := BufferedMessage{
					Timestamp: time.Now(),
					ServerID:  ev.ServerID,
					Channel:   ev.Channel,
					Nick:      ev.Nick,
					Text:      data["message"],
					MsgType:   data["type"],
				}
				mb.append(ev.ServerID, ev.Channel, msg)
			case <-mb.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the background goroutine.
func (mb *MessageBuffer) Stop() {
	close(mb.stopCh)
}

func (mb *MessageBuffer) key(serverID int64, channel string) string {
	return fmt.Sprintf("%d:%s", serverID, channel)
}

func (mb *MessageBuffer) append(serverID int64, channel string, msg BufferedMessage) {
	k := mb.key(serverID, channel)
	mb.mu.Lock()
	defer mb.mu.Unlock()
	buf := mb.bufs[k]
	buf = append(buf, msg)
	if len(buf) > mb.cap {
		buf = buf[len(buf)-mb.cap:]
	}
	mb.bufs[k] = buf
}

// GetMessages returns up to limit messages with timestamp strictly before
// the before parameter. Pass zero time to get the most recent messages.
func (mb *MessageBuffer) GetMessages(serverID int64, channel string, before time.Time, limit int) []BufferedMessage {
	k := mb.key(serverID, channel)
	mb.mu.RLock()
	buf := mb.bufs[k]
	mb.mu.RUnlock()

	if len(buf) == 0 {
		return nil
	}

	var filtered []BufferedMessage
	for _, m := range buf {
		if before.IsZero() || m.Timestamp.Before(before) {
			filtered = append(filtered, m)
		}
	}

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./irc/ -run TestMessageBuffer -v
```

Expected: all PASS.

- [ ] **Step 5: Create HTTP handler**

Create `server/irc_messages_handler.go`:

```go
package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GET /api/irc/{server_id}/messages?channel=&before=RFC3339&limit=200
func (s *Server) handleIRCMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.msgBuf == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	// Extract server_id from path: /api/irc/{server_id}/messages
	path := strings.TrimPrefix(r.URL.Path, "/api/irc/")
	path = strings.TrimSuffix(path, "/messages")
	serverID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}

	channel := r.URL.Query().Get("channel")
	beforeStr := r.URL.Query().Get("before")
	limitStr := r.URL.Query().Get("limit")

	var before time.Time
	if beforeStr != "" {
		before, err = time.Parse(time.RFC3339Nano, beforeStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before parameter")
			return
		}
	}

	limit := 200
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	msgs := s.msgBuf.GetMessages(serverID, channel, before, limit)
	if msgs == nil {
		msgs = []irc.BufferedMessage{}
	}
	writeJSON(w, http.StatusOK, msgs)
}
```

Add import `"github.com/maxwell-xirc/xirc/irc"` to the file.

- [ ] **Step 6: Wire into server**

In `server/server.go`, add `msgBuf *irc.MessageBuffer` field to `Server` struct and update `New()`. Do NOT add `errBuf` yet — `irc.ErrorBuffer` is defined in Task 3; add it there.

```go
type Server struct {
	store  db.Store
	ircMgr *irc.Manager
	parser *parser.Parser
	engine *queue.Engine
	wsHub  *ws.Hub
	msgBuf *irc.MessageBuffer
	// errBuf added in Task 3
	mux    *http.ServeMux
}

func New(store db.Store, ircMgr *irc.Manager, p *parser.Parser, eng *queue.Engine, hub *ws.Hub, msgBuf *irc.MessageBuffer) *Server {
```

Add this route in `routes()`:

```go
s.mux.HandleFunc("/api/irc/messages", s.handleIRCMessages)
```

Wait — the path is `/api/irc/{id}/messages`. With Go 1.13's ServeMux, a trailing slash prefix-matches. Register as:

```go
s.mux.HandleFunc("/api/irc/", s.handleIRCDispatch)
```

Create `server/irc_dispatch.go`:

```go
package server

import (
	"net/http"
	"strings"
)

// handleIRCDispatch routes /api/irc/{id}/messages and /api/irc/{id}/names
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
```

Remove the existing individual `/api/irc/status`, `/api/irc/connect`, etc. routes — those paths don't contain a second segment, so they won't match `/api/irc/` incorrectly. Actually, they will conflict with the `/api/irc/` prefix handler. Fix by registering the specific paths BEFORE the prefix:

```go
// IRC endpoints (exact paths first, then prefix for parameterised routes)
s.mux.HandleFunc("/api/irc/status", s.handleIRCStatus)
s.mux.HandleFunc("/api/irc/connect", s.handleIRCConnect)
s.mux.HandleFunc("/api/irc/disconnect", s.handleIRCDisconnect)
s.mux.HandleFunc("/api/irc/message", s.handleIRCSendMessage)
s.mux.HandleFunc("/api/irc/raw", s.handleIRCSendRaw)
s.mux.HandleFunc("/api/irc/", s.handleIRCDispatch)  // must be last
```

Go's ServeMux gives priority to longer/more-specific patterns, so `/api/irc/status` beats `/api/irc/`.

- [ ] **Step 7: Update main.go**

In `main.go`, after `bus := ircpkg.NewEventBus()`:

```go
msgBuf := ircpkg.NewMessageBuffer(bus, 1000)
msgBuf.Start()
defer msgBuf.Stop()
```

Update `server.New(...)` call to pass `msgBuf` (errBuf is added in Task 3).

- [ ] **Step 8: Add API client method**

In `web/js/api.js`, add to the api object:

```js
getIRCMessages(serverId, channel, before, limit) {
    let q = `/irc/${serverId}/messages?channel=${encodeURIComponent(channel)}`;
    if (before) q += `&before=${encodeURIComponent(before)}`;
    if (limit)  q += `&limit=${limit}`;
    return this.get(q);
},
```

- [ ] **Step 9: Build and test**

```bash
go test ./irc/ -run TestMessageBuffer -v
make build
```

Expected: tests pass, binary builds.

- [ ] **Step 10: Commit**

```bash
git add irc/message_buffer.go irc/message_buffer_test.go server/irc_messages_handler.go server/irc_dispatch.go server/server.go main.go web/js/api.js
git commit -m "feat: add IRC message buffer and /api/irc/{id}/messages endpoint"
```

---

## Task 3: Error Buffer + Endpoint

**Files:**
- Modify: `irc/events.go`
- Create: `irc/error_buffer.go`
- Create: `irc/error_buffer_test.go`
- Create: `server/errors_handler.go`
- Modify: `server/server.go`
- Modify: `irc/connection.go`
- Modify: `web/js/api.js`

- [ ] **Step 1: Add EventError to events.go**

In `irc/events.go`, add:

```go
const (
	// existing constants ...
	EventError EventType = "error_event"
)

// ErrorEvent is the payload for EventError bus events.
type ErrorEvent struct {
	ErrorType string `json:"error_type"` // "irc_disconnect", "download_failed", "hook_failed"
	ServerID  int64  `json:"server_id,omitempty"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"` // RFC3339
}
```

- [ ] **Step 2: Write failing tests**

Create `irc/error_buffer_test.go`:

```go
package irc

import (
	"testing"
	"time"
)

func TestErrorBuffer_StoresEvent(t *testing.T) {
	bus := NewEventBus()
	buf := NewErrorBuffer(bus, 10)
	buf.Start()

	bus.Publish(Event{
		Type:     EventError,
		ServerID: 1,
		Data: ErrorEvent{
			ErrorType: "irc_disconnect",
			ServerID:  1,
			Message:   "connection reset",
			Timestamp: time.Now().Format(time.RFC3339),
		},
	})
	time.Sleep(50 * time.Millisecond)

	errs := buf.GetErrors(50)
	if len(errs) != 1 {
		t.Fatalf("expected 1, got %d", len(errs))
	}
	if errs[0].Message != "connection reset" {
		t.Errorf("unexpected message: %s", errs[0].Message)
	}
}

func TestErrorBuffer_BoundedByCapacity(t *testing.T) {
	bus := NewEventBus()
	buf := NewErrorBuffer(bus, 3)
	buf.Start()

	for i := 0; i < 5; i++ {
		bus.Publish(Event{
			Type: EventError,
			Data: ErrorEvent{ErrorType: "test", Message: "err", Timestamp: time.Now().Format(time.RFC3339)},
		})
	}
	time.Sleep(50 * time.Millisecond)

	if len(buf.GetErrors(100)) != 3 {
		t.Fatalf("expected 3 (capacity)")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./irc/ -run TestErrorBuffer -v
```

Expected: compile error.

- [ ] **Step 4: Implement error buffer**

Create `irc/error_buffer.go`:

```go
package irc

import (
	"sync"
)

// ErrorBuffer is an in-memory ring buffer for ErrorEvent values.
// It subscribes to the EventBus and stores EventError events.
type ErrorBuffer struct {
	mu     sync.RWMutex
	bus    *EventBus
	cap    int
	items  []ErrorEvent
	stopCh chan struct{}
}

func NewErrorBuffer(bus *EventBus, capacity int) *ErrorBuffer {
	return &ErrorBuffer{
		bus:    bus,
		cap:    capacity,
		stopCh: make(chan struct{}),
	}
}

func (eb *ErrorBuffer) Start() {
	ch := eb.bus.Subscribe()
	go func() {
		defer eb.bus.Unsubscribe(ch)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.Type != EventError {
					continue
				}
				errEv, ok := ev.Data.(ErrorEvent)
				if !ok {
					continue
				}
				eb.mu.Lock()
				eb.items = append(eb.items, errEv)
				if len(eb.items) > eb.cap {
					eb.items = eb.items[len(eb.items)-eb.cap:]
				}
				eb.mu.Unlock()
			case <-eb.stopCh:
				return
			}
		}
	}()
}

func (eb *ErrorBuffer) Stop() {
	close(eb.stopCh)
}

// GetErrors returns up to limit recent errors (most recent last).
func (eb *ErrorBuffer) GetErrors(limit int) []ErrorEvent {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if len(eb.items) == 0 {
		return nil
	}
	out := make([]ErrorEvent, len(eb.items))
	copy(out, eb.items)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
```

- [ ] **Step 5: Publish errors on IRC disconnect failure**

In `irc/connection.go`, in `connectLoop()`, after logging the connection failure:

```go
if err != nil {
	// existing log line...
	c.bus.Publish(Event{
		Type:     EventError,
		ServerID: c.server.ID,
		Data: ErrorEvent{
			ErrorType: "irc_disconnect",
			ServerID:  c.server.ID,
			Message:   fmt.Sprintf("[%s] connection failed: %v", c.server.Name, err),
			Timestamp: time.Now().Format(time.RFC3339),
		},
	})
	// existing setStatus + delay...
}
```

- [ ] **Step 6: Create errors REST handler**

Create `server/errors_handler.go`:

```go
package server

import (
	"net/http"
	"strconv"
)

// GET /api/errors?limit=50
func (s *Server) handleGetErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	errs := s.errBuf.GetErrors(limit)
	if errs == nil {
		errs = []irc.ErrorEvent{}
	}
	writeJSON(w, http.StatusOK, errs)
}
```

Add import `"github.com/maxwell-xirc/xirc/irc"`.

- [ ] **Step 7: Add errBuf to server.Server and wire in main.go**

In `server/server.go`, add `errBuf *irc.ErrorBuffer` to the `Server` struct (the `// errBuf added in Task 3` placeholder) and update `New()` signature:

```go
type Server struct {
	// existing fields...
	msgBuf *irc.MessageBuffer
	errBuf *irc.ErrorBuffer
	mux    *http.ServeMux
}

func New(store db.Store, ircMgr *irc.Manager, p *parser.Parser, eng *queue.Engine, hub *ws.Hub, msgBuf *irc.MessageBuffer, errBuf *irc.ErrorBuffer) *Server {
```

In `main.go`, after the msgBuf block, add:

```go
errBuf := ircpkg.NewErrorBuffer(bus, 200)
errBuf.Start()
defer errBuf.Stop()
```

Update `server.New(...)` call to pass both `msgBuf, errBuf`.

Update any `server.New(...)` calls in `server/server_test.go` to pass `nil, nil` for the two new buffer params.

- [ ] **Step 8: Register route**

In `server/server.go` `routes()`, add:

```go
s.mux.HandleFunc("/api/errors", s.handleGetErrors)
```

- [ ] **Step 8: Add API client method**

In `web/js/api.js`:

```js
getErrors(limit) {
    return this.get('/errors' + (limit ? '?limit=' + limit : ''));
},
```

- [ ] **Step 9: Run tests and build**

```bash
go test ./irc/ -run TestErrorBuffer -v
make build
```

Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add irc/events.go irc/error_buffer.go irc/error_buffer_test.go irc/connection.go server/errors_handler.go server/server.go web/js/api.js
git commit -m "feat: add error event buffer and /api/errors endpoint"
```

---

## Task 4: IRC Connection Stats + NAMES Endpoint

**Files:**
- Modify: `irc/client.go` — add `SendLine(line string)` to IRCClient
- Modify: `irc/rawclient.go` — implement `SendLine`
- Modify: `irc/connection.go` — track ConnectedAt/ReconnectCount/LagMs, implement Names()
- Modify: `irc/manager.go` — extend ServerStatus, expose Names()
- Create: `server/irc_names_handler.go`
- Modify: `server/irc_dispatch.go` — already routes /names
- Modify: `web/js/api.js`

- [ ] **Step 1: Add SendLine to IRCClient interface**

In `irc/client.go`, add to the IRCClient interface:

```go
// SendLine writes a raw IRC protocol line (without CRLF — the implementation adds it).
// Use for protocol-level messages like PING that have no dedicated method.
SendLine(line string)
```

- [ ] **Step 2: Implement SendLine in RawClient**

In `irc/rawclient.go`, add after the existing method implementations:

```go
func (c *RawClient) SendLine(line string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "%s\r\n", line)
}
```

- [ ] **Step 3: Extend Connection with stats tracking**

In `irc/connection.go`, add fields to the `Connection` struct:

```go
type Connection struct {
	// existing fields ...
	connectedAt    *time.Time
	reconnectCount int
	lagMs          int64
	namesMu        sync.Mutex
	namesPending   map[string]chan []string // channel -> result chan
}
```

Update `NewConnection` to initialise `namesPending`:

```go
func NewConnection(server *db.Server, channels []db.Channel, bus *EventBus) *Connection {
	return &Connection{
		server:       server,
		channels:     channels,
		bus:          bus,
		status:       StatusDisconnected,
		stopCh:       make(chan struct{}),
		namesPending: make(map[string]chan []string),
	}
}
```

In `applyHandlers`, extend `OnConnect`:

```go
client.OnConnect(func() {
	now := time.Now()
	c.mu.Lock()
	c.connectedAt = &now
	c.mu.Unlock()
	c.setStatus(StatusConnected)
	// ... existing auto-join code ...

	// Start PING ticker for lag measurement
	go c.pingLoop(client)
})
```

In `applyHandlers`, add `OnRaw` handler for PONG + NAMES replies:

```go
client.OnRaw(func(line string) {
	c.handleRawLine(line)
})
```

Add increment in `connectLoop` (after each reconnect attempt > 0):

```go
// At top of the for loop in connectLoop, before setStatus(Connecting):
if attempt > 0 {
	c.mu.Lock()
	c.reconnectCount++
	c.mu.Unlock()
}
```

Add these new methods to `connection.go`:

```go
func (c *Connection) pingLoop(client IRCClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sent := time.Now().UnixNano()
			token := fmt.Sprintf("xirc%d", sent)
			c.mu.Lock()
			// store sent time by token — reuse lagMs slot with a simple approach
			c.lagMs = -sent // negative = ping in flight; will be set positive on PONG
			c.mu.Unlock()
			client.SendLine("PING :" + token)
		case <-c.stopCh:
			return
		}
	}
}

func (c *Connection) handleRawLine(line string) {
	// PONG :<token>
	if strings.HasPrefix(line, "PONG ") {
		c.mu.Lock()
		if c.lagMs < 0 {
			sentNano := -c.lagMs
			c.lagMs = (time.Now().UnixNano() - sentNano) / int64(time.Millisecond)
		}
		c.mu.Unlock()
		return
	}

	// :server 353 nick = #channel :nick1 nick2 ...
	// :server 366 nick #channel :End of /NAMES list
	parts := strings.SplitN(line, " ", 5)
	if len(parts) < 4 {
		return
	}
	code := parts[1]
	switch code {
	case "353":
		// parts[4] is ":nick1 nick2 ..."
		if len(parts) < 5 {
			return
		}
		// channel is parts[3]
		ch := parts[3]
		nicks := strings.Fields(strings.TrimPrefix(parts[4], ":"))
		c.namesMu.Lock()
		if resCh, ok := c.namesPending[ch]; ok {
			resCh <- nicks
		}
		c.namesMu.Unlock()
	case "366":
		ch := parts[3]
		c.namesMu.Lock()
		if resCh, ok := c.namesPending[ch]; ok {
			close(resCh)
			delete(c.namesPending, ch)
		}
		c.namesMu.Unlock()
	}
}

// Names sends a NAMES command and collects the response (timeout 5s).
func (c *Connection) Names(channel string) ([]string, error) {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return nil, fmt.Errorf("not connected")
	}

	resCh := make(chan []string, 20)
	c.namesMu.Lock()
	c.namesPending[channel] = resCh
	c.namesMu.Unlock()

	client.SendLine("NAMES " + channel)

	var nicks []string
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case batch, ok := <-resCh:
			if !ok {
				return nicks, nil
			}
			nicks = append(nicks, batch...)
		case <-timer.C:
			c.namesMu.Lock()
			delete(c.namesPending, channel)
			c.namesMu.Unlock()
			return nicks, fmt.Errorf("names timeout")
		}
	}
}

// Stats returns current connection statistics.
func (c *Connection) Stats() (connectedAt *time.Time, reconnectCount int, lagMs int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	lag := c.lagMs
	if lag < 0 {
		lag = 0 // ping in flight, report last known value as 0
	}
	return c.connectedAt, c.reconnectCount, lag
}
```

- [ ] **Step 4: Extend ServerStatus and Manager**

In `irc/manager.go`, update `ServerStatus`:

```go
type ServerStatus struct {
	ServerID       int64            `json:"server_id"`
	ServerName     string           `json:"server_name"`
	Status         ConnectionStatus `json:"status"`
	Channels       []string         `json:"channels"`
	ConnectedAt    *time.Time       `json:"connected_at,omitempty"`
	ReconnectCount int              `json:"reconnect_count"`
	LagMs          int64            `json:"lag_ms"`
}
```

Update `GetStatuses()` to populate new fields:

```go
func (m *Manager) GetStatuses() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []ServerStatus
	for _, conn := range m.connections {
		connectedAt, reconnectCount, lagMs := conn.Stats()
		statuses = append(statuses, ServerStatus{
			ServerID:       conn.ServerID(),
			ServerName:     conn.ServerName(),
			Status:         conn.Status(),
			Channels:       conn.AllChannelNames(),
			ConnectedAt:    connectedAt,
			ReconnectCount: reconnectCount,
			LagMs:          lagMs,
		})
	}
	return statuses
}
```

Add `Names` proxy to Manager:

```go
func (m *Manager) Names(serverID int64, channel string) ([]string, error) {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return nil, fmt.Errorf("server %d not found", serverID)
	}
	return conn.Names(channel)
}
```

- [ ] **Step 5: Create NAMES handler**

Create `server/irc_names_handler.go`:

```go
package server

import (
	"net/http"
	"strconv"
	"strings"
)

// GET /api/irc/{server_id}/names?channel=#chan
func (s *Server) handleIRCNames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/irc/")
	path = strings.TrimSuffix(path, "/names")
	serverID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server id")
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		writeError(w, http.StatusBadRequest, "channel required")
		return
	}

	if s.ircMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "IRC manager not available")
		return
	}

	nicks, err := s.ircMgr.Names(serverID, channel)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"channel": channel,
		"nicks":   nicks,
	})
}
```

- [ ] **Step 6: Add API client methods**

In `web/js/api.js`:

```js
getIRCNames(serverId, channel) {
    return this.get(`/irc/${serverId}/names?channel=${encodeURIComponent(channel)}`);
},
```

- [ ] **Step 7: Build**

```bash
make build
```

Expected: builds cleanly.

- [ ] **Step 8: Commit**

```bash
git add irc/client.go irc/rawclient.go irc/connection.go irc/manager.go server/irc_names_handler.go web/js/api.js
git commit -m "feat: add IRC connection stats, PING/PONG lag, and NAMES endpoint"
```

---

## Task 5: Browse + Files Endpoints

**Files:**
- Create: `server/browse_handler.go`
- Create: `server/files_handler.go`
- Modify: `server/server.go`
- Modify: `web/js/api.js`

- [ ] **Step 1: Create browse handler**

Create `server/browse_handler.go`:

```go
package server

import (
	"net/http"
	"os"
	"path/filepath"
)

type browseEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

type browseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	Entries []browseEntry `json:"entries"`
}

// GET /api/browse?path=/srv/downloads
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	raw := r.URL.Query().Get("path")
	if raw == "" {
		raw = "/"
	}

	// Sanitise: clean and resolve symlinks to prevent traversal
	clean := filepath.Clean(raw)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "path not found")
		} else {
			writeError(w, http.StatusBadRequest, "invalid path")
		}
		return
	}

	entries, err := readDirEntries(resolved)
	if err != nil {
		if os.IsPermission(err) {
			writeError(w, http.StatusForbidden, "permission denied")
		} else {
			writeError(w, http.StatusInternalServerError, "could not read directory")
		}
		return
	}

	parent := filepath.Dir(resolved)
	if parent == resolved {
		parent = "" // at filesystem root
	}

	writeJSON(w, http.StatusOK, browseResponse{
		Path:    resolved,
		Parent:  parent,
		Entries: entries,
	})
}

func readDirEntries(path string) ([]browseEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}

	var entries []browseEntry
	for _, info := range infos {
		if info.Name() == "" || info.Name()[0] == '.' {
			continue // skip hidden
		}
		entries = append(entries, browseEntry{
			Name:  info.Name(),
			IsDir: info.IsDir(),
		})
	}
	return entries, nil
}
```

- [ ] **Step 2: Create files handler**

Create `server/files_handler.go`:

```go
package server

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type fileEntry struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type filesResponse struct {
	Dir   string      `json:"dir"`
	Files []fileEntry `json:"files"`
}

// GET /api/files?dir=/srv/downloads
// Only serves directories configured as destination_dir in routing rules.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeError(w, http.StatusBadRequest, "dir required")
		return
	}

	// Security: only serve dirs that are configured routing destinations
	allowed, err := s.isAllowedDir(dir)
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "not a configured destination directory")
		return
	}

	clean := filepath.Clean(dir)
	f, err := os.Open(clean)
	if err != nil {
		if os.IsPermission(err) {
			writeError(w, http.StatusForbidden, "permission denied")
		} else {
			writeError(w, http.StatusNotFound, "directory not found")
		}
		return
	}
	defer f.Close()

	infos, err := f.Readdir(-1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read directory")
		return
	}

	var files []fileEntry
	for _, info := range infos {
		if info.IsDir() {
			continue
		}
		files = append(files, fileEntry{
			Name:     info.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC(),
		})
	}
	if files == nil {
		files = []fileEntry{}
	}
	writeJSON(w, http.StatusOK, filesResponse{Dir: clean, Files: files})
}

func (s *Server) isAllowedDir(dir string) (bool, error) {
	rules, err := s.store.GetAllFileRoutingRules()
	if err != nil {
		return false, err
	}
	cleanDir := filepath.Clean(dir)
	for _, r := range rules {
		if filepath.Clean(r.DestinationDir) == cleanDir {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 3: Register routes**

In `server/server.go` `routes()`, add:

```go
s.mux.HandleFunc("/api/browse", s.handleBrowse)
s.mux.HandleFunc("/api/files", s.handleFiles)
```

- [ ] **Step 4: Add API client methods**

In `web/js/api.js`:

```js
browseDir(path)  { return this.get('/browse?path=' + encodeURIComponent(path || '/')); },
listFiles(dir)   { return this.get('/files?dir=' + encodeURIComponent(dir)); },
```

- [ ] **Step 5: Build**

```bash
make build
```

- [ ] **Step 6: Commit**

```bash
git add server/browse_handler.go server/files_handler.go server/server.go web/js/api.js
git commit -m "feat: add /api/browse and /api/files endpoints"
```

---

## Task 6: Download Stats Table + Engine Changes

**Files:**
- Modify: `db/models.go`
- Modify: `db/migrations.go`
- Modify: `db/store.go`
- Modify: `db/sqlite.go`
- Modify: `db/mysql.go`
- Modify: `queue/engine.go` (find and update completion/failure handling)
- Create: `server/stats_handlers.go`
- Modify: `server/server.go`
- Modify: `web/js/api.js`

- [ ] **Step 1: Add DownloadStat model and StatsOnly to Download**

In `db/models.go`, add `StatsOnly bool` to `Download`:

```go
type Download struct {
	// ... existing fields ...
	StatsOnly bool `json:"stats_only"`
}
```

Add new `DownloadStat` struct:

```go
type DownloadStat struct {
	ID          int64      `json:"id"`
	Filename    string     `json:"filename"`
	SizeBytes   int64      `json:"size_bytes"`
	ServerID    int64      `json:"server_id"`
	Channel     string     `json:"channel"`
	BotNick     string     `json:"bot_nick"`
	PackNumber  int        `json:"pack_number"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Status      string     `json:"status"` // "completed", "failed", "cancelled", "stats_only"
	StatsOnly   bool       `json:"stats_only"`
}

type DownloadStatsSummary struct {
	TotalTransfers int64   `json:"total_transfers"`
	SuccessRate    float64 `json:"success_rate"`
	TotalBytes     int64   `json:"total_bytes"`
	TotalSaved     int64   `json:"total_saved"` // bytes kept on disk (not stats_only)
}
```

- [ ] **Step 2: Add migrations**

In `db/migrations.go`, add to `migrationStatements()`:

```go
`CREATE TABLE IF NOT EXISTS download_stats (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    filename     TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    server_id    INTEGER NOT NULL DEFAULT 0,
    channel      TEXT NOT NULL DEFAULT '',
    bot_nick     TEXT NOT NULL DEFAULT '',
    pack_number  INTEGER NOT NULL DEFAULT 0,
    started_at   DATETIME,
    completed_at DATETIME,
    status       TEXT NOT NULL DEFAULT 'completed',
    stats_only   INTEGER NOT NULL DEFAULT 0
)`,

`ALTER TABLE downloads ADD COLUMN stats_only INTEGER NOT NULL DEFAULT 0`,
```

Note: SQLite silently ignores `ALTER TABLE ... ADD COLUMN` if the column already exists when using `IF NOT EXISTS` — but standard SQL doesn't have `IF NOT EXISTS` for ADD COLUMN. Wrap the ALTER in a helper that ignores "duplicate column" errors:

In `db/migrations.go`, update `runMigrations`:

```go
func runMigrations(db *sql.DB) error {
	for _, stmt := range migrationStatements() {
		if _, err := db.Exec(stmt); err != nil {
			// Ignore "duplicate column" errors from ALTER TABLE ADD COLUMN
			// so migrations are idempotent on re-run.
			msg := err.Error()
			if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "Duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}
```

Add `"strings"` import.

- [ ] **Step 3: Extend Store interface**

In `db/store.go`, add:

```go
// Download Stats
CreateDownloadStat(s *DownloadStat) error
GetDownloadStatsSummary() (*DownloadStatsSummary, error)
GetDownloadHistory(offset, limit int) ([]DownloadStat, error)
```

- [ ] **Step 4: Implement in SQLite**

In `db/sqlite.go`, add:

```go
func (s *SQLiteStore) CreateDownloadStat(stat *DownloadStat) error {
	_, err := s.db.Exec(
		`INSERT INTO download_stats (filename,size_bytes,server_id,channel,bot_nick,pack_number,started_at,completed_at,status,stats_only)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		stat.Filename, stat.SizeBytes, stat.ServerID, stat.Channel, stat.BotNick,
		stat.PackNumber, stat.StartedAt, stat.CompletedAt, stat.Status, boolToInt(stat.StatsOnly),
	)
	return err
}

func (s *SQLiteStore) GetDownloadStatsSummary() (*DownloadStatsSummary, error) {
	row := s.db.QueryRow(`
		SELECT
			COUNT(*),
			SUM(CASE WHEN status='completed' OR status='stats_only' THEN 1 ELSE 0 END),
			COALESCE(SUM(size_bytes),0),
			COALESCE(SUM(CASE WHEN stats_only=0 AND status='completed' THEN size_bytes ELSE 0 END),0)
		FROM download_stats`)
	var total, success int64
	var totalBytes, totalSaved int64
	if err := row.Scan(&total, &success, &totalBytes, &totalSaved); err != nil {
		return nil, err
	}
	var rate float64
	if total > 0 {
		rate = float64(success) / float64(total) * 100
	}
	return &DownloadStatsSummary{
		TotalTransfers: total,
		SuccessRate:    rate,
		TotalBytes:     totalBytes,
		TotalSaved:     totalSaved,
	}, nil
}

func (s *SQLiteStore) GetDownloadHistory(offset, limit int) ([]DownloadStat, error) {
	rows, err := s.db.Query(
		`SELECT id,filename,size_bytes,server_id,channel,bot_nick,pack_number,started_at,completed_at,status,stats_only
		 FROM download_stats ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DownloadStat
	for rows.Next() {
		var d DownloadStat
		var statsOnly int
		if err := rows.Scan(&d.ID, &d.Filename, &d.SizeBytes, &d.ServerID, &d.Channel,
			&d.BotNick, &d.PackNumber, &d.StartedAt, &d.CompletedAt, &d.Status, &statsOnly); err != nil {
			return nil, err
		}
		d.StatsOnly = statsOnly == 1
		out = append(out, d)
	}
	return out, rows.Err()
}
```

Add the same three methods to `db/mysql.go` with identical logic (MySQL syntax is compatible here).

- [ ] **Step 5: Record stats on download completion**

Find `queue/engine.go`. Locate where download status is set to `"completed"` or `"failed"`. After each status update, call `store.CreateDownloadStat`. Also: if `dl.StatsOnly` is true and status is `"completed"`, call `os.Remove(dl.DestinationPath)` and record status as `"stats_only"`.

The exact location depends on the engine implementation — look for calls to `store.UpdateDownload` with `status = "completed"` or `status = "failed"`. Add after each:

```go
now := time.Now()
stat := &db.DownloadStat{
    Filename:    dl.Filename,
    SizeBytes:   dl.Filesize,
    ServerID:    dl.ServerID,
    Channel:     dl.Channel,
    BotNick:     dl.BotNick,
    PackNumber:  dl.PackNumber,
    StartedAt:   dl.StartedAt,
    CompletedAt: &now,
    Status:      dl.Status,
    StatsOnly:   dl.StatsOnly,
}
if dl.StatsOnly && dl.Status == "completed" {
    os.Remove(dl.DestinationPath)
    stat.Status = "stats_only"
}
if err := eng.store.CreateDownloadStat(stat); err != nil {
    log.Printf("warning: could not record download stat: %v", err)
}
```

- [ ] **Step 6: Create stats handlers**

Create `server/stats_handlers.go`:

```go
package server

import (
	"net/http"
	"strconv"
)

// GET /api/stats/downloads
func (s *Server) handleDownloadStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	summary, err := s.store.GetDownloadStatsSummary()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load stats")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// GET /api/stats/history?offset=0&limit=50
func (s *Server) handleDownloadHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	history, err := s.store.GetDownloadHistory(offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load history")
		return
	}
	if history == nil {
		history = []db.DownloadStat{}
	}
	writeJSON(w, http.StatusOK, history)
}
```

Add import `"github.com/maxwell-xirc/xirc/db"`.

- [ ] **Step 7: Register routes**

In `server/server.go` `routes()`:

```go
s.mux.HandleFunc("/api/stats/downloads", s.handleDownloadStats)
s.mux.HandleFunc("/api/stats/history", s.handleDownloadHistory)
```

- [ ] **Step 8: Add API client methods**

In `web/js/api.js`:

```js
getDownloadStats()                { return this.get('/stats/downloads'); },
getDownloadHistory(offset, limit) {
    return this.get(`/stats/history?offset=${offset||0}&limit=${limit||50}`);
},
```

- [ ] **Step 9: Test and build**

```bash
go test ./db/ -v
make build
```

Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add db/models.go db/migrations.go db/store.go db/sqlite.go db/mysql.go queue/engine.go server/stats_handlers.go server/server.go web/js/api.js
git commit -m "feat: add download_stats table, stats-only mode, and stats REST endpoints"
```

---

## Task 7: Alpine Store Extensions

All frontend tasks modify `web/js/app.js` and `web/index.html`. Read both files fully before starting any frontend task.

**Files:**
- Modify: `web/js/app.js`
- Modify: `web/js/api.js` (minor additions)

- [ ] **Step 1: Add new state variables to appStore**

In `app.js`, add to the initial state object (inside `Alpine.data('appStore', () => ({`):

```js
// Mode
appMode: localStorage.getItem('xirc_mode') || 'simple',

// Active server (for server view)
activeServerObj: null,

// Server view
serverMessages: {},  // keyed by server_id, value: [{timestamp,nick,text,msg_type}]
serverMsgLoading: {},

// Channel view
channelLayout: {},        // keyed by "serverId:channel", value: 'single'|'sidebyside'
activeChannelTab: {},     // keyed by key, value: 'search'|'download'
userLists: {},            // keyed by key, value: [nick, ...]
userListLoading: {},
selectedUser: null,

// Dir picker
dirPickerOpen: false,
dirPickerPath: '/',
dirPickerEntries: [],
dirPickerCallback: null,

// File manager
fileManagerDir: null,
fileManagerFiles: [],
fileManagerLoading: false,

// Errors
errors: [],
unreadErrors: 0,

// Stats
statsSummary: null,
statsHistory: [],
statsHistoryOffset: 0,
statsLoading: false,

// Sidebar mobile
sidebarOpen: false,

// Stats-only setting
statsOnlyDefault: false,
```

- [ ] **Step 2: Add setMode method**

```js
setMode(mode) {
    this.appMode = mode;
    localStorage.setItem('xirc_mode', mode);
},
```

- [ ] **Step 3: Add server view methods**

```js
async selectServer(server) {
    this.activeServerObj = server;
    this.activeServer = server.id;
    this.activeChannel = null;
    this.activeView = 'server';
    await this.loadServerMessages(server.id);
},

async loadServerMessages(serverId) {
    this.serverMsgLoading[serverId] = true;
    try {
        const msgs = await api.getIRCMessages(serverId, '', null, 200);
        this.serverMessages[serverId] = Array.isArray(msgs) ? msgs : [];
    } catch(e) {
        console.error('loadServerMessages', e);
    } finally {
        this.serverMsgLoading[serverId] = false;
    }
},

async loadMoreServerMessages(serverId) {
    const existing = this.serverMessages[serverId] || [];
    if (existing.length === 0) return;
    const oldest = existing[0].timestamp;
    try {
        const msgs = await api.getIRCMessages(serverId, '', oldest, 200);
        if (msgs && msgs.length > 0) {
            this.serverMessages[serverId] = msgs.concat(existing);
        }
    } catch(e) {
        console.error('loadMoreServerMessages', e);
    }
},
```

- [ ] **Step 4: Add channel layout methods**

```js
channelLayout(key) {
    return this._channelLayout[key] || 'single';
},
setChannelLayout(key, layout) {
    this._channelLayout = Object.assign({}, this._channelLayout, {[key]: layout});
    localStorage.setItem('xirc_layout_' + key, layout);
},
activeTab(key) {
    return this.activeChannelTab[key] || 'search';
},
setActiveTab(key, tab) {
    this.activeChannelTab = Object.assign({}, this.activeChannelTab, {[key]: tab});
},
```

Note: rename the existing `channelLayout` object field to `_channelLayout` to avoid name collision with the method.

- [ ] **Step 5: Add user list methods**

```js
async loadUserList(serverId, channel) {
    const key = this.channelKey(serverId, channel);
    this.userListLoading[key] = true;
    try {
        const res = await api.getIRCNames(serverId, channel);
        this.userLists[key] = res && res.nicks ? res.nicks : [];
    } catch(e) {
        console.error('loadUserList', e);
    } finally {
        this.userListLoading[key] = false;
    }
},

selectUser(nick) {
    this.selectedUser = this.selectedUser === nick ? null : nick;
},

isUserSelected(nick) {
    return this.selectedUser === nick;
},

userHighlightClass(nick) {
    return this.selectedUser && nick === this.selectedUser ? 'bg-yellow-100 font-bold' : '';
},
```

- [ ] **Step 6: Add IRC message scroll-load + WS buffer hookup**

Update `_handleWsEvent` in app.js: after pushing to `this.ircMessages[key]`, also push to server messages buffer when channel is `""`:

```js
if (data.channel === '' || data.channel === null) {
    if (!this.serverMessages[data.server_id]) {
        this.serverMessages[data.server_id] = [];
    }
    this.serverMessages[data.server_id].push({
        nick: data.nick,
        message: data.message || (data.data && data.data.message) || '',
        timestamp: data.timestamp,
    });
    if (this.serverMessages[data.server_id].length > 500) {
        this.serverMessages[data.server_id].splice(0, this.serverMessages[data.server_id].length - 500);
    }
}
```

Also handle `error_event` WS type:

```js
} else if (type === 'error_event') {
    this.errors.unshift(data.data || data);
    if (this.errors.length > 200) this.errors.pop();
    this.unreadErrors++;
}
```

- [ ] **Step 7: Add dir picker methods**

```js
async openDirPicker(currentPath, callback) {
    this.dirPickerCallback = callback;
    this.dirPickerOpen = true;
    await this.browseDir(currentPath || '/');
},

async browseDir(path) {
    try {
        const res = await api.browseDir(path);
        this.dirPickerPath = res.path;
        this.dirPickerEntries = res.entries || [];
    } catch(e) {
        console.error('browseDir', e);
    }
},

async browseDirUp() {
    const parent = this.dirPickerPath.split('/').slice(0, -1).join('/') || '/';
    await this.browseDir(parent);
},

selectDir() {
    if (this.dirPickerCallback) {
        this.dirPickerCallback(this.dirPickerPath);
    }
    this.dirPickerOpen = false;
    this.dirPickerCallback = null;
},

cancelDirPicker() {
    this.dirPickerOpen = false;
    this.dirPickerCallback = null;
},
```

- [ ] **Step 8: Add file manager, stats, and error methods**

```js
async loadFileManager(dir) {
    this.fileManagerDir = dir;
    this.fileManagerLoading = true;
    try {
        const res = await api.listFiles(dir);
        this.fileManagerFiles = res && res.files ? res.files : [];
    } catch(e) {
        console.error('loadFileManager', e);
    } finally {
        this.fileManagerLoading = false;
    }
},

async loadStats() {
    this.statsLoading = true;
    try {
        const [summary, history] = await Promise.all([
            api.getDownloadStats(),
            api.getDownloadHistory(0, 50),
        ]);
        this.statsSummary = summary;
        this.statsHistory = Array.isArray(history) ? history : [];
        this.statsHistoryOffset = 50;
    } catch(e) {
        console.error('loadStats', e);
    } finally {
        this.statsLoading = false;
    }
},

async loadMoreHistory() {
    try {
        const more = await api.getDownloadHistory(this.statsHistoryOffset, 50);
        if (more && more.length > 0) {
            this.statsHistory = this.statsHistory.concat(more);
            this.statsHistoryOffset += more.length;
        }
    } catch(e) {
        console.error('loadMoreHistory', e);
    }
},

async loadErrors() {
    try {
        const errs = await api.getErrors(100);
        this.errors = Array.isArray(errs) ? errs : [];
    } catch(e) {
        console.error('loadErrors', e);
    }
},

clearErrors() {
    this.errors = [];
    this.unreadErrors = 0;
},
```

- [ ] **Step 9: Update init() to load errors and read mode from localStorage**

At the top of `init()`, add:

```js
// Restore layout prefs from localStorage
this._channelLayout = {};
for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i);
    if (k && k.startsWith('xirc_layout_')) {
        this._channelLayout[k.slice('xirc_layout_'.length)] = localStorage.getItem(k);
    }
}
this.statsOnlyDefault = localStorage.getItem('xirc_stats_only') === 'true';
```

After `loadSavedSearches()`:

```js
await this.loadErrors();
```

- [ ] **Step 10: Commit**

```bash
git add web/js/app.js web/js/api.js
git commit -m "feat: extend Alpine store with server view, user lists, dir picker, stats, and error state"
```

---

## Task 8: index.html — Simple/Advanced Toggle + Mobile Layout

**Files:**
- Modify: `web/index.html`

Read `web/index.html` fully before starting.

- [ ] **Step 1: Add mobile hamburger and sidebar overlay**

In `<body>`, add before the sidebar div:

```html
<!-- Mobile hamburger -->
<button class="fixed top-3 left-3 z-50 md:hidden p-2 rounded bg-gray-800 text-white"
        @click="sidebarOpen = !sidebarOpen">
    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
              d="M4 6h16M4 12h16M4 18h16"/>
    </svg>
</button>
<!-- Mobile overlay -->
<div class="fixed inset-0 z-30 bg-black bg-opacity-50 md:hidden"
     x-show="sidebarOpen"
     @click="sidebarOpen = false"></div>
```

Update the sidebar div's class to include mobile transforms:

```html
<div class="... fixed md:static z-40 transform transition-transform duration-200
            -translate-x-full md:translate-x-0"
     :class="sidebarOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'">
```

Close the sidebar on channel/server selection by adding `sidebarOpen = false` to the `@click` handlers on channel and server items.

- [ ] **Step 2: Add mode toggle to sidebar footer**

At the bottom of the sidebar, add:

```html
<div class="border-t border-gray-700 p-2 flex items-center justify-between text-xs text-gray-400">
    <span>Mode:</span>
    <div class="flex gap-1">
        <button @click="setMode('simple')"
                :class="appMode==='simple' ? 'bg-blue-600 text-white' : 'text-gray-400 hover:text-white'"
                class="px-2 py-1 rounded text-xs">Simple</button>
        <button @click="setMode('advanced')"
                :class="appMode==='advanced' ? 'bg-blue-600 text-white' : 'text-gray-400 hover:text-white'"
                class="px-2 py-1 rounded text-xs">Advanced</button>
    </div>
</div>
```

- [ ] **Step 3: Wrap advanced-only sidebar sections**

Wrap the servers tree, and the "Settings" nav tab in `x-show="appMode === 'advanced'"` divs. The nav tabs "Downloads" and "Search" are shown in both modes.

- [ ] **Step 4: Add responsive classes to main content area**

The main content div should use `ml-0 md:ml-64` (or whatever the sidebar width is) to account for the fixed sidebar on mobile:

```html
<div class="flex-1 ml-0 md:ml-64 overflow-auto">
```

- [ ] **Step 5: Commit**

```bash
git add web/index.html
git commit -m "feat: add Simple/Advanced mode toggle and mobile responsive sidebar"
```

---

## Task 9: Home/Dashboard + Error Overview Views

**Files:**
- Modify: `web/index.html`

- [ ] **Step 1: Make the xirc header clickable**

Find the sidebar header element showing "xirc" and add `@click="setView('home')"` and `cursor-pointer` class.

- [ ] **Step 2: Add Channels nav tab**

In the nav row, add a "Channels" button that sets `activeView = 'channel'` (or restores the last active channel). Show in both modes:

```html
<button @click="activeView = activeChannel ? 'channel' : 'channel'"
        :class="activeView==='channel' ? 'border-blue-400 text-blue-400' : 'border-transparent text-gray-400'"
        class="px-3 py-2 border-b-2 text-sm font-medium">Channels</button>
```

- [ ] **Step 3: Add Home view template**

Add a new `<div x-show="activeView === 'home'">` section containing:

```html
<div x-show="activeView === 'home'" class="p-4">
    <h2 class="text-xl font-semibold mb-4">Overview</h2>

    <!-- Server status cards -->
    <div class="grid grid-cols-1 md:grid-cols-2 gap-3 mb-6">
        <template x-for="srv in servers" :key="srv.id">
            <div class="bg-gray-800 rounded-lg p-4 cursor-pointer hover:bg-gray-700"
                 @click="selectServer(srv)">
                <div class="flex items-center justify-between mb-2">
                    <span class="font-medium" x-text="srv.name"></span>
                    <span class="text-xs px-2 py-0.5 rounded-full"
                          :class="{
                              'bg-green-700 text-green-100': serverStatus(srv.id) === 'connected',
                              'bg-yellow-700 text-yellow-100': serverStatus(srv.id) === 'connecting',
                              'bg-red-700 text-red-100': serverStatus(srv.id) === 'disconnected'
                          }"
                          x-text="serverStatus(srv.id)"></span>
                </div>
                <div class="text-xs text-gray-400 space-y-1">
                    <template x-if="ircStatus[srv.id] && ircStatus[srv.id].connected_at">
                        <div x-text="'Connected since: ' + formatTime(ircStatus[srv.id].connected_at)"></div>
                    </template>
                    <div x-text="'Reconnects: ' + (ircStatus[srv.id] ? ircStatus[srv.id].reconnect_count || 0 : 0)"></div>
                    <template x-if="ircStatus[srv.id] && ircStatus[srv.id].lag_ms">
                        <div x-text="'Lag: ' + ircStatus[srv.id].lag_ms + 'ms'"></div>
                    </template>
                </div>
            </div>
        </template>
    </div>

    <!-- Error overview -->
    <div class="bg-gray-800 rounded-lg p-4">
        <div class="flex items-center justify-between mb-3">
            <h3 class="font-medium">Recent Errors
                <span x-show="unreadErrors > 0"
                      class="ml-2 bg-red-600 text-white text-xs rounded-full px-1.5 py-0.5"
                      x-text="unreadErrors"></span>
            </h3>
            <button @click="clearErrors()" class="text-xs text-gray-400 hover:text-white">Clear all</button>
        </div>
        <div x-show="errors.length === 0" class="text-gray-500 text-sm">No recent errors.</div>
        <div class="space-y-2 max-h-64 overflow-y-auto">
            <template x-for="(err, idx) in errors.slice(0,20)" :key="idx">
                <div class="flex items-start gap-2 text-sm bg-gray-700 rounded p-2">
                    <span class="text-red-400 flex-shrink-0">⚠</span>
                    <div class="flex-1 min-w-0">
                        <div class="text-gray-300 truncate" x-text="err.message"></div>
                        <div class="text-xs text-gray-500" x-text="err.timestamp"></div>
                    </div>
                </div>
            </template>
        </div>
    </div>
</div>
```

- [ ] **Step 4: Commit**

```bash
git add web/index.html
git commit -m "feat: add home/dashboard view with server status cards and error overview"
```

---

## Task 10: Server View

**Files:**
- Modify: `web/index.html`

- [ ] **Step 1: Add server view template**

Add a `<div x-show="activeView === 'server'">` section:

```html
<div x-show="activeView === 'server'" class="flex flex-col h-full" x-show="activeServerObj">
    <div class="p-4 border-b border-gray-700 flex items-center gap-3">
        <h2 class="text-lg font-semibold" x-text="activeServerObj && activeServerObj.name"></h2>
        <span class="text-xs px-2 py-0.5 rounded-full"
              :class="{
                  'bg-green-700 text-green-100': serverStatus(activeServer) === 'connected',
                  'bg-yellow-700 text-yellow-100': serverStatus(activeServer) === 'connecting',
                  'bg-red-700 text-red-100': serverStatus(activeServer) === 'disconnected'
              }"
              x-text="serverStatus(activeServer)"></span>
    </div>

    <!-- Stats card -->
    <div class="p-4 grid grid-cols-3 gap-3 border-b border-gray-700 text-sm">
        <div class="bg-gray-800 rounded p-3">
            <div class="text-gray-400 text-xs mb-1">Connected since</div>
            <div x-text="ircStatus[activeServer] && ircStatus[activeServer].connected_at ? formatDate(ircStatus[activeServer].connected_at) : '—'"></div>
        </div>
        <div class="bg-gray-800 rounded p-3">
            <div class="text-gray-400 text-xs mb-1">Reconnects</div>
            <div x-text="ircStatus[activeServer] ? ircStatus[activeServer].reconnect_count || 0 : 0"></div>
        </div>
        <div class="bg-gray-800 rounded p-3">
            <div class="text-gray-400 text-xs mb-1">Lag</div>
            <div x-text="ircStatus[activeServer] && ircStatus[activeServer].lag_ms ? ircStatus[activeServer].lag_ms + ' ms' : '—'"></div>
        </div>
    </div>

    <!-- Raw IRC log -->
    <div class="flex-1 overflow-y-auto font-mono text-xs p-3 space-y-0.5"
         x-ref="serverLog"
         @scroll="$el.scrollTop <= 20 && activeServerObj && loadMoreServerMessages(activeServerObj.id)">
        <template x-for="(msg, idx) in serverMessages[activeServer] || []" :key="idx">
            <div class="flex gap-2">
                <span class="text-gray-500 flex-shrink-0" x-text="formatTime(msg.timestamp)"></span>
                <span class="text-blue-400 flex-shrink-0" x-text="msg.nick ? '&lt;' + msg.nick + '&gt;' : '*'"></span>
                <span class="text-gray-300 break-all" x-text="msg.text || msg.message"></span>
            </div>
        </template>
    </div>

    <!-- Connect/Disconnect buttons (advanced mode) -->
    <div x-show="appMode === 'advanced'" class="p-3 border-t border-gray-700 flex gap-2">
        <button @click="api.connectServer(activeServer).catch(console.error)"
                class="px-3 py-1 bg-green-700 hover:bg-green-600 rounded text-sm">Connect</button>
        <button @click="api.disconnectServer(activeServer).catch(console.error)"
                class="px-3 py-1 bg-red-700 hover:bg-red-600 rounded text-sm">Disconnect</button>
    </div>
</div>
```

- [ ] **Step 2: Update ircStatus merge in init() and WS handler**

In `init()`, update the status merge to include new fields:

```js
const statusMap = {};
for (const s of statuses) {
    statusMap[s.server_id] = {
        status: s.status,
        channels: s.channels,
        name: s.server_name,
        connected_at: s.connected_at,
        reconnect_count: s.reconnect_count,
        lag_ms: s.lag_ms,
    };
}
```

In `_handleWsEvent`, update the `connection_status` handler to merge all fields:

```js
} else if (type === 'connection_status') {
    const d = data.data;
    const status = typeof d === 'string' ? d : d.status;
    this.ircStatus[data.server_id] = Object.assign({}, this.ircStatus[data.server_id] || {}, {
        status: status,
        connected_at: d.connected_at || (this.ircStatus[data.server_id] || {}).connected_at,
        reconnect_count: d.reconnect_count !== undefined ? d.reconnect_count : (this.ircStatus[data.server_id] || {}).reconnect_count,
        lag_ms: d.lag_ms !== undefined ? d.lag_ms : (this.ircStatus[data.server_id] || {}).lag_ms,
    });
}
```

Also update `server/ws_handler.go` to include the new fields when publishing `connection_status`. Find where `EventConnectionStatus` events are forwarded to WS clients. The event's `Data` field currently is just a string (the status). Change the WS forwarding to also include `connected_at`, `reconnect_count`, `lag_ms` from the IRC manager status.

In `ws/hub.go` or `server/ws_handler.go`, when forwarding `EventConnectionStatus`, call `ircMgr.GetStatuses()` to look up the full status for that server and include those fields in the WS message. Alternatively, publish an extended `Data` struct in `setStatus`:

In `irc/connection.go`, update `setStatus` to publish a richer payload:

```go
type ConnectionStatusEvent struct {
    Status         string     `json:"status"`
    ConnectedAt    *time.Time `json:"connected_at,omitempty"`
    ReconnectCount int        `json:"reconnect_count"`
    LagMs          int64      `json:"lag_ms"`
}

func (c *Connection) setStatus(s ConnectionStatus) {
    c.mu.Lock()
    c.status = s
    connectedAt := c.connectedAt
    reconnectCount := c.reconnectCount
    lagMs := c.lagMs
    if lagMs < 0 {
        lagMs = 0
    }
    c.mu.Unlock()

    c.bus.Publish(Event{
        Type:     EventConnectionStatus,
        ServerID: c.server.ID,
        Data: ConnectionStatusEvent{
            Status:         string(s),
            ConnectedAt:    connectedAt,
            ReconnectCount: reconnectCount,
            LagMs:          lagMs,
        },
    })
}
```

Update `ws_handler.go` WS forwarding: the `data.data` field will now be a `ConnectionStatusEvent` struct (JSON object) instead of a plain string. The frontend `_handleWsEvent` already handles this with the `typeof d === 'string'` check above.

- [ ] **Step 3: Build**

```bash
make build
```

- [ ] **Step 4: Commit**

```bash
git add web/index.html irc/connection.go web/js/app.js
git commit -m "feat: add server detail view with stats card and raw IRC log"
```

---

## Task 11: Channel View Rework — Layout Toggle, Download Tab, User List

**Files:**
- Modify: `web/index.html`
- Modify: `web/js/app.js`

- [ ] **Step 1: Restructure the channel view**

Find the existing channel view `<div x-show="activeView === 'channel'">`. Replace its inner layout with:

```html
<div x-show="activeView === 'channel'" class="flex flex-col h-full">
    <!-- Header -->
    <div class="p-3 border-b border-gray-700 flex items-center justify-between flex-shrink-0">
        <div class="flex items-center gap-2">
            <span class="font-medium" x-text="activeChannel"></span>
            <!-- Tab row (single pane mode) -->
            <template x-if="activeChannel && channelLayout(channelKey(activeServer, activeChannel)) === 'single'">
                <div class="flex gap-1 ml-4" x-show="activeChannel">
                    <button @click="setActiveTab(channelKey(activeServer, activeChannel), 'search')"
                            :class="activeTab(channelKey(activeServer, activeChannel)) === 'search' ? 'text-blue-400 border-b-2 border-blue-400' : 'text-gray-400'"
                            class="px-2 py-1 text-sm">Search</button>
                    <template x-if="currentChannelHasDownloadChannel()">
                        <button @click="setActiveTab(channelKey(activeServer, activeChannel), 'download')"
                                :class="activeTab(channelKey(activeServer, activeChannel)) === 'download' ? 'text-blue-400 border-b-2 border-blue-400' : 'text-gray-400'"
                                class="px-2 py-1 text-sm">Downloads IRC</button>
                    </template>
                </div>
            </template>
        </div>
        <!-- Layout toggle (advanced mode, not mobile) -->
        <div class="hidden md:flex gap-1" x-show="appMode === 'advanced' && activeChannel">
            <button @click="setChannelLayout(channelKey(activeServer, activeChannel), 'single')"
                    :class="channelLayout(channelKey(activeServer, activeChannel)) === 'single' ? 'bg-gray-600' : ''"
                    class="px-2 py-1 text-xs rounded text-gray-300">Single</button>
            <button @click="setChannelLayout(channelKey(activeServer, activeChannel), 'sidebyside')"
                    :class="channelLayout(channelKey(activeServer, activeChannel)) === 'sidebyside' ? 'bg-gray-600' : ''"
                    class="px-2 py-1 text-xs rounded text-gray-300">Side by side</button>
        </div>
    </div>

    <!-- Single pane -->
    <template x-if="!activeChannel || channelLayout(channelKey(activeServer, activeChannel)) === 'single'">
        <div class="flex-1 overflow-hidden">
            <!-- search pane: move the existing search form + results table + IRC log from the
                 current channel view into this div. Read the existing index.html to find the
                 search form (x-show="activeView === 'channel'" section), then move its contents here.
                 The three sections to move are: (1) search bar + run button, (2) results table with
                 sortedResults(), (3) IRC log with currentChannelMessages() + input bar. -->
            <div x-show="!activeChannel || activeTab(channelKey(activeServer, activeChannel)) === 'search'"
                 class="flex flex-col h-full">
            </div>
            <!-- download channel pane -->
            <div x-show="currentChannelHasDownloadChannel() && activeTab(channelKey(activeServer, activeChannel)) === 'download'"
                 class="flex flex-col h-full">
                <template x-if="activeServer && downloadChannelForCurrent()">
                    <!-- IRC log for download channel -->
                    <div class="flex-1 overflow-y-auto font-mono text-xs p-3 space-y-0.5">
                        <template x-for="(msg, idx) in ircMessages[channelKey(activeServer, downloadChannelForCurrent())] || []" :key="idx">
                            <div class="flex gap-2"
                                 :class="selectedUser && msg.nick === selectedUser ? 'bg-yellow-900 bg-opacity-40' : ''">
                                <span class="text-gray-500 flex-shrink-0" x-text="formatTime(msg.timestamp)"></span>
                                <span class="text-blue-400 flex-shrink-0" x-text="'&lt;' + msg.nick + '&gt;'"></span>
                                <span class="text-gray-300 break-all" x-text="msg.message"></span>
                            </div>
                        </template>
                    </div>
                    <div class="p-2 border-t border-gray-700 flex gap-2">
                        <input type="text" x-model="ircInput"
                               @keyup.enter="sendIrcToChannel(activeServer, downloadChannelForCurrent())"
                               class="flex-1 bg-gray-700 rounded px-2 py-1 text-sm"
                               placeholder="Message...">
                        <button @click="sendIrcToChannel(activeServer, downloadChannelForCurrent())"
                                class="px-3 py-1 bg-blue-600 rounded text-sm">Send</button>
                    </div>
                </template>
            </div>
        </div>
    </template>

    <!-- Side by side pane (advanced + not mobile) -->
    <template x-if="channelLayout(channelKey(activeServer, activeChannel)) === 'sidebyside'">
        <div class="flex-1 flex overflow-hidden">
            <div class="flex-1 flex flex-col border-r border-gray-700 overflow-hidden">
                <div class="px-3 py-1 text-xs text-gray-400 border-b border-gray-700" x-text="activeChannel + ' — Search'"></div>
                <!-- search pane content (same as single/search) -->
            </div>
            <template x-if="currentChannelHasDownloadChannel()">
                <div class="flex-1 flex flex-col overflow-hidden">
                    <div class="px-3 py-1 text-xs text-gray-400 border-b border-gray-700" x-text="downloadChannelForCurrent() + ' — IRC'"></div>
                    <!-- download channel IRC log -->
                </div>
            </template>
        </div>
    </template>
</div>
```

The search pane content is the existing search form + results table + IRC log from the current channel view — move it into the indicated spots.

- [ ] **Step 2: Add helper methods to app.js**

```js
currentChannelHasDownloadChannel() {
    if (!this.activeServer || !this.activeChannel) return false;
    const srv = this.servers.find(s => s.id === this.activeServer);
    if (!srv) return false;
    // channels are on the server object or fetched separately
    // Check ircStatus channels list — fallback: check channel config
    // For simplicity, always show tab if activeChannel !== downloadChannelForCurrent()
    const dl = this.downloadChannelForCurrent();
    return dl && dl !== this.activeChannel;
},

downloadChannelForCurrent() {
    // The download_channel for the active search channel.
    // Stored in the channel config; for now derive from server channels list if available.
    // This requires the channel objects to be accessible.
    // Load them from settingsChannels if needed, or keep a channelConfigs map.
    return this._channelConfigs && this._channelConfigs[this.channelKey(this.activeServer, this.activeChannel)];
},

sendIrcToChannel(serverId, channel) {
    const text = this.ircInput.trim();
    if (!text || !serverId || !channel) return;
    this.ircInput = '';
    api.sendMessage(serverId, channel, text).catch(console.error);
},
```

Add `_channelConfigs: {}` to initial state. Populate it in `init()` after loading servers:

```js
// Build channel config map: channelKey -> download_channel
for (const srv of this.servers) {
    const channels = await api.getChannels(srv.id).catch(() => []);
    for (const ch of (channels || [])) {
        const key = this.channelKey(srv.id, ch.name);
        this._channelConfigs[key] = ch.download_channel || ch.name;
    }
}
```

- [ ] **Step 3: Add User List sidebar to IRC panes**

Within each IRC pane, add a collapsible user list sidebar:

```html
<div class="flex flex-1 overflow-hidden">
    <!-- IRC log (flex-1) -->
    <div class="flex-1 overflow-y-auto font-mono text-xs p-3 space-y-0.5" x-ref="ircLog">
        <!-- existing messages -->
    </div>
    <!-- User list sidebar (collapsible) -->
    <div class="w-32 border-l border-gray-700 flex flex-col flex-shrink-0"
         x-show="appMode === 'advanced'">
        <div class="px-2 py-1 border-b border-gray-700 flex items-center justify-between text-xs">
            <span class="text-gray-400">Users</span>
            <button @click="loadUserList(activeServer, activeChannel)"
                    class="text-gray-500 hover:text-white text-xs">↺</button>
        </div>
        <div class="flex-1 overflow-y-auto text-xs p-1 space-y-0.5">
            <template x-if="userListLoading[channelKey(activeServer, activeChannel)]">
                <div class="text-gray-500 p-2">Loading...</div>
            </template>
            <template x-for="nick in userLists[channelKey(activeServer, activeChannel)] || []" :key="nick">
                <div @click="selectUser(nick.replace(/^[@+%]/, ''))"
                     class="px-1 py-0.5 rounded cursor-pointer truncate hover:bg-gray-700"
                     :class="isUserSelected(nick.replace(/^[@+%]/, '')) ? 'bg-yellow-900 text-yellow-200' : 'text-gray-300'"
                     x-text="nick"></div>
            </template>
        </div>
    </div>
</div>
```

- [ ] **Step 4: Filter search results by selected user (bot nick)**

In `sortedResults()`, add filter before sort:

```js
sortedResults() {
    var col = this.searchSort.col;
    var dir = this.searchSort.dir;
    var arr = this.searchResults.slice();
    if (this.selectedUser) {
        arr = arr.filter(function(r) {
            return r.bot_nick === this.selectedUser;
        }.bind(this));
    }
    // ... existing sort logic ...
},
```

- [ ] **Step 5: Highlight selected user's messages in IRC log**

The IRC log message template already has `:class="selectedUser && msg.nick === selectedUser ? 'bg-yellow-900 bg-opacity-40' : ''"` from the app.js `userHighlightClass` — apply this to the existing channel IRC log messages template:

Find the `<template x-for="(msg, idx) in currentChannelMessages()"` loop and add the class binding to the outer div.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/js/app.js
git commit -m "feat: rework channel view with layout toggle, download tab, and user list"
```

---

## Task 12: File Routing UX Inversion + Directory Picker Modal

**Files:**
- Modify: `web/index.html`
- Modify: `web/js/app.js`

- [ ] **Step 1: Add grouped routing computed property to app.js**

```js
groupedRoutingRules() {
    var groups = {};
    for (var i = 0; i < this.routingRules.length; i++) {
        var r = this.routingRules[i];
        var dir = r.destination_dir;
        if (!groups[dir]) groups[dir] = [];
        // Extract extension from pattern like "*.mkv" -> "mkv"
        var ext = r.pattern.replace(/^\*\./, '');
        groups[dir].push({ id: r.id, ext: ext, rule: r });
    }
    return groups;
},

predefinedExtensions() {
    return ['mkv', 'mp4', 'avi', 'mp3', 'flac', 'epub', 'pdf', 'zip', 'cbz'];
},
```

- [ ] **Step 2: Add new routing state to appStore**

```js
routingNewDir: '',
routingNewExt: '',
showAddDestForm: false,
```

- [ ] **Step 3: Replace flat routing rules list in settings HTML**

Find the routing rules settings section in `index.html`. Replace the flat table with:

```html
<!-- Routing Rules — grouped by destination dir -->
<div x-show="settingsTab === 'routing'">
    <div class="flex justify-between items-center mb-3">
        <h3 class="font-medium">File Routing</h3>
        <button @click="showAddDestForm = !showAddDestForm"
                class="px-3 py-1 bg-blue-600 hover:bg-blue-700 rounded text-sm">+ Add destination</button>
    </div>

    <!-- Add destination form -->
    <div x-show="showAddDestForm" class="bg-gray-700 rounded p-3 mb-4 space-y-2">
        <div class="flex gap-2">
            <input type="text" x-model="routingNewDir" placeholder="/srv/downloads"
                   class="flex-1 bg-gray-600 rounded px-2 py-1 text-sm">
            <button @click="openDirPicker(routingNewDir, d => routingNewDir = d)"
                    class="px-2 py-1 bg-gray-500 hover:bg-gray-400 rounded text-sm">📁</button>
        </div>
        <div class="flex flex-wrap gap-1">
            <template x-for="ext in predefinedExtensions()" :key="ext">
                <button @click="addRoutingRuleForDir(routingNewDir, ext)"
                        class="px-2 py-0.5 bg-gray-600 hover:bg-blue-600 rounded text-xs">
                    + <span x-text="ext"></span>
                </button>
            </template>
            <div class="flex gap-1">
                <input type="text" x-model="routingNewExt" placeholder="custom ext" maxlength="10"
                       class="w-24 bg-gray-600 rounded px-2 py-0.5 text-xs">
                <button @click="addRoutingRuleForDir(routingNewDir, routingNewExt); routingNewExt = ''"
                        class="px-2 py-0.5 bg-blue-600 hover:bg-blue-700 rounded text-xs">Add</button>
            </div>
        </div>
    </div>

    <!-- Groups -->
    <div class="space-y-3">
        <template x-for="(exts, dir) in groupedRoutingRules()" :key="dir">
            <div class="bg-gray-700 rounded p-3">
                <div class="flex items-center justify-between mb-2">
                    <div class="font-mono text-sm text-green-400" x-text="dir"></div>
                    <button @click="openDirPicker(dir, d => renameRoutingDir(dir, d))"
                            class="text-xs text-gray-400 hover:text-white">📁 Change</button>
                </div>
                <div class="flex flex-wrap gap-1">
                    <template x-for="item in exts" :key="item.id">
                        <span class="flex items-center gap-1 px-2 py-0.5 bg-gray-600 rounded text-xs">
                            <span x-text="item.ext"></span>
                            <button @click="deleteRoutingRule(item.id)"
                                    class="text-gray-400 hover:text-red-400 ml-1">×</button>
                        </span>
                    </template>
                    <button @click="routingNewDir = dir; showAddDestForm = true"
                            class="px-2 py-0.5 bg-gray-600 hover:bg-blue-600 rounded text-xs">+ ext</button>
                </div>
            </div>
        </template>
    </div>
</div>
```

- [ ] **Step 4: Add helper methods to app.js**

```js
async addRoutingRuleForDir(dir, ext) {
    if (!dir || !ext) return;
    const pattern = '*.' + ext.replace(/^\./, '');
    await api.createRoutingRule({ pattern, destination_dir: dir, priority: 0 });
    await this.loadSettingsData();
},

async renameRoutingDir(oldDir, newDir) {
    if (!newDir || newDir === oldDir) return;
    const groups = this.groupedRoutingRules();
    const rules = groups[oldDir] || [];
    for (const item of rules) {
        await api.updateRoutingRule(item.id, Object.assign({}, item.rule, { destination_dir: newDir }));
    }
    await this.loadSettingsData();
},
```

- [ ] **Step 5: Add Directory Picker Modal**

Add the modal HTML at the bottom of `<body>` (before closing `</body>`), outside the main app layout:

```html
<!-- Directory Picker Modal -->
<div x-show="dirPickerOpen"
     class="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-60"
     @keydown.escape.window="cancelDirPicker()">
    <div class="bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4">
        <div class="flex items-center justify-between p-4 border-b border-gray-700">
            <h3 class="font-medium">Select Directory</h3>
            <button @click="cancelDirPicker()" class="text-gray-400 hover:text-white">✕</button>
        </div>
        <!-- Breadcrumb -->
        <div class="px-4 py-2 border-b border-gray-700 font-mono text-sm text-green-400 flex items-center gap-1">
            <button @click="browseDir('/')" class="hover:text-white">/</button>
            <template x-for="(seg, i) in dirPickerPath.split('/').filter(Boolean)" :key="i">
                <span class="flex items-center gap-1">
                    <span class="text-gray-500">/</span>
                    <button @click="browseDir('/' + dirPickerPath.split('/').filter(Boolean).slice(0, i+1).join('/'))"
                            class="hover:text-white" x-text="seg"></button>
                </span>
            </template>
        </div>
        <!-- Entries -->
        <div class="max-h-64 overflow-y-auto p-2 space-y-0.5">
            <button x-show="dirPickerPath !== '/'"
                    @click="browseDirUp()"
                    class="w-full text-left px-3 py-1.5 rounded hover:bg-gray-700 text-sm text-gray-400">
                .. (up)
            </button>
            <template x-for="entry in dirPickerEntries.filter(e => e.is_dir)" :key="entry.name">
                <button @click="browseDir(dirPickerPath.replace(/\/$/, '') + '/' + entry.name)"
                        class="w-full text-left px-3 py-1.5 rounded hover:bg-gray-700 text-sm flex items-center gap-2">
                    <span class="text-yellow-400">📁</span>
                    <span x-text="entry.name"></span>
                </button>
            </template>
            <div x-show="dirPickerEntries.filter(e => e.is_dir).length === 0"
                 class="text-gray-500 text-sm px-3 py-2">No subdirectories.</div>
        </div>
        <!-- Actions -->
        <div class="flex justify-end gap-2 p-4 border-t border-gray-700">
            <button @click="cancelDirPicker()" class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 rounded text-sm">Cancel</button>
            <button @click="selectDir()" class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded text-sm">
                Select: <span class="font-mono" x-text="dirPickerPath"></span>
            </button>
        </div>
    </div>
</div>
```

- [ ] **Step 6: Wire dir picker into channel form's download_channel field**

Note: `download_channel` in the channel config is an IRC channel name (e.g., `#xdcc-dl`), not a filesystem path. Do NOT wire the dir picker to this field. Only wire it to `destination_dir` fields in routing rules (already done above).

- [ ] **Step 7: Commit**

```bash
git add web/index.html web/js/app.js
git commit -m "feat: invert file routing UX to destination-first grouping and add dir picker modal"
```

---

## Task 13: File Manager + Download Stats + History Views

**Files:**
- Modify: `web/index.html`
- Modify: `web/js/app.js`

- [ ] **Step 1: Add File Manager view**

Add a new nav tab "Files" (visible in both modes). Add to the nav row:

```html
<button @click="setView('files')"
        :class="activeView==='files' ? 'border-blue-400 text-blue-400' : 'border-transparent text-gray-400'"
        class="px-3 py-2 border-b-2 text-sm font-medium">Files</button>
```

Add the file manager view template:

```html
<div x-show="activeView === 'files'" class="p-4 h-full flex flex-col">
    <h2 class="text-xl font-semibold mb-4">File Manager</h2>
    <div class="flex gap-4 flex-1 overflow-hidden">
        <!-- Dir list (left) -->
        <div class="w-48 flex-shrink-0 space-y-1">
            <div class="text-xs text-gray-400 mb-2 uppercase tracking-wide">Destinations</div>
            <template x-for="dir in Object.keys(groupedRoutingRules())" :key="dir">
                <button @click="loadFileManager(dir)"
                        :class="fileManagerDir === dir ? 'bg-blue-700 text-white' : 'bg-gray-700 hover:bg-gray-600 text-gray-300'"
                        class="w-full text-left px-3 py-2 rounded text-sm font-mono truncate"
                        x-text="dir"></button>
            </template>
        </div>
        <!-- File list (right) -->
        <div class="flex-1 overflow-auto">
            <div x-show="!fileManagerDir" class="text-gray-500 text-sm p-4">Select a directory.</div>
            <div x-show="fileManagerLoading" class="text-gray-500 text-sm p-4">Loading...</div>
            <table x-show="fileManagerDir && !fileManagerLoading" class="w-full text-sm">
                <thead>
                    <tr class="text-gray-400 border-b border-gray-700 text-left">
                        <th class="pb-2 pr-4">Filename</th>
                        <th class="pb-2 pr-4">Size</th>
                        <th class="pb-2">Modified</th>
                    </tr>
                </thead>
                <tbody>
                    <tr x-show="fileManagerFiles.length === 0">
                        <td colspan="3" class="text-gray-500 py-4">No files.</td>
                    </tr>
                    <template x-for="f in fileManagerFiles" :key="f.name">
                        <tr class="border-b border-gray-800 hover:bg-gray-800">
                            <td class="py-1.5 pr-4 font-mono text-xs" x-text="f.name"></td>
                            <td class="py-1.5 pr-4 text-gray-400 text-xs" x-text="formatSize(f.size)"></td>
                            <td class="py-1.5 text-gray-400 text-xs" x-text="formatDate(f.modified)"></td>
                        </tr>
                    </template>
                </tbody>
            </table>
        </div>
    </div>
</div>
```

- [ ] **Step 2: Add Stats view**

Add a "Stats" nav tab (advanced mode only `x-show="appMode==='advanced'"`):

```html
<button x-show="appMode==='advanced'"
        @click="setView('stats'); loadStats()"
        :class="activeView==='stats' ? 'border-blue-400 text-blue-400' : 'border-transparent text-gray-400'"
        class="px-3 py-2 border-b-2 text-sm font-medium">Stats</button>
```

Add the stats view:

```html
<div x-show="activeView === 'stats'" class="p-4 overflow-auto">
    <h2 class="text-xl font-semibold mb-4">Download Statistics</h2>

    <!-- Summary cards -->
    <div x-show="statsSummary" class="grid grid-cols-2 md:grid-cols-4 gap-3 mb-6">
        <div class="bg-gray-800 rounded p-3 text-center">
            <div class="text-2xl font-bold" x-text="statsSummary && statsSummary.total_transfers || 0"></div>
            <div class="text-xs text-gray-400">Total transfers</div>
        </div>
        <div class="bg-gray-800 rounded p-3 text-center">
            <div class="text-2xl font-bold"
                 x-text="statsSummary ? statsSummary.success_rate.toFixed(1) + '%' : '—'"></div>
            <div class="text-xs text-gray-400">Success rate</div>
        </div>
        <div class="bg-gray-800 rounded p-3 text-center">
            <div class="text-2xl font-bold"
                 x-text="statsSummary ? formatSize(statsSummary.total_bytes) : '—'"></div>
            <div class="text-xs text-gray-400">Total transferred</div>
        </div>
        <div class="bg-gray-800 rounded p-3 text-center">
            <div class="text-2xl font-bold"
                 x-text="statsSummary ? formatSize(statsSummary.total_saved) : '—'"></div>
            <div class="text-xs text-gray-400">Saved to disk</div>
        </div>
    </div>

    <!-- Stats-only toggle -->
    <div class="bg-gray-800 rounded p-3 mb-4 flex items-center gap-3">
        <label class="text-sm font-medium">Download mode:</label>
        <button @click="statsOnlyDefault = false; localStorage.setItem('xirc_stats_only', 'false')"
                :class="!statsOnlyDefault ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300'"
                class="px-3 py-1 rounded text-sm">Save file</button>
        <button @click="statsOnlyDefault = true; localStorage.setItem('xirc_stats_only', 'true')"
                :class="statsOnlyDefault ? 'bg-orange-600 text-white' : 'bg-gray-700 text-gray-300'"
                class="px-3 py-1 rounded text-sm">Stats only (don't save)</button>
        <span class="text-xs text-gray-400 ml-2" x-show="statsOnlyDefault">
            Files will be downloaded then immediately deleted — only statistics are kept.
        </span>
    </div>

    <!-- History table -->
    <h3 class="font-medium mb-2">History</h3>
    <div class="overflow-x-auto">
        <table class="w-full text-sm">
            <thead>
                <tr class="text-gray-400 border-b border-gray-700 text-left">
                    <th class="pb-2 pr-3">Filename</th>
                    <th class="pb-2 pr-3">Size</th>
                    <th class="pb-2 pr-3">Bot</th>
                    <th class="pb-2 pr-3">Status</th>
                    <th class="pb-2">Date</th>
                </tr>
            </thead>
            <tbody>
                <template x-for="d in statsHistory" :key="d.id">
                    <tr class="border-b border-gray-800 hover:bg-gray-800">
                        <td class="py-1 pr-3 font-mono text-xs truncate max-w-xs" x-text="d.filename"></td>
                        <td class="py-1 pr-3 text-gray-400 text-xs" x-text="formatSize(d.size_bytes)"></td>
                        <td class="py-1 pr-3 text-xs" x-text="d.bot_nick"></td>
                        <td class="py-1 pr-3 text-xs">
                            <span :class="{
                                'text-green-400': d.status === 'completed' || d.status === 'stats_only',
                                'text-red-400': d.status === 'failed',
                                'text-gray-400': d.status === 'cancelled'
                            }" x-text="d.status"></span>
                        </td>
                        <td class="py-1 text-gray-400 text-xs" x-text="formatDate(d.completed_at)"></td>
                    </tr>
                </template>
            </tbody>
        </table>
    </div>
    <button @click="loadMoreHistory()"
            x-show="statsHistory.length > 0 && statsHistory.length === statsHistoryOffset"
            class="mt-3 px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded text-sm">Load more</button>
</div>
```

- [ ] **Step 3: Wire stats-only into downloadPack**

In `app.js`, update `downloadPack`:

```js
downloadPack(row) {
    api.requestDownload({
        server_id: this.activeServer,
        channel: this.activeChannel,
        bot_nick: row.bot_nick,
        pack_number: row.pack_number,
        stats_only: this.statsOnlyDefault,
    }).catch(function(e) { console.error('download request error', e); });
},
```

In `server/download_handlers.go`, ensure `stats_only` is decoded from the request and set on the `Download` struct before creating it. Find `handleRequestDownload` and add `StatsOnly bool \`json:"stats_only"\`` to the request struct, then set `d.StatsOnly = req.StatsOnly`.

- [ ] **Step 4: Commit**

```bash
git add web/index.html web/js/app.js server/download_handlers.go
git commit -m "feat: add file manager, download stats view, history, and stats-only mode"
```

---

## Task 14: Final Integration + Tests

- [ ] **Step 1: Run all tests**

```bash
make test
```

Fix any failures. Common issues:
- If `db` tests fail, the new Store interface methods may be missing from the test double or MySQL implementation.
- If `server` tests fail, `server.New()` signature changed — update test calls to pass `nil, nil` for `msgBuf, errBuf`.
- If `irc` tests fail, check that mock `IRCClient` implements `SendLine`.

- [ ] **Step 2: Verify existing mock IRCClient has SendLine**

Check `irc/connection_test.go` or wherever the mock `IRCClient` is defined. Add:

```go
func (m *mockIRCClient) SendLine(line string) {}
```

- [ ] **Step 3: Fix server_test.go**

In `server/server_test.go`, update all `server.New(...)` calls to pass two nil buffer args:

```go
srv := server.New(store, ircMgr, p, eng, hub, nil, nil)
```

- [ ] **Step 4: Final build**

```bash
make build
```

- [ ] **Step 5: Smoke test**

```bash
./xirc --config config.yaml
```

Open browser, verify:
- Home view loads with server cards
- Simple/Advanced toggle works
- Clicking server shows server view
- Channel view has layout toggle in advanced mode
- Settings → Routing shows destination-grouped view
- Dir picker opens and navigates filesystem
- Stats view shows (may be empty)
- File manager shows configured dirs

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "fix: update test doubles and server constructor calls for Plan 9 backend changes"
```

---

## End State

After Plan 9:
- Startup fails fast with clear error messages and correct exit codes (78 for config errors, 1 for transient)
- IRC messages buffered in-memory, accessible via REST for history scrolling
- Error events collected and browsable on the dashboard
- Server status includes uptime, reconnect count, and lag
- NAMES endpoint provides user list snapshots
- Directory picker modal for routing rule destination dirs
- Download stats recorded per transfer, with stats-only mode
- Simple mode hides technical details for casual users
- Mobile sidebar with hamburger
- File manager browses configured routing directories
- Channel view supports side-by-side layout and download channel tab
- User list with nick filtering for search results and message highlighting

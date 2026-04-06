# xirc Plan 2: IRC Core — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the IRC connection manager that connects to multiple servers, authenticates (NickServ/SASL/none), joins channel pairs, and routes messages — all manageable from the existing HTTP API.

**Architecture:** An `irc` package wraps the girc library behind a `Manager` that owns all connections. Each server gets a `Connection` goroutine. Messages are fanned out to subscribers (future WebSocket clients, search parser, DCC engine) via a typed event bus. The HTTP server gets new endpoints for managing connections.

**Tech Stack:** Go 1.22+, `github.com/lrstanley/girc`, existing `db` and `server` packages

**Depends on:** Plan 1 (Foundation) must be complete.

---

## File Structure

```
maxwell-xirc/
├── irc/
│   ├── manager.go          # Manager: owns all connections, start/stop lifecycle
│   ├── manager_test.go
│   ├── connection.go        # Connection: single server, auth, channel management
│   ├── connection_test.go
│   ├── events.go            # Event types + EventBus for message fan-out
│   └── events_test.go
├── server/
│   ├── server.go            # (modify) add IRC manager + new API routes
│   ├── irc_handlers.go      # HTTP handlers for IRC operations
│   └── irc_handlers_test.go
```

---

### Task 1: Event System

**Files:**
- Create: `irc/events.go`
- Create: `irc/events_test.go`

- [ ] **Step 1: Write failing tests for the event bus**

Create `irc/events_test.go`:

```go
package irc

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_Subscribe_ReceivesEvents(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	go bus.Publish(Event{Type: EventIRCMessage, ServerID: 1, Channel: "#test", Data: "hello"})

	select {
	case ev := <-ch:
		if ev.Type != EventIRCMessage {
			t.Errorf("expected type %s, got %s", EventIRCMessage, ev.Type)
		}
		if ev.Channel != "#test" {
			t.Errorf("expected channel #test, got %s", ev.Channel)
		}
		if ev.Data != "hello" {
			t.Errorf("expected data hello, got %v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	go bus.Publish(Event{Type: EventConnectionStatus, ServerID: 1, Data: "connected"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Type != EventConnectionStatus {
				t.Errorf("expected type %s, got %s", EventConnectionStatus, ev.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}
}

func TestEventBus_Unsubscribe_StopsReceiving(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	bus.Unsubscribe(ch)

	bus.Publish(Event{Type: EventIRCMessage, Data: "should not arrive"})

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// Channel was closed, good
	}
}

func TestEventBus_SlowSubscriber_DoesNotBlock(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Don't read from ch — should not block the publisher
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			bus.Publish(Event{Type: EventIRCMessage, Data: i})
		}
		close(done)
	}()

	select {
	case <-done:
		// Publisher didn't block, good
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked on slow subscriber")
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			bus.Publish(Event{Type: EventIRCMessage, Data: n})
		}(i)
	}

	wg.Wait()
	// If we get here without panic/deadlock, concurrent publish is safe
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd irc && go test -v ./...
```

Expected: compilation error — `irc` package doesn't exist.

- [ ] **Step 3: Implement event bus**

Create `irc/events.go`:

```go
package irc

import "sync"

type EventType string

const (
	EventIRCMessage       EventType = "irc_message"
	EventConnectionStatus EventType = "connection_status"
	EventSearchResult     EventType = "search_result"
	EventDownloadProgress EventType = "download_progress"
	EventDownloadStatus   EventType = "download_status"
	EventNotification     EventType = "notification"
)

type Event struct {
	Type     EventType   `json:"type"`
	ServerID int64       `json:"server_id"`
	Channel  string      `json:"channel,omitempty"`
	Nick     string      `json:"nick,omitempty"`
	Data     interface{} `json:"data"`
}

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan Event]struct{}),
	}
}

func (b *EventBus) Subscribe() <-chan Event {
	ch := make(chan Event, 128)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBus) Unsubscribe(ch <-chan Event) {
	send := ch.(chan Event)
	b.mu.Lock()
	delete(b.subscribers, send)
	b.mu.Unlock()
	close(send)
}

func (b *EventBus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
			// Slow subscriber, drop event rather than block
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd irc && go test -v ./...
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add irc/events.go irc/events_test.go
git commit -m "feat: add event bus for IRC message fan-out"
```

---

### Task 2: IRC Connection — Connect, Auth, Join

**Files:**
- Create: `irc/connection.go`
- Create: `irc/connection_test.go`

- [ ] **Step 1: Write failing tests for connection**

Create `irc/connection_test.go`:

```go
package irc

import (
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestNewConnection_SetsFields(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{
		ID:           1,
		Name:         "test",
		Host:         "irc.example.com",
		Port:         6667,
		SSL:          false,
		Nickname:     "testbot",
		AltNicknames: []string{"testbot_"},
		AuthMethod:   "nickserv",
		AuthPassword: "secret",
		Enabled:      true,
	}
	channels := []db.Channel{
		{ID: 1, ServerID: 1, Name: "#search", SearchCommand: "!s", DownloadChannel: "#downloads", AutoJoin: true, Enabled: true},
		{ID: 2, ServerID: 1, Name: "#downloads", AutoJoin: true, Enabled: true},
	}

	conn := NewConnection(srv, channels, bus)

	if conn.ServerID() != int64(1) {
		t.Errorf("expected server ID 1, got %d", conn.ServerID())
	}
	if conn.Status() != StatusDisconnected {
		t.Errorf("expected status disconnected, got %s", conn.Status())
	}
}

func TestConnection_ChannelPairs(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	channels := []db.Channel{
		{ID: 1, ServerID: 1, Name: "#search", SearchCommand: "!s", DownloadChannel: "#downloads", AutoJoin: true, Enabled: true},
		{ID: 2, ServerID: 1, Name: "#simple", SearchCommand: "!s", AutoJoin: true, Enabled: true},
	}

	conn := NewConnection(srv, channels, bus)

	pairs := conn.ChannelPairs()
	if len(pairs) != 2 {
		t.Fatalf("expected 2 channel pairs, got %d", len(pairs))
	}

	// #search → #downloads
	if pairs[0].SearchChannel != "#search" {
		t.Errorf("expected search channel #search, got %s", pairs[0].SearchChannel)
	}
	if pairs[0].DownloadChannel != "#downloads" {
		t.Errorf("expected download channel #downloads, got %s", pairs[0].DownloadChannel)
	}

	// #simple → #simple (same channel)
	if pairs[1].SearchChannel != "#simple" {
		t.Errorf("expected search channel #simple, got %s", pairs[1].SearchChannel)
	}
	if pairs[1].DownloadChannel != "#simple" {
		t.Errorf("expected download channel #simple (same), got %s", pairs[1].DownloadChannel)
	}
}

func TestConnection_AllChannelNames(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	channels := []db.Channel{
		{ID: 1, ServerID: 1, Name: "#search", DownloadChannel: "#downloads", AutoJoin: true, Enabled: true},
	}

	conn := NewConnection(srv, channels, bus)

	names := conn.AllChannelNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 unique channel names, got %d: %v", len(names), names)
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["#search"] || !found["#downloads"] {
		t.Errorf("expected #search and #downloads, got %v", names)
	}
}

func TestConnectionStatus_StringValues(t *testing.T) {
	if StatusDisconnected != "disconnected" {
		t.Errorf("unexpected value: %s", StatusDisconnected)
	}
	if StatusConnecting != "connecting" {
		t.Errorf("unexpected value: %s", StatusConnecting)
	}
	if StatusConnected != "connected" {
		t.Errorf("unexpected value: %s", StatusConnected)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd irc && go test -v -run TestConnection ./... && go test -v -run TestNewConnection ./...
```

Expected: compilation error — `Connection`, `NewConnection` don't exist.

- [ ] **Step 3: Implement connection**

Create `irc/connection.go`:

```go
package irc

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lrstanley/girc"
	"github.com/maxwell-xirc/xirc/db"
)

type ConnectionStatus string

const (
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
)

type ChannelPair struct {
	SearchChannel   string
	DownloadChannel string
	SearchCommand   string
	AutoJoin        bool
}

type Connection struct {
	mu       sync.RWMutex
	server   *db.Server
	channels []db.Channel
	bus      *EventBus
	client   *girc.Client
	status   ConnectionStatus
	stopCh   chan struct{}
}

func NewConnection(server *db.Server, channels []db.Channel, bus *EventBus) *Connection {
	return &Connection{
		server:   server,
		channels: channels,
		bus:      bus,
		status:   StatusDisconnected,
		stopCh:   make(chan struct{}),
	}
}

func (c *Connection) ServerID() int64 {
	return c.server.ID
}

func (c *Connection) ServerName() string {
	return c.server.Name
}

func (c *Connection) Status() ConnectionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Connection) setStatus(s ConnectionStatus) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()

	c.bus.Publish(Event{
		Type:     EventConnectionStatus,
		ServerID: c.server.ID,
		Data:     string(s),
	})
}

func (c *Connection) ChannelPairs() []ChannelPair {
	var pairs []ChannelPair
	for _, ch := range c.channels {
		if !ch.Enabled {
			continue
		}
		dl := ch.DownloadChannel
		if dl == "" {
			dl = ch.Name
		}
		cmd := ch.SearchCommand
		if cmd == "" {
			cmd = "!s"
		}
		pairs = append(pairs, ChannelPair{
			SearchChannel:   ch.Name,
			DownloadChannel: dl,
			SearchCommand:   cmd,
			AutoJoin:        ch.AutoJoin,
		})
	}
	return pairs
}

func (c *Connection) AllChannelNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, pair := range c.ChannelPairs() {
		for _, name := range []string{pair.SearchChannel, pair.DownloadChannel} {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func (c *Connection) Connect() error {
	c.setStatus(StatusConnecting)

	cfg := girc.Config{
		Server: c.server.Host,
		Port:   c.server.Port,
		Nick:   c.server.Nickname,
		User:   c.server.Nickname,
		Name:   "xirc XDCC client",
		SSL:    c.server.SSL,
	}

	if c.server.AuthMethod == "sasl" && c.server.AuthPassword != "" {
		cfg.SASL = &girc.SASLPlain{
			User: c.server.Nickname,
			Pass: c.server.AuthPassword,
		}
	}

	client := girc.New(cfg)

	client.Handlers.Add(girc.CONNECTED, func(cl *girc.Client, e girc.Event) {
		c.setStatus(StatusConnected)
		log.Printf("[%s] connected", c.server.Name)

		// NickServ auth after connect
		if c.server.AuthMethod == "nickserv" && c.server.AuthPassword != "" {
			cl.Cmd.Message("NickServ", "IDENTIFY "+c.server.AuthPassword)
			log.Printf("[%s] sent NickServ IDENTIFY", c.server.Name)
		}

		// Auto-join channels
		for _, ch := range c.channels {
			if !ch.Enabled || !ch.AutoJoin {
				continue
			}
			if ch.Key != "" {
				cl.Cmd.JoinKey(ch.Name, ch.Key)
			} else {
				cl.Cmd.Join(ch.Name)
			}
			// Also join download channel if different
			if ch.DownloadChannel != "" && ch.DownloadChannel != ch.Name {
				cl.Cmd.Join(ch.DownloadChannel)
			}
		}
	})

	client.Handlers.Add(girc.DISCONNECTED, func(cl *girc.Client, e girc.Event) {
		c.setStatus(StatusDisconnected)
		log.Printf("[%s] disconnected", c.server.Name)
	})

	// Route all PRIVMSG/NOTICE to event bus
	client.Handlers.Add(girc.PRIVMSG, func(cl *girc.Client, e girc.Event) {
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Channel:  e.Params[0],
			Nick:     e.Source.Name,
			Data: map[string]string{
				"type":    "privmsg",
				"message": e.Last(),
			},
		})
	})

	client.Handlers.Add(girc.NOTICE, func(cl *girc.Client, e girc.Event) {
		target := ""
		if len(e.Params) > 0 {
			target = e.Params[0]
		}
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Channel:  target,
			Nick:     e.Source.Name,
			Data: map[string]string{
				"type":    "notice",
				"message": e.Last(),
			},
		})
	})

	// Handle CTCP (for DCC)
	client.Handlers.Add(girc.CTCP, func(cl *girc.Client, e girc.Event) {
		c.bus.Publish(Event{
			Type:     EventIRCMessage,
			ServerID: c.server.ID,
			Nick:     e.Source.Name,
			Data: map[string]string{
				"type":    "ctcp",
				"command": e.Command,
				"message": e.Last(),
			},
		})
	})

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()

	// Connect with reconnect loop in a goroutine
	go c.connectLoop()

	return nil
}

func (c *Connection) connectLoop() {
	backoff := []time.Duration{5, 10, 30, 60, 120}
	attempt := 0

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		c.setStatus(StatusConnecting)
		err := c.client.Connect()
		if err == nil {
			return // Clean disconnect (e.g., via Disconnect())
		}

		select {
		case <-c.stopCh:
			return
		default:
		}

		delay := backoff[len(backoff)-1]
		if attempt < len(backoff) {
			delay = backoff[attempt]
		}
		attempt++

		log.Printf("[%s] connection failed: %v, retrying in %ds", c.server.Name, err, delay)
		c.setStatus(StatusDisconnected)

		select {
		case <-time.After(delay * time.Second):
		case <-c.stopCh:
			return
		}
	}
}

func (c *Connection) Disconnect() {
	close(c.stopCh)

	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Quit("xirc shutting down")
		client.Close()
	}
	c.setStatus(StatusDisconnected)
}

func (c *Connection) JoinChannel(name, key string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return
	}
	if key != "" {
		client.Cmd.JoinKey(name, key)
	} else {
		client.Cmd.Join(name)
	}
}

func (c *Connection) PartChannel(name string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Cmd.Part(name)
	}
}

func (c *Connection) SendMessage(target, message string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Cmd.Message(target, message)
	}
}

func (c *Connection) SendRaw(raw string) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client != nil {
		client.Cmd.SendRaw(raw)
	}
}

func (c *Connection) IsInChannel(name string) bool {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return false
	}

	channels := client.ChannelList()
	target := strings.ToLower(name)
	for _, ch := range channels {
		if strings.ToLower(ch) == target {
			return true
		}
	}
	return false
}

func (c *Connection) Search(searchChannel, command, query string) error {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return fmt.Errorf("not connected to %s", c.server.Name)
	}

	msg := fmt.Sprintf("%s %s", command, query)
	client.Cmd.Message(searchChannel, msg)
	return nil
}

func (c *Connection) RequestPack(channel, botNick string, packNumber int) error {
	c.mu.RLock()
	client := c.client
	status := c.status
	c.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return fmt.Errorf("not connected to %s", c.server.Name)
	}

	if !c.IsInChannel(channel) {
		return fmt.Errorf("not in channel %s — join it first", channel)
	}

	msg := fmt.Sprintf("xdcc send #%d", packNumber)
	client.Cmd.Message(botNick, msg)
	return nil
}
```

- [ ] **Step 4: Install girc dependency and run tests**

Run:
```bash
go get github.com/lrstanley/girc
cd irc && go test -v ./...
```

Expected: all tests PASS (unit tests don't connect to a real server).

- [ ] **Step 5: Commit**

```bash
git add irc/connection.go irc/connection_test.go go.mod go.sum
git commit -m "feat: add IRC connection with auth, channels, and message routing"
```

---

### Task 3: IRC Manager

**Files:**
- Create: `irc/manager.go`
- Create: `irc/manager_test.go`

- [ ] **Step 1: Write failing tests for the manager**

Create `irc/manager_test.go`:

```go
package irc

import (
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

type mockStore struct {
	servers  []db.Server
	channels map[int64][]db.Channel
}

func (m *mockStore) GetServers() ([]db.Server, error)                   { return m.servers, nil }
func (m *mockStore) GetServer(id int64) (*db.Server, error)             { return nil, nil }
func (m *mockStore) CreateServer(s *db.Server) error                    { return nil }
func (m *mockStore) UpdateServer(s *db.Server) error                    { return nil }
func (m *mockStore) DeleteServer(id int64) error                        { return nil }
func (m *mockStore) GetChannels(serverID int64) ([]db.Channel, error)   { return m.channels[serverID], nil }
func (m *mockStore) GetChannel(id int64) (*db.Channel, error)           { return nil, nil }
func (m *mockStore) CreateChannel(c *db.Channel) error                  { return nil }
func (m *mockStore) UpdateChannel(c *db.Channel) error                  { return nil }
func (m *mockStore) DeleteChannel(id int64) error                       { return nil }
func (m *mockStore) GetDownloads(status string) ([]db.Download, error)  { return nil, nil }
func (m *mockStore) GetDownload(id int64) (*db.Download, error)         { return nil, nil }
func (m *mockStore) CreateDownload(d *db.Download) error                { return nil }
func (m *mockStore) UpdateDownload(d *db.Download) error                { return nil }
func (m *mockStore) GetSearchResults(q string, sid int64, ch string) ([]db.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) CreateSearchResult(r *db.SearchResult) error        { return nil }
func (m *mockStore) GetSavedSearches() ([]db.SavedSearch, error)        { return nil, nil }
func (m *mockStore) CreateSavedSearch(s *db.SavedSearch) error          { return nil }
func (m *mockStore) DeleteSavedSearch(id int64) error                   { return nil }
func (m *mockStore) GetParsePatterns() ([]db.ParsePattern, error)       { return nil, nil }
func (m *mockStore) UpdateParsePattern(p *db.ParsePattern) error        { return nil }
func (m *mockStore) CreateParsePattern(p *db.ParsePattern) error        { return nil }
func (m *mockStore) GetPostHooks(scope string, scopeID *int64) ([]db.PostHook, error) {
	return nil, nil
}
func (m *mockStore) CreatePostHook(h *db.PostHook) error                { return nil }
func (m *mockStore) UpdatePostHook(h *db.PostHook) error                { return nil }
func (m *mockStore) DeletePostHook(id int64) error                      { return nil }
func (m *mockStore) GetFileRoutingRules() ([]db.FileRoutingRule, error) { return nil, nil }
func (m *mockStore) CreateFileRoutingRule(r *db.FileRoutingRule) error   { return nil }
func (m *mockStore) UpdateFileRoutingRule(r *db.FileRoutingRule) error   { return nil }
func (m *mockStore) DeleteFileRoutingRule(id int64) error               { return nil }
func (m *mockStore) Close() error                                       { return nil }
func (m *mockStore) Migrate() error                                     { return nil }

func TestManager_New(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", AutoConnect: true, Enabled: true},
			{ID: 2, Name: "srv2", Host: "b.com", Port: 6697, Nickname: "bot", AutoConnect: false, Enabled: true},
		},
		channels: map[int64][]db.Channel{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
			2: {{ID: 2, ServerID: 2, Name: "#other", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)

	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManager_GetStatus(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", AutoConnect: false, Enabled: true},
		},
		channels: map[int64][]db.Channel{
			1: {{ID: 1, ServerID: 1, Name: "#test", AutoJoin: true, Enabled: true}},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)

	if err := mgr.LoadFromStore(); err != nil {
		t.Fatalf("LoadFromStore failed: %v", err)
	}

	statuses := mgr.GetStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].ServerName != "srv1" {
		t.Errorf("expected server name srv1, got %s", statuses[0].ServerName)
	}
	if statuses[0].Status != StatusDisconnected {
		t.Errorf("expected disconnected, got %s", statuses[0].Status)
	}
}

func TestManager_GetConnection(t *testing.T) {
	store := &mockStore{
		servers: []db.Server{
			{ID: 1, Name: "srv1", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true},
		},
		channels: map[int64][]db.Channel{
			1: {},
		},
	}

	bus := NewEventBus()
	mgr := NewManager(store, bus)
	mgr.LoadFromStore()

	conn := mgr.GetConnection(1)
	if conn == nil {
		t.Fatal("expected connection for server 1")
	}

	conn2 := mgr.GetConnection(999)
	if conn2 != nil {
		t.Error("expected nil for nonexistent server")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd irc && go test -v -run TestManager ./...
```

Expected: compilation error — `Manager`, `NewManager` don't exist.

- [ ] **Step 3: Implement manager**

Create `irc/manager.go`:

```go
package irc

import (
	"fmt"
	"log"
	"sync"

	"github.com/maxwell-xirc/xirc/db"
)

type ServerStatus struct {
	ServerID   int64            `json:"server_id"`
	ServerName string           `json:"server_name"`
	Status     ConnectionStatus `json:"status"`
	Channels   []string         `json:"channels"`
}

type Manager struct {
	mu          sync.RWMutex
	store       db.Store
	bus         *EventBus
	connections map[int64]*Connection
}

func NewManager(store db.Store, bus *EventBus) *Manager {
	return &Manager{
		store:       store,
		bus:         bus,
		connections: make(map[int64]*Connection),
	}
}

func (m *Manager) EventBus() *EventBus {
	return m.bus
}

func (m *Manager) LoadFromStore() error {
	servers, err := m.store.GetServers()
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, srv := range servers {
		if !srv.Enabled {
			continue
		}

		channels, err := m.store.GetChannels(srv.ID)
		if err != nil {
			return fmt.Errorf("loading channels for server %s: %w", srv.Name, err)
		}

		srvCopy := srv
		conn := NewConnection(&srvCopy, channels, m.bus)
		m.connections[srv.ID] = conn
	}

	return nil
}

func (m *Manager) ConnectAutoConnect() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.connections {
		if conn.server.AutoConnect {
			log.Printf("auto-connecting to %s", conn.server.Name)
			if err := conn.Connect(); err != nil {
				log.Printf("failed to connect to %s: %v", conn.server.Name, err)
			}
		}
	}
}

func (m *Manager) ConnectServer(serverID int64) error {
	m.mu.RLock()
	conn, ok := m.connections[serverID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("server %d not found", serverID)
	}

	return conn.Connect()
}

func (m *Manager) DisconnectServer(serverID int64) error {
	m.mu.RLock()
	conn, ok := m.connections[serverID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("server %d not found", serverID)
	}

	conn.Disconnect()
	return nil
}

func (m *Manager) GetConnection(serverID int64) *Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[serverID]
}

func (m *Manager) GetStatuses() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []ServerStatus
	for _, conn := range m.connections {
		statuses = append(statuses, ServerStatus{
			ServerID:   conn.ServerID(),
			ServerName: conn.ServerName(),
			Status:     conn.Status(),
			Channels:   conn.AllChannelNames(),
		})
	}
	return statuses
}

func (m *Manager) ReloadServer(serverID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Disconnect existing if present
	if existing, ok := m.connections[serverID]; ok {
		existing.Disconnect()
		delete(m.connections, serverID)
	}

	srv, err := m.store.GetServer(serverID)
	if err != nil {
		return fmt.Errorf("loading server %d: %w", serverID, err)
	}
	if !srv.Enabled {
		return nil
	}

	channels, err := m.store.GetChannels(serverID)
	if err != nil {
		return fmt.Errorf("loading channels for server %d: %w", serverID, err)
	}

	conn := NewConnection(srv, channels, m.bus)
	m.connections[serverID] = conn
	return nil
}

func (m *Manager) Shutdown() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.connections {
		conn.Disconnect()
	}
}

func (m *Manager) SendMessage(serverID int64, target, message string) error {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("server %d not found", serverID)
	}
	conn.SendMessage(target, message)
	return nil
}

func (m *Manager) SendRaw(serverID int64, raw string) error {
	conn := m.GetConnection(serverID)
	if conn == nil {
		return fmt.Errorf("server %d not found", serverID)
	}
	conn.SendRaw(raw)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd irc && go test -v -run TestManager ./...
```

Expected: all 3 manager tests PASS.

- [ ] **Step 5: Commit**

```bash
git add irc/manager.go irc/manager_test.go
git commit -m "feat: add IRC manager for multi-server connection lifecycle"
```

---

### Task 4: IRC HTTP API Handlers

**Files:**
- Create: `server/irc_handlers.go`
- Create: `server/irc_handlers_test.go`
- Modify: `server/server.go`

- [ ] **Step 1: Write failing tests for IRC API endpoints**

Create `server/irc_handlers_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/irc"
)

func TestGetIRCStatus(t *testing.T) {
	bus := irc.NewEventBus()
	srv := New(nil, irc.NewManager(nil, bus))

	req := httptest.NewRequest("GET", "/api/irc/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp []irc.ServerStatus
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
}

func TestPostIRCConnect_NoServer(t *testing.T) {
	bus := irc.NewEventBus()
	srv := New(nil, irc.NewManager(nil, bus))

	req := httptest.NewRequest("POST", "/api/irc/connect", strings.NewReader(`{"server_id": 999}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	// Should fail because server 999 doesn't exist
	if w.Code == http.StatusOK {
		t.Error("expected non-200 for nonexistent server")
	}
}

func TestPostIRCSendMessage_NoServer(t *testing.T) {
	bus := irc.NewEventBus()
	srv := New(nil, irc.NewManager(nil, bus))

	body := `{"server_id": 999, "target": "#test", "message": "hello"}`
	req := httptest.NewRequest("POST", "/api/irc/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected error for nonexistent server")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd server && go test -v ./...
```

Expected: compilation error — `New` signature changed, `irc_handlers.go` doesn't exist.

- [ ] **Step 3: Update server.go to accept IRC manager**

Replace `server/server.go` with:

```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

type Server struct {
	store   db.Store
	ircMgr  *irc.Manager
	mux     *http.ServeMux
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
	s.mux.HandleFunc("GET /api/health", s.handleHealth)

	// IRC endpoints
	s.mux.HandleFunc("GET /api/irc/status", s.handleIRCStatus)
	s.mux.HandleFunc("POST /api/irc/connect", s.handleIRCConnect)
	s.mux.HandleFunc("POST /api/irc/disconnect", s.handleIRCDisconnect)
	s.mux.HandleFunc("POST /api/irc/message", s.handleIRCSendMessage)
	s.mux.HandleFunc("POST /api/irc/raw", s.handleIRCSendRaw)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
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
```

- [ ] **Step 4: Create IRC handlers**

Create `server/irc_handlers.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleIRCStatus(w http.ResponseWriter, r *http.Request) {
	if s.ircMgr == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, s.ircMgr.GetStatuses())
}

func (s *Server) handleIRCConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID int64 `json:"server_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.ircMgr == nil {
		writeError(w, http.StatusInternalServerError, "IRC manager not initialized")
		return
	}

	if err := s.ircMgr.ConnectServer(req.ServerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "connecting"})
}

func (s *Server) handleIRCDisconnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID int64 `json:"server_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.ircMgr == nil {
		writeError(w, http.StatusInternalServerError, "IRC manager not initialized")
		return
	}

	if err := s.ircMgr.DisconnectServer(req.ServerID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) handleIRCSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID int64  `json:"server_id"`
		Target   string `json:"target"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.ircMgr == nil {
		writeError(w, http.StatusInternalServerError, "IRC manager not initialized")
		return
	}

	if err := s.ircMgr.SendMessage(req.ServerID, req.Target, req.Message); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleIRCSendRaw(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID int64  `json:"server_id"`
		Command  string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.ircMgr == nil {
		writeError(w, http.StatusInternalServerError, "IRC manager not initialized")
		return
	}

	if err := s.ircMgr.SendRaw(req.ServerID, req.Command); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
```

- [ ] **Step 5: Fix the health endpoint test**

Update `server/server_test.go` to pass the new `New` signature:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := New(nil, nil)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestNotFoundReturns404(t *testing.T) {
	srv := New(nil, nil)
	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 6: Run all server tests**

Run:
```bash
cd server && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 7: Commit**

```bash
git add server/
git commit -m "feat: add IRC API endpoints (status, connect, disconnect, message, raw)"
```

---

### Task 5: Update main.go to Wire IRC Manager

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Update main.go**

Replace `main.go` with:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	ircpkg "github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dsn := cfg.Database.Path
	if cfg.Database.Driver == "mysql" {
		dsn = cfg.Database.DSN
	}

	store, err := db.NewStore(cfg.Database.Driver, dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	bus := ircpkg.NewEventBus()
	ircMgr := ircpkg.NewManager(store, bus)

	if err := ircMgr.LoadFromStore(); err != nil {
		log.Printf("warning: failed to load IRC servers: %v", err)
	}

	ircMgr.ConnectAutoConnect()

	srv := server.New(store, ircMgr)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		ircMgr.Shutdown()
		httpServer.Close()
	}()

	log.Printf("xirc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 2: Verify everything compiles**

Run:
```bash
go build -o xirc .
```

Expected: compiles without error.

- [ ] **Step 3: Run all tests**

Run:
```bash
go test ./...
```

Expected: all tests PASS across all packages.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: wire IRC manager into main with auto-connect"
```

---

## End State

After completing Plan 2, you have:

- An event bus for decoupled message fan-out across the system.
- IRC connections that handle NickServ, SASL, and no-auth modes.
- Channel pair awareness — search channel vs download channel.
- Auto-connect and auto-join with opt-out per server/channel.
- Reconnection with exponential backoff.
- All IRC messages (PRIVMSG, NOTICE, CTCP) routed through the event bus.
- HTTP API endpoints: `/api/irc/status`, `/api/irc/connect`, `/api/irc/disconnect`, `/api/irc/message`, `/api/irc/raw`.
- Ready for Plan 3 (Search Parser) to subscribe to IRC events and parse XDCC results.

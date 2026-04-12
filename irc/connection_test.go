package irc

import (
	"sync"
	"testing"
	"time"

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

// --- newIRCClient nickname fallback tests ---

func TestNewIRCClient_UsesNicknameWhenSet(t *testing.T) {
	srv := &db.Server{
		ID:       1,
		Name:     "test",
		Host:     "irc.example.com",
		Port:     6667,
		Nickname: "mynick",
		Enabled:  true,
	}
	client := newIRCClient(srv)
	if client.Nick() != "mynick" {
		t.Errorf("expected nick %q, got %q", "mynick", client.Nick())
	}
}

func TestNewIRCClient_FallsBackToXircWhenEmpty(t *testing.T) {
	srv := &db.Server{
		ID:       2,
		Name:     "test",
		Host:     "irc.example.com",
		Port:     6667,
		Nickname: "",
		Enabled:  true,
	}
	client := newIRCClient(srv)
	if client.Nick() != "xirc" {
		t.Errorf("expected fallback nick %q, got %q", "xirc", client.Nick())
	}
}

// --- handleRawLine event bus publishing tests ---

func TestHandleRawLine_PublishesNonPongLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 42, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Channel{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine(":server.example.com 001 bot :Welcome to IRC")

	select {
	case ev := <-ch:
		if ev.Type != EventIRCMessage {
			t.Errorf("expected event type %s, got %s", EventIRCMessage, ev.Type)
		}
		if ev.Channel != "" {
			t.Errorf("expected empty channel for server-level message, got %q", ev.Channel)
		}
		if ev.ServerID != 42 {
			t.Errorf("expected server_id 42, got %d", ev.ServerID)
		}
		data, ok := ev.Data.(map[string]string)
		if !ok {
			t.Fatalf("expected Data to be map[string]string, got %T", ev.Data)
		}
		if data["type"] != "raw" {
			t.Errorf("expected data[type]=%q, got %q", "raw", data["type"])
		}
		if data["message"] != ":server.example.com 001 bot :Welcome to IRC" {
			t.Errorf("expected raw line in data[message], got %q", data["message"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for raw line event")
	}
}

func TestHandleRawLine_DoesNotPublishPongLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Channel{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine("PONG :token123")

	select {
	case ev := <-ch:
		t.Errorf("expected no event for PONG line, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// correct: nothing published
	}
}

func TestHandleRawLine_DoesNotPublishServerPrefixedPongLine(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	conn := NewConnection(srv, []db.Channel{}, bus)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	conn.handleRawLine(":irc.server.net PONG irc.server.net :token123")

	select {
	case ev := <-ch:
		t.Errorf("expected no event for server-prefixed PONG line, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
		// correct: nothing published
	}
}

func TestConnection_UpdateChannels_ThreadSafe(t *testing.T) {
	bus := NewEventBus()
	srv := &db.Server{ID: 1, Name: "test", Host: "irc.example.com", Port: 6667, Nickname: "testbot", Enabled: true}
	conn := NewConnection(srv, []db.Channel{
		{ID: 1, ServerID: 1, Name: "#search", AutoJoin: true, Enabled: true},
	}, bus)

	newChannels := []db.Channel{
		{ID: 2, ServerID: 1, Name: "#downloads", AutoJoin: true, Enabled: true},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = conn.AllChannelNames()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			conn.UpdateChannels(newChannels)
		}
	}()

	wg.Wait()

	if len(conn.AllChannelNames()) == 0 {
		t.Fatal("expected channel names after concurrent update/read")
	}
}

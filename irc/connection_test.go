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

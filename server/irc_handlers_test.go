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
	srv := New(nil, irc.NewManager(nil, bus), nil, nil, nil, nil, nil, nil)

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
	srv := New(nil, irc.NewManager(nil, bus), nil, nil, nil, nil, nil, nil)

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
	srv := New(nil, irc.NewManager(nil, bus), nil, nil, nil, nil, nil, nil)

	body := `{"server_id": 999, "target": "#test", "message": "hello"}`
	req := httptest.NewRequest("POST", "/api/irc/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected error for nonexistent server")
	}
}

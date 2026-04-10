package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestGetServers_Empty(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/servers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var servers []db.Server
	if err := json.NewDecoder(w.Body).Decode(&servers); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected empty slice, got %d servers", len(servers))
	}
}

func TestCreateServer(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"name":"testserver","host":"irc.example.com","port":6667,"nickname":"bot","enabled":true}`
	req := httptest.NewRequest("POST", "/api/servers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result db.Server
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ID == 0 {
		t.Errorf("expected server ID to be set, got 0")
	}
	if result.Name != "testserver" {
		t.Errorf("expected name testserver, got %q", result.Name)
	}
}

func TestUpdateServer(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "old", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	body := fmt.Sprintf(`{"id":%d,"name":"updated","host":"irc.example.com","port":6667,"nickname":"bot","enabled":true}`, s.ID)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/servers/%d", s.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result db.Server
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Name != "updated" {
		t.Errorf("expected name updated, got %q", result.Name)
	}
}

func TestDeleteServer(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "tobedeleted", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/servers/%d", s.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["status"] != "deleted" {
		t.Errorf("expected status deleted, got %q", result["status"])
	}
}

func TestGetChannelsForServer(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/servers/%d/channels", s.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var channels []db.Channel
	if err := json.NewDecoder(w.Body).Decode(&channels); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected empty slice, got %d channels", len(channels))
	}
}

func TestCreateChannel(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	body := fmt.Sprintf(`{"server_id":%d,"name":"#test","auto_join":true,"enabled":true}`, s.ID)
	req := httptest.NewRequest("POST", "/api/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result db.Channel
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ID == 0 {
		t.Errorf("expected channel ID to be set, got 0")
	}
	if result.Name != "#test" {
		t.Errorf("expected name #test, got %q", result.Name)
	}
}

func TestUpdateChannel(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	ch := &db.Channel{ServerID: s.ID, Name: "#old", AutoJoin: true, Enabled: true}
	store.CreateChannel(ch)

	body := fmt.Sprintf(`{"server_id":%d,"name":"#new","auto_join":true,"enabled":true}`, s.ID)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/channels/%d", ch.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result db.Channel
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Name != "#new" {
		t.Errorf("expected name #new, got %q", result.Name)
	}
}

func TestDeleteChannel(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	ch := &db.Channel{ServerID: s.ID, Name: "#tobedeleted", AutoJoin: false, Enabled: true}
	store.CreateChannel(ch)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/channels/%d", ch.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["status"] != "deleted" {
		t.Errorf("expected status deleted, got %q", result["status"])
	}
}

func TestUpdateServer_ZeroID(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/api/servers/0", strings.NewReader(`{"name":"x","host":"h","port":6667,"nickname":"n"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for zero ID, got %d", w.Code)
	}
}

func TestDeleteServer_ZeroID(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/servers/0", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for zero ID, got %d", w.Code)
	}
}

func TestMethodNotAllowed_Servers(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/servers", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestMethodNotAllowed_Channels(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/channels", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

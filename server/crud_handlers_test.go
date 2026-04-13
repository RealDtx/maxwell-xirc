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

func TestGetRealmsForServer(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/servers/%d/realms", s.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var realms []db.Realm
	if err := json.NewDecoder(w.Body).Decode(&realms); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(realms) != 0 {
		t.Errorf("expected empty slice, got %d realms", len(realms))
	}
}

func TestCreateRealm(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	body := fmt.Sprintf(`{"server_id":%d,"name":"#test","auto_join":true,"enabled":true}`, s.ID)
	req := httptest.NewRequest("POST", "/api/realms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result db.Realm
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ID == 0 {
		t.Errorf("expected realm ID to be set, got 0")
	}
	if result.Name != "#test" {
		t.Errorf("expected name #test, got %q", result.Name)
	}
}

func TestUpdateRealm(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	ch := &db.Realm{ServerID: s.ID, Name: "#old", AutoJoin: true, Enabled: true}
	store.CreateRealm(ch)

	body := fmt.Sprintf(`{"server_id":%d,"name":"#new","auto_join":true,"enabled":true}`, s.ID)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/realms/%d", ch.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result db.Realm
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Name != "#new" {
		t.Errorf("expected name #new, got %q", result.Name)
	}
}

func TestDeleteRealm(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	ch := &db.Realm{ServerID: s.ID, Name: "#tobedeleted", AutoJoin: false, Enabled: true}
	store.CreateRealm(ch)

	req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/realms/%d", ch.ID), nil)
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

func TestMethodNotAllowed_Realms(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/realms", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetRealms_ByServerID(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s1 := &db.Server{Name: "srv1", Host: "irc.example.com", Port: 6667, Nickname: "bot1", Enabled: true}
	s2 := &db.Server{Name: "srv2", Host: "irc2.example.com", Port: 6667, Nickname: "bot2", Enabled: true}
	if err := store.CreateServer(s1); err != nil {
		t.Fatalf("CreateServer s1 failed: %v", err)
	}
	if err := store.CreateServer(s2); err != nil {
		t.Fatalf("CreateServer s2 failed: %v", err)
	}

	if err := store.CreateRealm(&db.Realm{ServerID: s1.ID, Name: "#one", AutoJoin: true, Enabled: true}); err != nil {
		t.Fatalf("CreateRealm #one failed: %v", err)
	}
	if err := store.CreateRealm(&db.Realm{ServerID: s1.ID, Name: "#two", AutoJoin: false, Enabled: true}); err != nil {
		t.Fatalf("CreateRealm #two failed: %v", err)
	}
	if err := store.CreateRealm(&db.Realm{ServerID: s2.ID, Name: "#other", AutoJoin: true, Enabled: true}); err != nil {
		t.Fatalf("CreateRealm #other failed: %v", err)
	}

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/realms?server_id=%d", s1.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var realms []db.Realm
	if err := json.NewDecoder(w.Body).Decode(&realms); err != nil {
		t.Fatalf("failed to decode realms: %v", err)
	}
	if len(realms) != 2 {
		t.Fatalf("expected 2 realms for server %d, got %d", s1.ID, len(realms))
	}
	for _, realm := range realms {
		if realm.ServerID != s1.ID {
			t.Fatalf("expected only server %d realms, got server %d", s1.ID, realm.ServerID)
		}
	}
}

// --- Nickname validation tests ---

func TestCreateServer_RejectsEmptyNickname(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"name":"testserver","host":"irc.example.com","port":6667,"nickname":"","enabled":true}`
	req := httptest.NewRequest("POST", "/api/servers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty nickname, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateServer_RejectsMissingNickname(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"name":"testserver","host":"irc.example.com","port":6667,"enabled":true}`
	req := httptest.NewRequest("POST", "/api/servers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing nickname, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateServer_RejectsEmptyNickname(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "old", Host: "irc.example.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	body := fmt.Sprintf(`{"id":%d,"name":"old","host":"irc.example.com","port":6667,"nickname":"","enabled":true}`, s.ID)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/servers/%d", s.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty nickname on update, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateServer_AcceptsValidNickname(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"name":"testserver","host":"irc.example.com","port":6667,"nickname":"mybot","enabled":true}`
	req := httptest.NewRequest("POST", "/api/servers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for valid nickname, got %d: %s", w.Code, w.Body.String())
	}
}

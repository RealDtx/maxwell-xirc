package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/discovery"
	"github.com/RealDtx/maxwell-irc/irc"
)

func discoveryFixture(t *testing.T) (*Server, db.Store, int64, func()) {
	srv, store, cleanup := newTestServerWithStore(t)
	sv := &db.Server{Name: "s", Host: "irc.example.org", Port: 6667, Nickname: "me"}
	if err := store.CreateServer(sv); err != nil {
		t.Fatal(err)
	}
	srv.SetDiscovery(discovery.New(store, irc.NewEventBus(), func(int64) discovery.Conn { return nil }, config.DiscoveryConfig{}))
	if err := store.UpsertDiscoveredChannel(&db.DiscoveredChannel{ServerID: sv.ID, Channel: "#example", Type: "chat", Reason: "keyword: xdcc"}); err != nil {
		t.Fatal(err)
	}
	return srv, store, sv.ID, cleanup
}

func discDo(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestDiscoveryListDismissSeen(t *testing.T) {
	srv, _, sid, cleanup := discoveryFixture(t)
	defer cleanup()

	w := discDo(srv, "GET", "/api/discovery?server_id=1", "")
	var resp struct {
		Channels []db.DiscoveredChannel `json:"channels"`
		Scan     discovery.ScanStatus   `json:"scan"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &resp) != nil {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if len(resp.Channels) != 1 || resp.Scan.State != "idle" {
		t.Fatalf("got %+v", resp)
	}

	w = discDo(srv, "GET", "/api/discovery/unseen", "")
	var unseen map[string]int
	json.Unmarshal(w.Body.Bytes(), &unseen)
	if unseen["1"] != 1 {
		t.Fatalf("unseen %v (server %d)", unseen, sid)
	}
	if w = discDo(srv, "POST", "/api/discovery/seen", `{"server_id":1}`); w.Code != 200 {
		t.Fatalf("seen: %d", w.Code)
	}
	w = discDo(srv, "GET", "/api/discovery/unseen", "")
	unseen = nil
	json.Unmarshal(w.Body.Bytes(), &unseen)
	if unseen["1"] != 0 {
		t.Fatalf("unseen after seen %v", unseen)
	}

	id := resp.Channels[0].ID
	if w = discDo(srv, "POST", "/api/discovery/dismiss", `{"id":`+fmt.Sprint(id)+`}`); w.Code != 200 {
		t.Fatalf("dismiss: %d", w.Code)
	}
	w = discDo(srv, "GET", "/api/discovery?server_id=1", "")
	if !strings.Contains(w.Body.String(), `"channels":[]`) {
		t.Fatalf("expected empty channels: %s", w.Body.String())
	}
}

func TestDiscoveryScanNotConnected(t *testing.T) {
	srv, _, _, cleanup := discoveryFixture(t)
	defer cleanup()
	if w := discDo(srv, "POST", "/api/discovery/scan", `{"server_id":1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestDiscoveryListHidesRealmChannels(t *testing.T) {
	srv, store, sid, cleanup := discoveryFixture(t)
	defer cleanup()
	for _, d := range []*db.DiscoveredChannel{
		{ServerID: sid, Channel: "#example-chat", Partner: "#example-dl", Type: "chat"},
		{ServerID: sid, Channel: "#other", Type: "unknown"},
	} {
		if err := store.UpsertDiscoveredChannel(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateRealm(&db.Realm{ServerID: sid, Name: "#Example", DownloadChannel: "#example-dl", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	w := discDo(srv, "GET", fmt.Sprintf("/api/discovery?server_id=%d", sid), "")
	var resp struct {
		Channels []db.DiscoveredChannel `json:"channels"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &resp) != nil {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if len(resp.Channels) != 1 || resp.Channels[0].Channel != "#other" {
		t.Fatalf("got %+v", resp.Channels)
	}
}

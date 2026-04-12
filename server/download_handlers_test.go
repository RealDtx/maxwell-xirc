package server

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
	"github.com/maxwell-xirc/xirc/queue"
)

func newTestServerWithEngine(t *testing.T) (*Server, db.Store, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "xirc-engine-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create store: %v", err)
	}
	store.Migrate()

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	p := parser.New(store, bus)

	storageCfg := &config.StorageConfig{
		DownloadsDir: dir,
		TempDir:      filepath.Join(dir, "tmp"),
		MinFreeSpace: "0",
	}
	eng := queue.NewEngine(store, bus, storageCfg, 3)

	srv := New(store, ircMgr, p, eng, nil, nil, nil, nil)
	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return srv, store, cleanup
}

func TestGetDownloads(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	store.CreateDownload(&db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "queued"})

	req := httptest.NewRequest("GET", "/api/downloads", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var downloads []db.Download
	json.NewDecoder(w.Body).Decode(&downloads)
	if len(downloads) != 1 {
		t.Errorf("expected 1 download, got %d", len(downloads))
	}
}

func TestGetDownloads_FilterByStatus(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	store.CreateDownload(&db.Download{ServerID: s.ID, Channel: "#t", BotNick: "b1", PackNumber: 1, Status: "queued"})
	store.CreateDownload(&db.Download{ServerID: s.ID, Channel: "#t", BotNick: "b2", PackNumber: 2, Status: "completed"})

	req := httptest.NewRequest("GET", "/api/downloads?status=queued", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var downloads []db.Download
	json.NewDecoder(w.Body).Decode(&downloads)
	if len(downloads) != 1 {
		t.Errorf("expected 1 queued, got %d", len(downloads))
	}
}

func TestPostDownloadCancel(t *testing.T) {
	srv, store, cleanup := newTestServerWithEngine(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	dl := &db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "queued"}
	store.CreateDownload(dl)

	body := fmt.Sprintf(`{"download_id": %d}`, dl.ID)
	req := httptest.NewRequest("POST", "/api/downloads/cancel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostDownloadRetry(t *testing.T) {
	srv, store, cleanup := newTestServerWithEngine(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	dl := &db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "failed", ErrorMessage: "timeout"}
	store.CreateDownload(dl)

	body := fmt.Sprintf(`{"download_id": %d}`, dl.ID)
	req := httptest.NewRequest("POST", "/api/downloads/retry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostDownloadMove(t *testing.T) {
srv, store, cleanup := newTestServerWithEngine(t)
defer cleanup()

s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
store.CreateServer(s)
dl := &db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "queued"}
store.CreateDownload(dl)

body := fmt.Sprintf(`{"download_id": %d}`, dl.ID)
req := httptest.NewRequest("POST", "/api/downloads/move", strings.NewReader(body))
req.Header.Set("Content-Type", "application/json")
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusOK {
t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
}

var resp map[string]string
if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
t.Fatalf("failed to decode response: %v", err)
}
if resp["status"] != "moved" {
t.Errorf("expected status moved, got %q", resp["status"])
}
}

func TestPostDownloadMove_ZeroID(t *testing.T) {
srv, _, cleanup := newTestServerWithEngine(t)
defer cleanup()

req := httptest.NewRequest("POST", "/api/downloads/move", strings.NewReader(`{"download_id": 0}`))
req.Header.Set("Content-Type", "application/json")
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusBadRequest {
t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
}
}

func TestPostDownloadMove_NoEngine(t *testing.T) {
srv, store, cleanup := newTestServerWithStore(t)
defer cleanup()

s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
store.CreateServer(s)
dl := &db.Download{ServerID: s.ID, Channel: "#t", BotNick: "bot", PackNumber: 1, Status: "queued"}
store.CreateDownload(dl)

body := fmt.Sprintf(`{"download_id": %d}`, dl.ID)
req := httptest.NewRequest("POST", "/api/downloads/move", strings.NewReader(body))
req.Header.Set("Content-Type", "application/json")
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusInternalServerError {
t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
}
}

func TestGetDownloads_MethodNotAllowed(t *testing.T) {
srv, _, cleanup := newTestServerWithStore(t)
defer cleanup()

req := httptest.NewRequest("POST", "/api/downloads", nil)
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusMethodNotAllowed {
t.Errorf("expected 405, got %d", w.Code)
}
}

package server

import (
"encoding/json"
"net/http"
"net/http/httptest"
"testing"
"time"

"github.com/maxwell-xirc/xirc/db"
)

func TestGetDownloadStats(t *testing.T) {
srv, store, cleanup := newTestServerWithStore(t)
defer cleanup()

s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
store.CreateServer(s)

now := time.Now()
store.CreateDownloadStat(&db.DownloadStat{
ServerID:     s.ID,
Channel:      "#t",
BotNick:      "bot",
Filename:     "test.mkv",
SizeBytes:    1000000,
PackNumber:   1,
CompletedAt:  &now,
Status:       "completed",
StatsOnly:    false,
})

req := httptest.NewRequest("GET", "/api/stats/downloads", nil)
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusOK {
t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
}
var summary db.DownloadStatsSummary
json.NewDecoder(w.Body).Decode(&summary)
if summary.TotalTransfers != 1 {
t.Errorf("expected 1 download stat, got %d", summary.TotalTransfers)
}
}

func TestGetDownloadHistory(t *testing.T) {
srv, store, cleanup := newTestServerWithStore(t)
defer cleanup()

s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
store.CreateServer(s)

now := time.Now()
store.CreateDownloadStat(&db.DownloadStat{
ServerID:     s.ID,
Channel:      "#t",
BotNick:      "bot",
Filename:     "test.mkv",
SizeBytes:    2000000,
PackNumber:   1,
CompletedAt:  &now,
Status:       "completed",
StatsOnly:    false,
})

req := httptest.NewRequest("GET", "/api/stats/history?offset=0&limit=10", nil)
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusOK {
t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
}
var history []db.DownloadStat
json.NewDecoder(w.Body).Decode(&history)
if len(history) != 1 {
t.Errorf("expected 1 history entry, got %d", len(history))
}
}

func TestGetDownloadStats_MethodNotAllowed(t *testing.T) {
srv, _, cleanup := newTestServerWithStore(t)
defer cleanup()
m := httptest.NewRequest("POST", "/api/stats/downloads", nil)
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, m)
if w.Code != http.StatusMethodNotAllowed {
t.Errorf("expected 405, got %d", w.Code)
}
}

func TestGetStorageStats(t *testing.T) {
srv, store, cleanup := newTestServerWithStore(t)
defer cleanup()

// Use "/" as dest dir since it definitely exists on Linux
store.CreateFileRoutingRule(&db.FileRoutingRule{
Pattern: "*", DestinationDir: "/", Priority: 0, Enabled: true,
})

req := httptest.NewRequest("GET", "/api/storage", nil)
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)

if w.Code != http.StatusOK {
t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
}
var stats []interface{}
json.NewDecoder(w.Body).Decode(&stats)
if len(stats) == 0 {
t.Error("expected at least one storage stat entry")
}
}

func TestGetStorageStats_MethodNotAllowed(t *testing.T) {
srv, _, cleanup := newTestServerWithStore(t)
defer cleanup()
req := httptest.NewRequest("DELETE", "/api/storage", nil)
w := httptest.NewRecorder()
srv.Handler().ServeHTTP(w, req)
if w.Code != http.StatusMethodNotAllowed {
t.Errorf("expected 405, got %d", w.Code)
}
}

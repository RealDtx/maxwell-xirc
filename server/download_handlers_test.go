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
	srv, store, cleanup := newTestServerWithStore(t)
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
	srv, store, cleanup := newTestServerWithStore(t)
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

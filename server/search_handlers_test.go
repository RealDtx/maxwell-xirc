package server

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
)

func newTestServerWithStore(t *testing.T) (*Server, db.Store, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "xirc-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create store: %v", err)
	}
	store.Migrate()
	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	p := parser.New(store, bus)

	srv := New(store, ircMgr, p, nil)
	return srv, store, cleanup
}

func TestGetSearchResults(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	pack := 5
	fname := "movie.mkv"
	store.CreateSearchResult(&db.SearchResult{
		ServerID:    s.ID,
		Channel:     "#test",
		BotNick:     "bot1",
		PackNumber:  &pack,
		Filename:    &fname,
		RawLine:     "#5 [1.4G] movie.mkv",
		SearchQuery: "movie",
		Parsed:      true,
	})

	req := httptest.NewRequest("GET", "/api/search/results?query=movie&server_id=1&channel=%23test", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []db.SearchResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestGetSavedSearches(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	store.CreateSavedSearch(&db.SavedSearch{Name: "movies", ServerID: s.ID, Channel: "#test", Query: "movie"})

	req := httptest.NewRequest("GET", "/api/search/saved", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var searches []db.SavedSearch
	json.NewDecoder(w.Body).Decode(&searches)
	if len(searches) != 1 {
		t.Errorf("expected 1 saved search, got %d", len(searches))
	}
}

func TestCreateSavedSearch(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	body := `{"name":"test","server_id":1,"channel":"#test","query":"keyword"}`
	req := httptest.NewRequest("POST", "/api/search/saved", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetParsePatterns(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()
	parser.SeedPatterns(store)

	req := httptest.NewRequest("GET", "/api/search/patterns", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var patterns []db.ParsePattern
	json.NewDecoder(w.Body).Decode(&patterns)
	if len(patterns) == 0 {
		t.Error("expected built-in patterns")
	}
}

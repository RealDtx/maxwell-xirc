package server

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/parser"
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

	srv := New(store, ircMgr, p, nil, nil, nil, nil, nil, "", nil)
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

func TestDeleteSavedSearch(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)
	store.CreateSavedSearch(&db.SavedSearch{Name: "movies", ServerID: s.ID, Channel: "#test", Query: "movie"})

	req := httptest.NewRequest("DELETE", "/api/search/saved/1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "deleted" {
		t.Errorf("expected status deleted, got %q", resp["status"])
	}
}

func TestDeleteSavedSearch_InvalidID(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/search/saved/notanid", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateParsePattern(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"name":"test","regex":"#(\\\\d+).*"}`
	req := httptest.NewRequest("POST", "/api/search/patterns", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var p db.ParsePattern
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if p.ID == 0 {
		t.Error("expected pattern ID to be set")
	}
	if !p.Enabled {
		t.Error("expected pattern to be enabled")
	}
}

func TestUpdateParsePattern(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	pattern := &db.ParsePattern{Name: "initial", Regex: `#(\\d+).*`, Enabled: true}
	if err := store.CreateParsePattern(pattern); err != nil {
		t.Fatalf("failed to create pattern: %v", err)
	}

	body := `{"name":"updated","regex":"#(\\\\d+)-updated","enabled":false,"match_count":7}`
	req := httptest.NewRequest("PUT", "/api/search/patterns/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated db.ParsePattern
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if updated.ID != 1 {
		t.Errorf("expected ID 1, got %d", updated.ID)
	}
	if updated.Name != "updated" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}
	if updated.Enabled {
		t.Error("expected enabled=false after update")
	}
}

func TestGetUnmatchedSamples(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	if err := store.CreateSearchResult(&db.SearchResult{
		ServerID:    s.ID,
		Channel:     "#test",
		BotNick:     "bot1",
		RawLine:     "001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989",
		SearchQuery: "movie",
		Parsed:      false,
	}); err != nil {
		t.Fatalf("failed to create search result: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/search/unmatched?server_id=1&query=movie&since="+strconv.FormatInt(time.Now().Add(-1*time.Hour).UnixMilli(), 10)+"&limit=20", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []db.SearchResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RawLine == "" {
		t.Fatal("expected raw_line in result")
	}
}

func TestLearnPatternPreview(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	raw := "001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989"
	body := `{"raw_line":"` + raw + `","annotations":[` +
		`{"start":0,"end":3,"field":"skip_number"},` +
		`{"start":5,"end":8,"field":"downloads_count"},` +
		`{"start":12,"end":16,"field":"filesize"},` +
		`{"start":19,"end":33,"field":"filename"},` +
		`{"start":41,"end":49,"field":"bot_nick"},` +
		`{"start":60,"end":63,"field":"pack_number"}` +
		`],"preview":true}`
	req := httptest.NewRequest("POST", "/api/search/patterns/learn", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["regex"] == "" || resp["field_mapping"] == "" {
		t.Fatalf("expected regex and field_mapping, got %+v", resp)
	}
}

func TestImportPatterns(t *testing.T) {
	tests := []struct {
		name           string
		preCreate      []db.ParsePattern // patterns to insert into DB before the request
		body           string
		wantStatus     int
		wantImported   int
		wantSkipped    int
		wantErrContain string
	}{
		{
			name: "happy path: two new patterns",
			body: `{"patterns":[
				{"name":"pat-a","regex":"#(\\d+)","field_mapping":{"pack_number":1},"priority":10},
				{"name":"pat-b","regex":"(\\w+)\\.mkv","field_mapping":{"filename":1},"priority":5}
			]}`,
			wantStatus:   http.StatusOK,
			wantImported: 2,
			wantSkipped:  0,
		},
		{
			name: "duplicate name is skipped",
			preCreate: []db.ParsePattern{
				{Name: "foo", Regex: `#(\d+)`, FieldMapping: "{}", Enabled: true},
			},
			body: `{"patterns":[
				{"name":"foo","regex":"#(\\d+)","field_mapping":{}},
				{"name":"bar","regex":"(\\w+)","field_mapping":{}}
			]}`,
			wantStatus:   http.StatusOK,
			wantImported: 1,
			wantSkipped:  1,
		},
		{
			name: "invalid regex returns 400",
			body: `{"patterns":[{"name":"bad","regex":"(unclosed"}]}`,
			wantStatus:     http.StatusBadRequest,
			wantErrContain: "invalid regex",
		},
		{
			name:           "regex too long returns 400",
			body:           `{"patterns":[{"name":"toolong","regex":"` + strings.Repeat("a", 2000) + `"}]}`,
			wantStatus:     http.StatusBadRequest,
			wantErrContain: "regex exceeds",
		},
		{
			name:           "invalid JSON in field_mapping returns 400",
			body:           `{"patterns":[{"name":"badfm","regex":"#(\\d+)","field_mapping":"not-json"}]}`,
			wantStatus:     http.StatusBadRequest,
			wantErrContain: "invalid field_mapping",
		},
		{
			name:           "empty pattern name returns 400",
			body:           `{"patterns":[{"name":"","regex":"#(\\d+)"}]}`,
			wantStatus:     http.StatusBadRequest,
			wantErrContain: "name required",
		},
		{
			name: "missing field_mapping defaults to {}",
			body: `{"patterns":[{"name":"nomap","regex":"#(\\d+)"}]}`,
			wantStatus:   http.StatusOK,
			wantImported: 1,
			wantSkipped:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, store, cleanup := newTestServerWithStore(t)
			defer cleanup()

			for i := range tc.preCreate {
				if err := store.CreateParsePattern(&tc.preCreate[i]); err != nil {
					t.Fatalf("preCreate pattern: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/api/search/patterns/import", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}

			if tc.wantStatus == http.StatusOK {
				var resp map[string]int
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp["imported"] != tc.wantImported {
					t.Errorf("expected imported=%d, got %d", tc.wantImported, resp["imported"])
				}
				if resp["skipped"] != tc.wantSkipped {
					t.Errorf("expected skipped=%d, got %d", tc.wantSkipped, resp["skipped"])
				}
			}

			if tc.wantErrContain != "" {
				body := w.Body.String()
				if !strings.Contains(body, tc.wantErrContain) {
					t.Errorf("expected response to contain %q, got: %s", tc.wantErrContain, body)
				}
			}
		})
	}
}

func TestImportPatterns_MissingFieldMappingStoredAsEmptyObject(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"patterns":[{"name":"nomap","regex":"#(\\d+)"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/search/patterns/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	patterns, err := store.GetAllParsePatterns()
	if err != nil {
		t.Fatalf("GetAllParsePatterns: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("expected at least one pattern")
	}
	found := false
	for _, p := range patterns {
		if p.Name == "nomap" {
			found = true
			if p.FieldMapping != "{}" {
				t.Errorf("expected field_mapping={}, got %q", p.FieldMapping)
			}
			break
		}
	}
	if !found {
		t.Error("pattern 'nomap' not found in DB")
	}
}

func TestLearnPatternSaveAndReprocess(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	raw := "001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989"
	if err := store.CreateSearchResult(&db.SearchResult{
		ServerID:    s.ID,
		Channel:     "#test",
		BotNick:     "fallback-bot",
		RawLine:     raw,
		SearchQuery: "movie",
		Parsed:      false,
	}); err != nil {
		t.Fatalf("failed to create unmatched result: %v", err)
	}

	body := `{"raw_line":"` + raw + `","annotations":[` +
		`{"start":0,"end":3,"field":"skip_number"},` +
		`{"start":5,"end":8,"field":"downloads_count"},` +
		`{"start":12,"end":16,"field":"filesize"},` +
		`{"start":19,"end":33,"field":"filename"},` +
		`{"start":41,"end":49,"field":"bot_nick"},` +
		`{"start":60,"end":63,"field":"pack_number"}` +
		`],"name":"my-custom-pattern","preview":false}`
	req := httptest.NewRequest("POST", "/api/search/patterns/learn", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		PatternID   int64  `json:"pattern_id"`
		Regex       string `json:"regex"`
		FieldMap    string `json:"field_mapping"`
		NewlyParsed int    `json:"newly_parsed"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.PatternID == 0 {
		t.Fatal("expected pattern_id to be set")
	}
	if resp.NewlyParsed != 1 {
		t.Fatalf("expected newly_parsed=1, got %d", resp.NewlyParsed)
	}

	results, err := store.GetAllSearchResults("movie", nil)
	if err != nil {
		t.Fatalf("failed to query results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if !results[0].Parsed {
		t.Fatal("expected result to be marked parsed")
	}
	if results[0].PackNumber == nil || *results[0].PackNumber != 989 {
		t.Fatalf("expected pack_number=989, got %#v", results[0].PackNumber)
	}
}

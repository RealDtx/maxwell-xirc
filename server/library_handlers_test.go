package server

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/library"
)

func newTestServerWithLibrary(t *testing.T) (*Server, string, func()) {
	t.Helper()
	srv, _, cleanupStore := newTestServerWithStore(t)
	mediaRoot, err := ioutil.TempDir("", "xirc-library-*")
	if err != nil {
		t.Fatal(err)
	}
	cfg := library.Detect(mediaRoot)
	srv.SetLibrary(library.NewManager(mediaRoot+"/categories.yaml", cfg))
	cleanup := func() {
		cleanupStore()
		os.RemoveAll(mediaRoot)
	}
	return srv, mediaRoot, cleanup
}

func TestHandleLibrary_GetPut(t *testing.T) {
	srv, mediaRoot, cleanup := newTestServerWithLibrary(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/library", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cfg library.Config
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MediaRoot != mediaRoot {
		t.Errorf("media_root = %q, want %q", cfg.MediaRoot, mediaRoot)
	}

	cfg.SearchDepth = 5
	body, _ := json.Marshal(cfg)
	req = httptest.NewRequest("PUT", "/api/library", strings.NewReader(string(body)))
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got := srv.library.Get()
	if got.SearchDepth != 5 {
		t.Errorf("search_depth after PUT = %d, want 5", got.SearchDepth)
	}
}

func TestHandleLibrary_PutInvalidMediaRoot(t *testing.T) {
	srv, _, cleanup := newTestServerWithLibrary(t)
	defer cleanup()

	cfg := srv.library.Get()
	cfg.MediaRoot = "/no/such/path/xirc-test"
	body, _ := json.Marshal(cfg)
	req := httptest.NewRequest("PUT", "/api/library", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleLibraryDetect(t *testing.T) {
	srv, _, cleanup := newTestServerWithLibrary(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/library/detect", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cfg library.Config
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Categories) == 0 {
		t.Error("expected detected categories")
	}
}

func TestHandleLibraryPreview(t *testing.T) {
	srv, _, cleanup := newTestServerWithLibrary(t)
	defer cleanup()

	body := `{"filenames": ["Oppenheimer.2023.WEB-GRP.mkv", "unrelated.txt"]}`
	req := httptest.NewRequest("POST", "/api/library/preview", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []library.PreviewResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Kind != "movie" {
		t.Errorf("results[0].Kind = %q, want movie", results[0].Kind)
	}
	if results[1].Kind != "" {
		t.Errorf("results[1].Kind = %q, want \"\" (no match)", results[1].Kind)
	}
}

func TestHandleLibraryKinds(t *testing.T) {
	srv, _, cleanup := newTestServerWithLibrary(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/library/kinds", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var kinds map[string][]string
	if err := json.NewDecoder(w.Body).Decode(&kinds); err != nil {
		t.Fatal(err)
	}
	if len(kinds["series"]) == 0 {
		t.Error("expected fields for kind 'series'")
	}
}

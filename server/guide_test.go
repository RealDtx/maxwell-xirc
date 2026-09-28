package server

import (
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
)

func TestGuideServed(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/guide.md", nil))
	if w.Code != 404 {
		t.Errorf("unset guide: %d", w.Code)
	}
	srv.SetGuide([]byte("# hi"))
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/guide.md", nil))
	if w.Code != 200 || w.Body.String() != "# hi" || w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Errorf("guide: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
}

// Every help block in the UI must point at an existing guide section.
func TestGuideAnchorsExist(t *testing.T) {
	guide, err := os.ReadFile("../docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile("../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^## .*\{#([a-z0-9-]+)\}\s*$`).FindAllSubmatch(guide, -1) {
		have[string(m[1])] = true
	}
	if len(have) == 0 {
		t.Fatal("no {#anchor} sections found in docs/guide.md")
	}
	for _, m := range regexp.MustCompile(`data-help="([a-z0-9-]+)"`).FindAllSubmatch(html, -1) {
		if !have[string(m[1])] {
			t.Errorf("web/index.html uses help anchor %q, missing in docs/guide.md", m[1])
		}
	}
}

package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

func TestIndexExportImportRoundTrip(t *testing.T) {
	src, srcStore, cleanup := newTestServerWithStore(t)
	defer cleanup()
	sv := &db.Server{Name: "Example", Host: "irc.example.net", Port: 6667, Nickname: "me", Enabled: true}
	srcStore.CreateServer(sv)
	pack, size := 7, "700M"
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	srcStore.BulkMergeIndexedFiles([]db.IndexedFile{{ServerID: sv.ID, Channel: "#example-downloads", BotNick: "ExampleBot",
		PackNumber: &pack, Filename: "Example.File.mkv", Filesize: &size, RawLine: "ad", FirstSeenAt: first, LastSeenAt: last}})

	w := httptest.NewRecorder()
	src.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/index/export", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/gzip" {
		t.Fatalf("export %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	gz, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(gz)
	if !strings.Contains(string(plain), "xirc://irc.example.net/%23example-downloads/ExampleBot/7?") {
		t.Fatalf("export body = %q", plain)
	}

	// Import into a fresh instance with a different server ID for the same host.
	dst, dstStore, cleanup2 := newTestServerWithStore(t)
	defer cleanup2()
	dstStore.CreateServer(&db.Server{Name: "Other", Host: "irc.unrelated.org", Port: 6667, Nickname: "me", Enabled: true})
	dsv := &db.Server{Name: "Mine", Host: "IRC.example.net", Port: 6697, Nickname: "me", Enabled: true}
	dstStore.CreateServer(dsv)

	body := append([]byte{}, w.Body.Bytes()...)
	w = httptest.NewRecorder()
	dst.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/index/import", bytes.NewReader(body)))
	var res map[string]int
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != http.StatusOK || res["added"] != 1 {
		t.Fatalf("import %d %s", w.Code, w.Body)
	}
	files, _ := dstStore.SearchIndexedFiles("example", dsv.ID, "", 10)
	if len(files) != 1 || !files[0].FirstSeenAt.Equal(first) || !files[0].LastSeenAt.Equal(last) || *files[0].Filesize != "700M" {
		t.Fatalf("imported = %+v", files)
	}

	// Plain text works too; unknown networks and junk are counted.
	text := "xirc://irc.nowhere.net/%23c/Bot/1?name=X.mkv\nxirc://irc.example.net/%23c/Bot/abc\nxirc://irc.example.net/%23c/Bot/2\n"
	w = httptest.NewRecorder()
	dst.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/index/import", strings.NewReader(text)))
	res = map[string]int{}
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["skipped_unknown_network"] != 1 || res["invalid"] != 2 || res["added"] != 0 {
		t.Fatalf("plain import = %+v", res) // invalid: bad pack + link without name
	}
}

func TestIndexImportRejectsBrokenGzip(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/index/import", bytes.NewReader([]byte{0x1f, 0x8b, 0, 1, 2, 3})))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("broken gzip: status %d, want 400", w.Code)
	}
}

func TestIndexImportRejectsFilesWithoutLinks(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/index/import", strings.NewReader("just some\nnotes\n")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no links: status %d, want 400", w.Code)
	}
}

func TestIndexImportRejectsOversizedDecompression(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()
	store.CreateServer(&db.Server{Name: "Example", Host: "irc.example.net", Port: 6667, Nickname: "me", Enabled: true})
	old := maxIndexImportDecoded
	maxIndexImportDecoded = 1 << 10
	defer func() { maxIndexImportDecoded = old }()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(gz, "xirc://irc.example.net/%%23c/ExampleBot/%d?name=F%d.mkv\n", i, i)
	}
	gz.Close()
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/index/import", &buf))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("over decoded limit: status %d, want 400 (%s)", w.Code, w.Body)
	}
}

// The UI uploads through the auth middleware, which only lets JSON-typed
// writes through (CSRF); the handler must still sniff the gzip body.
func TestIndexImportThroughAuthWithJSONContentType(t *testing.T) {
	srv, store := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.1.0/24"}})
	store.CreateServer(&db.Server{Name: "Example", Host: "irc.example.net", Port: 6667, Nickname: "me", Enabled: true})
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("xirc://irc.example.net/%23c/ExampleBot/1?name=A.mkv\n"))
	gz.Close()
	w := do(srv, "POST", "/api/index/import", "192.168.1.5:4000", map[string]string{"Content-Type": "application/json"}, buf.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RealDtx/maxwell-irc/library"
)

func newTextTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	srv, _, cleanup := newTestServerWithStore(t)
	t.Cleanup(cleanup)
	root := t.TempDir()
	srv.SetLibrary(library.NewManager(filepath.Join(root, "categories.yaml"), library.Config{MediaRoot: root}))
	return srv, root
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func getText(t *testing.T, srv *Server, path, extra string) (*httptest.ResponseRecorder, textFileResponse) {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/files/text?path="+url.QueryEscape(path)+extra, nil)
	w := httptest.NewRecorder()
	srv.handleFilesText(w, r)
	var resp textFileResponse
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
	}
	return w, resp
}

func TestFilesText_Sniff(t *testing.T) {
	srv, root := newTextTestServer(t)
	writeFile(t, filepath.Join(root, "nul.txt"), []byte("ab\x00cd"))
	writeFile(t, filepath.Join(root, "img.txt"), []byte("\x89PNG\r\n\x1a\nrest-of-file"))
	writeFile(t, filepath.Join(root, "a.zip"), []byte("PK\x03\x04rest-of-zip"))
	writeFile(t, filepath.Join(root, "doc.txt"), []byte("%PDF-1.7\nrest"))
	// CP437 art: block chars plus a control-range glyph (0x10) that
	// http.DetectContentType alone would call binary.
	writeFile(t, filepath.Join(root, "art.nfo"), []byte("\xDB\xDB\xB0 \x10 hello\r\n"))

	for _, name := range []string{"nul.txt", "img.txt", "a.zip", "doc.txt"} {
		if w, _ := getText(t, srv, filepath.Join(root, name), ""); w.Code != 415 {
			t.Errorf("%s: got %d want 415", name, w.Code)
		}
	}
	w, resp := getText(t, srv, filepath.Join(root, "art.nfo"), "")
	if w.Code != 200 {
		t.Fatalf("art.nfo: %d %s", w.Code, w.Body.String())
	}
	if resp.Encoding != "cp437" || !strings.HasPrefix(resp.Text, "██░") || resp.EOL != "crlf" ||
		!strings.HasSuffix(resp.Text, "hello\n") || !resp.Editable || resp.NextOffset != -1 {
		t.Errorf("art.nfo: %+v", resp)
	}
}

func TestFilesText_EncodingParam(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "w.srt")
	writeFile(t, p, []byte("caf\xE9\n"))

	if _, resp := getText(t, srv, p, ""); resp.Encoding != "windows-1252" || resp.Text != "café\n" {
		t.Errorf("default: %+v", resp)
	}
	if _, resp := getText(t, srv, p, "&encoding=cp437"); resp.Encoding != "cp437" || resp.Text != "cafΘ\n" {
		t.Errorf("cp437: %+v", resp)
	}
	if w, _ := getText(t, srv, p, "&encoding=utf-8"); w.Code != 422 {
		t.Errorf("forced utf-8 on legacy bytes: %d", w.Code)
	}
	if w, _ := getText(t, srv, p, "&encoding=latin9"); w.Code != 400 {
		t.Errorf("unknown encoding: %d", w.Code)
	}
	u := filepath.Join(root, "u.txt")
	writeFile(t, u, []byte("\xEF\xBB\xBFgrüße\n"))
	if _, resp := getText(t, srv, u, ""); resp.Encoding != "utf-8" || !resp.BOM || resp.Text != "grüße\n" || resp.EOL != "lf" {
		t.Errorf("utf-8 bom: %+v", resp)
	}
}

func TestFilesText_Large(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "big.log")
	// "a" + "ä"×N: every ä starts at an odd offset, so the even window size
	// ends mid-rune at offset textWindow-1.
	data := append([]byte("a"), bytes.Repeat([]byte("ä"), textEditMax/2)...)
	writeFile(t, p, data)
	size := int64(len(data)) // textEditMax + 1

	_, r0 := getText(t, srv, p, "")
	if r0.Editable || r0.Encoding != "utf-8" || r0.Offset != 0 || r0.NextOffset != textWindow-1 ||
		!utf8.ValidString(r0.Text) || len(r0.Text) != textWindow-1 || r0.Size != size {
		t.Errorf("window 0: editable=%v enc=%s off=%d next=%d len=%d", r0.Editable, r0.Encoding, r0.Offset, r0.NextOffset, len(r0.Text))
	}
	// Starting on a continuation byte skips to the next rune start.
	if _, r := getText(t, srv, p, "&offset=2"); r.Offset != 3 || !utf8.ValidString(r.Text) {
		t.Errorf("offset 2: off=%d", r.Offset)
	}
	_, rEnd := getText(t, srv, p, "&offset=-1")
	if rEnd.Offset != size-textWindow || rEnd.NextOffset != -1 || !strings.HasSuffix(rEnd.Text, "ä") {
		t.Errorf("end: off=%d next=%d", rEnd.Offset, rEnd.NextOffset)
	}
	for _, bad := range []string{"&offset=abc", "&offset=-5"} {
		if w, _ := getText(t, srv, p, bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
}

func TestFilesText_PathChecks(t *testing.T) {
	srv, root := newTextTestServer(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(root, ".hidden.txt"), []byte("x"))
	writeFile(t, filepath.Join(outside, "secret.txt"), []byte("s"))
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt"))
	for name, p := range map[string]string{
		"hidden":  filepath.Join(root, ".hidden.txt"),
		"outside": filepath.Join(outside, "secret.txt"),
		"symlink": filepath.Join(root, "link.txt"),
	} {
		if w, _ := getText(t, srv, p, ""); w.Code != 403 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w, _ := getText(t, srv, filepath.Join(root, "missing.txt"), ""); w.Code != 404 {
		t.Errorf("missing: %d", w.Code)
	}
}

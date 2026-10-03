package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RealDtx/maxwell-irc/config"
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
func putText(t *testing.T, srv *Server, req textSaveRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(req)
	r := httptest.NewRequest("PUT", "/api/files/text", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleFilesText(w, r)
	return w
}

// saveReq builds the request the UI sends after loading resp, with new text.
func saveReq(path string, resp textFileResponse, text string) textSaveRequest {
	return textSaveRequest{Path: path, Text: text, Encoding: resp.Encoding, BOM: resp.BOM, EOL: resp.EOL,
		ExpectedMtime: resp.Mtime, ExpectedSize: resp.Size}
}

func TestFilesText_RoundTripByteIdentical(t *testing.T) {
	srv, root := newTextTestServer(t)
	cases := map[string]struct {
		data []byte
		enc  string
	}{
		"u.srt":    {[]byte("1\nhéllo wörld\n"), "utf-8"},
		"bom.txt":  {[]byte("\xEF\xBB\xBFx\r\ny\r\n"), "utf-8"},
		"art.nfo":  {[]byte("\xDB\xDB NFO \x10\r\nline2\r\n"), "cp437"},
		"w.srt":    {[]byte("caf\xE9 \x80\n"), "windows-1252"},
		"noeol.md": {[]byte("no trailing newline"), "utf-8"},
	}
	for name, c := range cases {
		p := filepath.Join(root, name)
		writeFile(t, p, c.data)
		_, resp := getText(t, srv, p, "")
		if resp.Encoding != c.enc {
			t.Errorf("%s: encoding %s want %s", name, resp.Encoding, c.enc)
		}
		if w := putText(t, srv, saveReq(p, resp, resp.Text)); w.Code != 200 {
			t.Fatalf("%s: save %d %s", name, w.Code, w.Body.String())
		}
		if got, _ := os.ReadFile(p); !bytes.Equal(got, c.data) {
			t.Errorf("%s: not byte-identical\n got %q\nwant %q", name, got, c.data)
		}
	}
}

func TestFilesText_SaveEdits(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "art.nfo")
	writeFile(t, p, []byte("\xDB old\r\n"))
	_, resp := getText(t, srv, p, "")
	w := putText(t, srv, saveReq(p, resp, "█ new\n▓\n"))
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(p); string(got) != "\xDB new\r\n\xB2\r\n" {
		t.Errorf("saved bytes %q", got)
	}
	var out struct {
		Mtime time.Time `json:"mtime"`
		Size  int64     `json:"size"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Size != 10 || out.Mtime.IsZero() {
		t.Errorf("response: %+v", out)
	}
}

func TestFilesText_Unrepresentable(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "art.nfo")
	orig := []byte("\xDB\r\n")
	writeFile(t, p, orig)
	_, resp := getText(t, srv, p, "")
	w := putText(t, srv, saveReq(p, resp, "line1\nab😀"))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "line 2:3") || !strings.Contains(w.Body.String(), "CP437") {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, orig) {
		t.Errorf("file changed: %q", got)
	}
}

func TestFilesText_PutValidation(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, []byte("x\n"))
	_, resp := getText(t, srv, p, "")
	for name, mod := range map[string]func(*textSaveRequest){
		"bad eol":      func(r *textSaveRequest) { r.EOL = "cr" },
		"bad encoding": func(r *textSaveRequest) { r.Encoding = "latin9" },
	} {
		req := saveReq(p, resp, "y\n")
		mod(&req)
		if w := putText(t, srv, req); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// Encoded result over the limit.
	if w := putText(t, srv, saveReq(p, resp, strings.Repeat("x", textEditMax+1))); w.Code != 413 {
		t.Errorf("too large result: %d", w.Code)
	}
	// File itself over the limit.
	big := filepath.Join(root, "big.txt")
	writeFile(t, big, bytes.Repeat([]byte("x"), textEditMax+1))
	_, bresp := getText(t, srv, big, "")
	if w := putText(t, srv, saveReq(big, bresp, "x")); w.Code != 413 {
		t.Errorf("large file save: %d", w.Code)
	}
}

func TestFilesText_Conflict(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "a.txt")
	writeFile(t, p, []byte("v1\n"))
	_, resp := getText(t, srv, p, "")
	writeFile(t, p, []byte("v2 from elsewhere\n"))
	later := time.Now().Add(time.Minute)
	os.Chtimes(p, later, later)

	req := saveReq(p, resp, "mine\n")
	if w := putText(t, srv, req); w.Code != 409 {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(p); string(got) != "v2 from elsewhere\n" {
		t.Errorf("409 still wrote: %q", got)
	}
	req.Force = true
	if w := putText(t, srv, req); w.Code != 200 {
		t.Fatalf("force: %d", w.Code)
	}
	if got, _ := os.ReadFile(p); string(got) != "mine\n" {
		t.Errorf("force: %q", got)
	}
}

func TestFilesText_AtomicKeepsMode(t *testing.T) {
	srv, root := newTextTestServer(t)
	p := filepath.Join(root, "a.nfo")
	writeFile(t, p, []byte("\xDBx\n"))
	os.Chmod(p, 0o640)
	_, resp := getText(t, srv, p, "")
	putText(t, srv, saveReq(p, resp, "😀")) // 422: must leave no temp file
	if w := putText(t, srv, saveReq(p, resp, "ok\n")); w.Code != 200 {
		t.Fatalf("save: %d", w.Code)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if m, _ := filepath.Glob(filepath.Join(root, ".xirc-edit-*")); len(m) != 0 {
		t.Errorf("temp files left: %v", m)
	}
}

func TestFilesText_SymlinkSaveKeepsLink(t *testing.T) {
	srv, root := newTextTestServer(t)
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	target := filepath.Join(root, "sub", "real.txt")
	link := filepath.Join(root, "link.txt")
	writeFile(t, target, []byte("old\n"))
	os.Symlink(target, link)
	_, resp := getText(t, srv, link, "")
	if w := putText(t, srv, saveReq(link, resp, "new\n")); w.Code != 200 {
		t.Fatalf("save via link: %d %s", w.Code, w.Body.String())
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link replaced by a regular file")
	}
	if got, _ := os.ReadFile(target); string(got) != "new\n" {
		t.Errorf("target: %q", got)
	}
}

func TestFilesText_PutPathChecksAndRoles(t *testing.T) {
	srv, root := newTextTestServer(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), []byte("s"))
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt"))
	writeFile(t, filepath.Join(root, ".hidden.txt"), []byte("x"))
	for _, p := range []string{filepath.Join(root, "link.txt"), filepath.Join(outside, "secret.txt"), filepath.Join(root, ".hidden.txt")} {
		req := textSaveRequest{Path: p, Text: "pwned", Encoding: "utf-8", EOL: "lf", Force: true}
		if w := putText(t, srv, req); w.Code != 403 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(got) != "s" {
		t.Errorf("outside file written")
	}

	// Through the full handler stack: users may view, only admins may save.
	usrv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"})
	uroot := t.TempDir()
	usrv.SetLibrary(library.NewManager(filepath.Join(uroot, "categories.yaml"), library.Config{MediaRoot: uroot}))
	up := filepath.Join(uroot, "a.txt")
	writeFile(t, up, []byte("x\n"))
	if w := do(usrv, "GET", "/api/files/text?path="+url.QueryEscape(up), "192.168.1.5:1", nil, ""); w.Code != 200 {
		t.Errorf("user GET: %d %s", w.Code, w.Body.String())
	}
	body := fmt.Sprintf(`{"path":%q,"text":"y","encoding":"utf-8","eol":"lf","force":true}`, up)
	if w := do(usrv, "PUT", "/api/files/text", "192.168.1.5:1", map[string]string{"Content-Type": "application/json"}, body); w.Code != 403 {
		t.Errorf("user PUT: %d", w.Code)
	}
}

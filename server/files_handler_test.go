package server

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/library"
)

func TestFileManager(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	root, err := ioutil.TempDir("", "xirc-files-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	outside, err := ioutil.TempDir("", "xirc-files-outside-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)

	for _, d := range []string{"Series/Show/S01", "Series/Old", ".hidden", "Downloads"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"ep1.mkv", "ep2.mkv", "dup.mkv", "Series/Show/S01/dup.mkv", "Series/Old/x.mkv", "Downloads/keep.pdf"} {
		if err := ioutil.WriteFile(filepath.Join(root, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// Two roots, one nested in the other (like /data/media and /data/media/Downloads).
	srv.SetLibrary(library.NewManager(filepath.Join(root, "categories.yaml"), library.Config{MediaRoot: root}))
	srv.SetDownloadsDir(filepath.Join(root, "Downloads"))

	list := func(dir string) (int, filesResponse) {
		w := httptest.NewRecorder()
		srv.handleFiles(w, httptest.NewRequest("GET", "/api/files?dir="+dir, nil))
		var resp filesResponse
		json.NewDecoder(w.Body).Decode(&resp)
		return w.Code, resp
	}
	action := func(body map[string]interface{}) (int, map[string]interface{}) {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		srv.handleFiles(w, httptest.NewRequest("POST", "/api/files", strings.NewReader(string(b))))
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		return w.Code, resp
	}
	exists := func(p string) bool { _, err := os.Lstat(filepath.Join(root, p)); return err == nil }

	// Listing: folders first, hidden skipped, item counts, parent/root.
	code, l := list(filepath.Join(root, "Series"))
	if code != 200 || l.Parent != root || l.Root != root || len(l.Entries) != 2 || !l.Entries[0].IsDir || l.Entries[0].Name != "Old" || l.Entries[0].Items != 1 {
		t.Errorf("list Series = %d %+v", code, l)
	}
	code, l = list(filepath.Join(root, "Downloads"))
	if code != 200 || l.Root != root {
		t.Errorf("nested root should report outermost root: %d %+v", code, l)
	}
	if code, _ := list(outside); code != http.StatusForbidden {
		t.Errorf("list outside: %d", code)
	}

	season := filepath.Join(root, "Series", "Show", "S01")
	series := filepath.Join(root, "Series")

	// Bulk move with per-item errors.
	code, resp := action(map[string]interface{}{"action": "move", "src_dir": root, "names": []string{"ep1.mkv", "dup.mkv", "../x", "nope"}, "dest_dir": season})
	errs, _ := resp["errors"].(map[string]interface{})
	if code != 200 || len(errs) != 3 || errs["dup.mkv"] == nil || !exists("Series/Show/S01/ep1.mkv") || !exists("dup.mkv") {
		t.Errorf("bulk move = %d %v", code, resp)
	}
	// Folder moves: into itself refused, normal move works, roots untouchable.
	if _, resp := action(map[string]interface{}{"action": "move", "src_dir": root, "names": []string{"Series"}, "dest_dir": season}); len(resp["errors"].(map[string]interface{})) != 1 {
		t.Errorf("moving a folder into itself must fail: %v", resp)
	}
	if _, resp := action(map[string]interface{}{"action": "move", "src_dir": series, "names": []string{"Old"}, "dest_dir": filepath.Join(series, "Show")}); len(resp["done"].([]interface{})) != 1 || !exists("Series/Show/Old/x.mkv") {
		t.Errorf("folder move: %v", resp)
	}
	if _, resp := action(map[string]interface{}{"action": "delete", "dir": root, "names": []string{"Downloads"}}); len(resp["errors"].(map[string]interface{})) != 1 || !exists("Downloads/keep.pdf") {
		t.Errorf("deleting a nested root must fail: %v", resp)
	}
	if code, _ := action(map[string]interface{}{"action": "move", "src_dir": root, "names": []string{"ep2.mkv"}, "dest_dir": outside}); code != http.StatusForbidden {
		t.Errorf("move outside: %d", code)
	}

	// Rename / mkdir.
	if code, _ := action(map[string]interface{}{"action": "rename", "dir": root, "name": "ep2.mkv", "new_name": "Episode 2.mkv"}); code != 200 || !exists("Episode 2.mkv") {
		t.Errorf("rename: %d", code)
	}
	for _, bad := range []string{"../evil", ".hidden2", "a/b", ""} {
		if code, _ := action(map[string]interface{}{"action": "rename", "dir": root, "name": "Episode 2.mkv", "new_name": bad}); code != http.StatusBadRequest {
			t.Errorf("rename to %q: %d", bad, code)
		}
	}
	if code, _ := action(map[string]interface{}{"action": "rename", "dir": root, "name": "Episode 2.mkv", "new_name": "dup.mkv"}); code != http.StatusConflict {
		t.Errorf("rename onto existing: %d", code)
	}
	if code, _ := action(map[string]interface{}{"action": "mkdir", "dir": root, "name": "Music"}); code != 200 || !exists("Music") {
		t.Errorf("mkdir: %d", code)
	}
	if code, _ := action(map[string]interface{}{"action": "mkdir", "dir": root, "name": "Music"}); code != http.StatusConflict {
		t.Errorf("mkdir existing: %d", code)
	}

	// Delete: recursive folder, symlink removes only the link.
	code, resp = action(map[string]interface{}{"action": "delete", "dir": series, "names": []string{"Show"}})
	if code != 200 || exists("Series/Show") {
		t.Errorf("delete folder: %d %v", code, resp)
	}
	action(map[string]interface{}{"action": "delete", "dir": root, "names": []string{"link"}})
	if exists("link") {
		t.Error("symlink not deleted")
	}
	if _, err := os.Stat(filepath.Join(outside, "secret")); err != nil {
		t.Errorf("symlink target must survive: %v", err)
	}
}

// TestFilesRoots checks GET /api/files with no dir returns the sorted,
// deduplicated configured roots.
func TestFilesRoots(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	mediaRoot, err := ioutil.TempDir("", "xirc-files-roots-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mediaRoot)

	movies := filepath.Join(mediaRoot, "Movies")
	downloads := filepath.Join(mediaRoot, "Downloads")
	for _, d := range []string{movies, downloads} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	srv.SetLibrary(library.NewManager(filepath.Join(mediaRoot, "categories.yaml"), library.Config{
		MediaRoot: mediaRoot,
		Categories: []library.Category{
			{ID: "movie", Kind: "movie", Dir: "Movies", Enabled: true},
			{ID: "disabled", Kind: "movie", Dir: "Off", Enabled: false},
		},
	}))
	srv.SetDownloadsDir(downloads)

	w := httptest.NewRecorder()
	srv.handleFiles(w, httptest.NewRequest("GET", "/api/files", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Roots []string `json:"roots"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := []string{mediaRoot, movies, downloads}
	sort.Strings(want)
	if len(resp.Roots) != len(want) {
		t.Fatalf("roots = %v, want %v", resp.Roots, want)
	}
	for i := range want {
		if resp.Roots[i] != want[i] {
			t.Errorf("roots[%d] = %q, want %q", i, resp.Roots[i], want[i])
		}
	}
}

func TestFileManager_ReadOnlyRootReturns403WithReason(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.mkv"), []byte("x"), 0644)
	if err := os.Chmod(root, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(root, 0755); err != nil {
			t.Fatal(err)
		}
	}()
	srv.SetDownloadsDir(root)

	body := `{"action":"rename","dir":"` + root + `","name":"a.mkv","new_name":"b.mkv"}`
	req := httptest.NewRequest("POST", "/api/files", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "permission denied: "+root) {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
}

// A rename/move blocked by a read-only destination is not the same fault as
// one blocked by a read-only source: the reason must name whichever side
// actually lacks write permission.
func TestFileManager_MoveBlamesReadOnlySource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	destDir := filepath.Join(root, "dest")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "a.mkv"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(srcDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(srcDir, 0755); err != nil {
			t.Fatal(err)
		}
	}()
	srv.SetDownloadsDir(root)

	body := `{"action":"move","src_dir":"` + srcDir + `","dest_dir":"` + destDir + `","names":["a.mkv"]}`
	req := httptest.NewRequest("POST", "/api/files", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errs, _ := resp["errors"].(map[string]interface{})
	msg, _ := errs["a.mkv"].(string)
	if w.Code != http.StatusOK || !strings.Contains(msg, "permission denied: "+srcDir) {
		t.Errorf("got %d %v", w.Code, resp)
	}
}

// TestAdvertisedDirsExist checks that a configured category folder that
// doesn't exist yet is neither offered as a download target nor listed as a
// file-manager root (isValidTargetDir would reject it), while an existing
// one and the media root itself are.
func TestAdvertisedDirsExist(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	mediaRoot := t.TempDir()
	movies := filepath.Join(mediaRoot, "Movies")
	missing := filepath.Join(mediaRoot, "Series")
	if err := os.MkdirAll(movies, 0755); err != nil {
		t.Fatal(err)
	}
	srv.SetLibrary(library.NewManager(filepath.Join(mediaRoot, "categories.yaml"), library.Config{
		MediaRoot: mediaRoot,
		Categories: []library.Category{
			{ID: "movie", Kind: "movie", Dir: "Movies", Enabled: true},
			{ID: "series", Kind: "series", Dir: "Series", Enabled: true},
		},
	}))

	w := httptest.NewRecorder()
	srv.handleGetDownloadTargets(w, httptest.NewRequest("GET", "/api/downloads/targets", nil))
	var targets []string
	if err := json.NewDecoder(w.Body).Decode(&targets); err != nil {
		t.Fatalf("decode targets: %v", err)
	}

	w = httptest.NewRecorder()
	srv.handleFiles(w, httptest.NewRequest("GET", "/api/files", nil))
	var files struct {
		Roots []string `json:"roots"`
	}
	if err := json.NewDecoder(w.Body).Decode(&files); err != nil {
		t.Fatalf("decode roots: %v", err)
	}

	for name, list := range map[string][]string{"targets": targets, "roots": files.Roots} {
		has := map[string]bool{}
		for _, d := range list {
			has[d] = true
		}
		if has[missing] {
			t.Errorf("%s advertises missing category dir %s: %v", name, missing, list)
		}
		if !has[movies] || !has[mediaRoot] {
			t.Errorf("%s lacks existing dirs %s / %s: %v", name, mediaRoot, movies, list)
		}
	}
}

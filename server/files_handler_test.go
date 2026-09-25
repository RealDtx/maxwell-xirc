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

	"github.com/RealDtx/maxwell-irc/db"
)

func TestFileManager(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
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
	for _, d := range []string{root, filepath.Join(root, "Downloads")} {
		if err := store.CreateFileRoutingRule(&db.FileRoutingRule{Pattern: "*", DestinationDir: d, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

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

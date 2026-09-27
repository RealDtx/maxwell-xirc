package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStorageDirsRace exercises setStorageDirs concurrently with
// downloadsDirNow under -race, to catch any read of the downloads root that
// isn't lock-protected.
func TestStorageDirsRace(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			srv.setStorageDirs("/downloads", "/tmp")
		}
	}()
	for i := 0; i < 200; i++ {
		srv.downloadsDirNow()
	}
	<-done
}

func TestCapabilities_ReadOnlyDownloadsDisablesRequests(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	dl := t.TempDir()
	if err := os.Chmod(dl, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(dl, 0755); err != nil {
			t.Fatal(err)
		}
	}()
	srv.SetDownloadsDir(dl)
	srv.SetCapabilityInputs(filepath.Join(dl, ".tmp"), t.TempDir(), nil)
	caps := srv.RecheckCapabilities()
	if caps.Downloads.OK || !strings.Contains(caps.Downloads.Reason, "not writable") {
		t.Fatalf("downloads capability: %+v", caps.Downloads)
	}

	req := httptest.NewRequest("POST", "/api/downloads/request", strings.NewReader(`{"server_id":1,"channel":"#x","bot_nick":"b","pack_number":1}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "not writable") {
		t.Errorf("request while disabled: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/capabilities", nil))
	var got Capabilities
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode capabilities response: %v", err)
	}
	if got.Downloads.OK || len(got.Roots) == 0 {
		t.Errorf("GET capabilities: %+v", got)
	}
}

func TestCapabilities_MissingTempDirIsFineIfParentWritable(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()
	dl := t.TempDir()
	srv.SetDownloadsDir(dl)
	srv.SetCapabilityInputs(filepath.Join(dl, ".tmp"), t.TempDir(), nil) // .tmp not created yet: engine MkdirAlls it
	if caps := srv.RecheckCapabilities(); !caps.Downloads.OK {
		t.Errorf("downloads should be OK: %+v", caps.Downloads)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"errors"
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

type applyMappingsMockStore struct {
	db.Store
	rules     []db.FileRoutingRule
	updateErr error
}

func (m *applyMappingsMockStore) GetAllFileRoutingRules() ([]db.FileRoutingRule, error) {
	return append([]db.FileRoutingRule(nil), m.rules...), nil
}

func (m *applyMappingsMockStore) UpdateFileRoutingRule(r *db.FileRoutingRule) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return nil
}

func newTestServerWithSetup(t *testing.T, setup *SetupState) (*Server, db.Store, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "xirc-setup-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("store: %v", err)
	}
	store.Migrate()
	cleanup := func() { store.Close(); os.RemoveAll(dir) }
	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	p := parser.New(store, bus)
	srv := New(store, ircMgr, p, nil, nil, nil, nil, setup, "", nil)
	return srv, store, cleanup
}

func TestSetupStatus_NotRequired(t *testing.T) {
	setup := &SetupState{Required: false}
	srv, _, cleanup := newTestServerWithSetup(t, setup)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/setup/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["required"] != false {
		t.Errorf("expected required=false, got %v", resp["required"])
	}
}

func TestSetupStatus_Required(t *testing.T) {
	setup := &SetupState{Required: true, BadDirs: []string{"/srv/missing"}}
	srv, _, cleanup := newTestServerWithSetup(t, setup)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/setup/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["required"] != true {
		t.Errorf("expected required=true, got %v", resp["required"])
	}
}

func TestSetupDefaults(t *testing.T) {
	setup := &SetupState{Required: true, HomeDir: "/home/testuser"}
	srv, _, cleanup := newTestServerWithSetup(t, setup)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/setup/defaults", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["videos_dir"] != "/home/testuser/Videos" {
		t.Errorf("unexpected videos_dir: %q", resp["videos_dir"])
	}
	if resp["downloads_dir"] != "/home/testuser/Downloads" {
		t.Errorf("unexpected downloads_dir: %q", resp["downloads_dir"])
	}
}

func TestSetupComplete_UpdatesRoutingRules(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-setup-complete-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Create a config.yaml with old dirs
	cfgPath := filepath.Join(dir, "config.yaml")
	ioutil.WriteFile(cfgPath, []byte("storage:\n  media_dir: /old/media\n  downloads_dir: /old/dl\n"), 0644)

	// Create writable new dirs
	newMedia := filepath.Join(dir, "media")
	newDl := filepath.Join(dir, "dl")
	os.Mkdir(newMedia, 0755)
	os.Mkdir(newDl, 0755)

	setup := &SetupState{
		Required:     true,
		BadDirs:      []string{"/old/media", "/old/dl"},
		MediaDir:     "/old/media",
		DownloadsDir: "/old/dl",
		ConfigPath:   cfgPath,
	}

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Migrate()
	store.CreateFileRoutingRule(&db.FileRoutingRule{Pattern: "*.mkv", DestinationDir: "/old/media", Enabled: true})
	store.CreateFileRoutingRule(&db.FileRoutingRule{Pattern: "*", DestinationDir: "/old/dl", Enabled: true})

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	p := parser.New(store, bus)
	srv := New(store, ircMgr, p, nil, nil, nil, nil, setup, "", nil)

	body, _ := json.Marshal(map[string]interface{}{
		"mappings": []map[string]string{
			{"old_dir": "/old/media", "new_dir": newMedia},
			{"old_dir": "/old/dl", "new_dir": newDl},
		},
	})
	req := httptest.NewRequest("POST", "/api/setup/complete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// SetupState should be cleared
	if setup.Required {
		t.Error("expected SetupState.Required to be false after complete")
	}

	// Routing rules should point to new dirs
	rules, _ := store.GetAllFileRoutingRules()
	for _, r := range rules {
		if r.DestinationDir == "/old/media" || r.DestinationDir == "/old/dl" {
			t.Errorf("rule %q still points to old dir %q", r.Pattern, r.DestinationDir)
		}
	}
}

func TestApplyMappings_DBErrorRollsBackConfig(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-apply-mappings-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	cfgFile, err := ioutil.TempFile(dir, "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := cfgFile.Name()
	original := "storage:\n  media_dir: /old/media\n  downloads_dir: /old/dl\n"
	if _, err := cfgFile.Write([]byte(original)); err != nil {
		cfgFile.Close()
		t.Fatal(err)
	}
	if err := cfgFile.Close(); err != nil {
		t.Fatal(err)
	}

	state := &SetupState{
		Required:     true,
		MediaDir:     "/old/media",
		DownloadsDir: "/old/dl",
		ConfigPath:   cfgPath,
	}

	store := &applyMappingsMockStore{
		rules: []db.FileRoutingRule{
			{ID: 1, Pattern: "*.mkv", DestinationDir: "/old/media", Enabled: true},
		},
		updateErr: errors.New("update failed"),
	}

	err = ApplyMappings([]Mapping{
		{OldDir: "/old/media", NewDir: "/new/media"},
		{OldDir: "/old/dl", NewDir: "/new/dl"},
	}, store, state)
	if err == nil {
		t.Fatal("expected ApplyMappings to return an error")
	}

	data, err := ioutil.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "media_dir: /old/media") {
		t.Fatalf("expected rolled back media_dir in config, got:\n%s", got)
	}
	if !strings.Contains(got, "downloads_dir: /old/dl") {
		t.Fatalf("expected rolled back downloads_dir in config, got:\n%s", got)
	}
}

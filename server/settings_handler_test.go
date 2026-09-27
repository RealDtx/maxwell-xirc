package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/maintenance"
	"github.com/RealDtx/maxwell-irc/parser"
	"github.com/RealDtx/maxwell-irc/queue"
)

// newSettingsTestServer builds a fully wired Server (engine, auth, maintenance,
// settings) from a config.yaml written under t.TempDir(). The initial auth
// policy trusts 10.0.0.0/8 as admin, so tests drive admin requests from
// "10.0.0.1:1".
func newSettingsTestServer(t *testing.T) (*Server, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "cfg")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	downloadsDir := filepath.Join(dir, "downloads")
	tempDir := filepath.Join(downloadsDir, ".tmp")
	mediaDir := filepath.Join(dir, "media")
	for _, d := range []string{downloadsDir, tempDir, mediaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	yamlContent := fmt.Sprintf(`
server:
  host: 127.0.0.1
  port: 8085
  prefix: ""
database:
  driver: sqlite
  path: %q
  dsn: "user:secret@tcp(x)/db"
storage:
  media_dir: %q
  downloads_dir: %q
  temp_dir: %q
  min_free_space: 1GB
downloads:
  max_concurrent: 3
maintenance:
  search_result_retention_days: 14
  index_max_files: 200000
  interval_hours: 6
auth:
  trusted_networks: ["10.0.0.0/8"]
  trusted_role: admin
`, filepath.Join(dir, "test.db"), mediaDir, downloadsDir, tempDir)
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o640); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	p := parser.New(store, bus)
	eng := queue.NewEngine(store, bus, ircMgr, &cfg.Storage, cfg.Downloads.MaxConcurrent)
	maint := maintenance.New(store, cfg.Maintenance)

	srv := New(store, ircMgr, p, eng, nil, nil, nil, nil, "", nil)
	srv.SetDownloadsDir(cfg.Storage.DownloadsDir)

	auth, err := NewAuth(cfg.Auth, store, "")
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	srv.SetAuth(auth)
	srv.SetSettings(configPath, cfg, maint)

	return srv, cfg, configPath
}

func adminDo(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	hdr := map[string]string{}
	if body != "" {
		hdr["Content-Type"] = "application/json"
	}
	return do(srv, method, path, "10.0.0.1:1", hdr, body)
}

func TestSettingsGet_Shape(t *testing.T) {
	srv, cfg, _ := newSettingsTestServer(t)

	w := adminDo(t, srv, "GET", "/api/settings", "")
	if w.Code != 200 {
		t.Fatalf("GET /api/settings: got %d body %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("secret")) {
		t.Fatalf("response leaked DSN: %s", w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var settings config.Editable
	if err := json.Unmarshal(resp["settings"], &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings.Downloads.MaxConcurrent != cfg.Downloads.MaxConcurrent {
		t.Errorf("max_concurrent: got %d want %d", settings.Downloads.MaxConcurrent, cfg.Downloads.MaxConcurrent)
	}

	var full struct {
		Readonly struct {
			Database map[string]any `json:"database"`
		} `json:"readonly"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode readonly: %v", err)
	}
	if _, ok := full.Readonly.Database["dsn"]; ok {
		t.Errorf("readonly.database must not include dsn: %v", full.Readonly.Database)
	}

	var persist struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(resp["persist"], &persist); err != nil {
		t.Fatalf("decode persist: %v", err)
	}
	if !persist.OK {
		t.Errorf("persist.ok: got false, want true")
	}
}

func TestSettingsPut_AppliesLiveAndPersists(t *testing.T) {
	srv, _, configPath := newSettingsTestServer(t)

	dir := filepath.Dir(filepath.Dir(configPath)) // t.TempDir() root
	newDownloads := filepath.Join(dir, "downloads")
	newTemp := filepath.Join(newDownloads, ".tmp")

	body := fmt.Sprintf(`{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "2GB"},
		"downloads": {"max_concurrent": 5},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 2},
		"auth": {"trusted_networks": ["192.168.0.0/16"], "trusted_role": "user", "trusted_proxies": []}
	}`, newDownloads, newTemp)

	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code != 200 {
		t.Fatalf("PUT /api/settings: got %d body %s", w.Code, w.Body.String())
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load after PUT: %v", err)
	}
	if loaded.Downloads.MaxConcurrent != 5 {
		t.Errorf("persisted max_concurrent: got %d want 5", loaded.Downloads.MaxConcurrent)
	}
	if loaded.Maintenance.IntervalHours != 2 {
		t.Errorf("persisted interval_hours: got %d want 2", loaded.Maintenance.IntervalHours)
	}
	if loaded.Storage.DownloadsDir != newDownloads {
		t.Errorf("persisted downloads_dir: got %q want %q", loaded.Storage.DownloadsDir, newDownloads)
	}

	if got := srv.downloadsDirNow(); got != newDownloads {
		t.Errorf("downloadsDirNow: got %q want %q", got, newDownloads)
	}

	// The trusted network/role just changed live: 192.168.1.5 is now a
	// "user" principal, which must be forbidden from an admin-only route.
	w2 := do(srv, "GET", "/api/errors", "192.168.1.5:1", nil, "")
	if w2.Code != 403 {
		t.Errorf("GET /api/errors as new user principal: got %d want 403", w2.Code)
	}
}

func TestSettingsPut_ValidationErrors(t *testing.T) {
	srv, _, configPath := newSettingsTestServer(t)

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	body := `{
		"storage": {"downloads_dir": "relative/dir", "temp_dir": "relative/tmp", "min_free_space": "lots"},
		"downloads": {"max_concurrent": 0},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 2},
		"auth": {"trusted_networks": ["nope"], "trusted_role": "root", "trusted_proxies": []}
	}`

	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code != 400 {
		t.Fatalf("PUT /api/settings: got %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	for _, want := range []string{"storage.downloads_dir", "storage.min_free_space", "downloads.max_concurrent", "trusted_role"} {
		if !strings.Contains(resp.Error, want) {
			t.Errorf("error message missing %q: %s", want, resp.Error)
		}
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("config file changed on validation failure:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestSettingsPut_EnvLocked(t *testing.T) {
	t.Setenv("XIRC_DOWNLOADS_MAX_CONCURRENT", "3")
	srv, _, _ := newSettingsTestServer(t)

	body := `{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"},
		"downloads": {"max_concurrent": 7},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 6},
		"auth": {"trusted_networks": ["10.0.0.0/8"], "trusted_role": "admin", "trusted_proxies": []}
	}`
	body = settingsBodyWithCurrentDirs(t, srv, body)

	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code != 400 {
		t.Fatalf("PUT with locked field changed: got %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "XIRC_DOWNLOADS_MAX_CONCURRENT") {
		t.Errorf("error should name the env var: %s", resp.Error)
	}

	body2 := `{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"},
		"downloads": {"max_concurrent": 3},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 9},
		"auth": {"trusted_networks": ["10.0.0.0/8"], "trusted_role": "admin", "trusted_proxies": []}
	}`
	body2 = settingsBodyWithCurrentDirs(t, srv, body2)
	w2 := adminDo(t, srv, "PUT", "/api/settings", body2)
	if w2.Code != 200 {
		t.Fatalf("PUT with locked field unchanged: got %d body %s", w2.Code, w2.Body.String())
	}
}

// settingsBodyWithCurrentDirs substitutes the two %q verbs in a template body
// with the server's current downloads_dir and temp_dir, so the test doesn't
// need to know them.
func settingsBodyWithCurrentDirs(t *testing.T, srv *Server, tmpl string) string {
	t.Helper()
	w := adminDo(t, srv, "GET", "/api/settings", "")
	var resp struct {
		Settings config.Editable `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(tmpl, resp.Settings.Storage.DownloadsDir, resp.Settings.Storage.TempDir)
}

func TestSettingsPut_PersistFailureAppliesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	srv, _, configPath := newSettingsTestServer(t)
	configDir := filepath.Dir(configPath)
	origDownloads := srv.downloadsDirNow()

	if err := os.Chmod(configDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(configDir, 0o755) })

	body := settingsBodyWithCurrentDirs(t, srv, `{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"},
		"downloads": {"max_concurrent": 9},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 6},
		"auth": {"trusted_networks": ["10.0.0.0/8"], "trusted_role": "admin", "trusted_proxies": []}
	}`)

	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code < 400 {
		t.Fatalf("PUT into read-only config dir: got %d body %s", w.Code, w.Body.String())
	}

	if got := srv.downloadsDirNow(); got != origDownloads {
		t.Errorf("downloadsDirNow changed despite persist failure: got %q want %q", got, origDownloads)
	}

	wg := adminDo(t, srv, "GET", "/api/settings", "")
	var resp struct {
		Persist struct {
			OK     bool   `json:"ok"`
			Reason string `json:"reason"`
		} `json:"persist"`
	}
	if err := json.Unmarshal(wg.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Persist.OK {
		t.Errorf("persist.ok: got true, want false")
	}
	if resp.Persist.Reason == "" {
		t.Errorf("persist.reason: got empty, want a reason")
	}
}

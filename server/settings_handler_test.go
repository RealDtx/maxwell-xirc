package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
// "10.0.0.1:1". maxConcurrent seeds storage.downloads.max_concurrent in the
// file (tests that want the env-lock/file-preservation case pass a value
// different from what an env override will apply in memory).
func newSettingsTestServer(t *testing.T, maxConcurrent int) (*Server, *config.Config, string) {
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
  max_concurrent: %d
maintenance:
  search_result_retention_days: 14
  index_max_files: 200000
  interval_hours: 6
auth:
  trusted_networks: ["10.0.0.0/8"]
  trusted_role: admin
`, filepath.Join(dir, "test.db"), mediaDir, downloadsDir, tempDir, maxConcurrent)
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
	// ircMgr is nil here (not the server's) so SetRuntime's background
	// TryDispatchQueued call returns immediately instead of racing the
	// store.Close() in t.Cleanup above (queue.Engine.TryDispatchQueued
	// no-ops when its ircMgr is nil) — this test never exercises dispatch.
	eng := queue.NewEngine(store, bus, nil, &cfg.Storage, cfg.Downloads.MaxConcurrent)
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
	srv, cfg, _ := newSettingsTestServer(t, 3)

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
	srv, _, configPath := newSettingsTestServer(t, 3)

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
	srv, _, configPath := newSettingsTestServer(t, 3)

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

// TestSettingsPut_InvalidCIDR isolates the auth.trusted_networks CIDR check:
// every other field is valid, only the CIDR is bad, so the ParseAuthPolicy
// branch of validateSettings must be what's reported.
func TestSettingsPut_InvalidCIDR(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 3)

	body := settingsBodyWithCurrentDirs(t, srv, `{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"},
		"downloads": {"max_concurrent": 3},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 6},
		"auth": {"trusted_networks": ["nope"], "trusted_role": "admin", "trusted_proxies": []}
	}`)

	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code != 400 {
		t.Fatalf("PUT with bad CIDR: got %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(resp.Error, "trusted_networks") {
		t.Errorf("error should mention trusted_networks: %s", resp.Error)
	}
}

func TestSettingsPut_EnvLocked(t *testing.T) {
	t.Setenv("XIRC_DOWNLOADS_MAX_CONCURRENT", "3")
	srv, _, _ := newSettingsTestServer(t, 3)

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

// TestSettingsPut_EnvLockedFieldNotRewrittenInFile: the file's own
// downloads.max_concurrent is 5, but XIRC_DOWNLOADS_MAX_CONCURRENT=3
// overrides it in memory — so the submitted value (which must equal the
// in-memory 3 to pass the lock check) must NOT overwrite the file's 5.
func TestSettingsPut_EnvLockedFieldNotRewrittenInFile(t *testing.T) {
	t.Setenv("XIRC_DOWNLOADS_MAX_CONCURRENT", "3")
	srv, _, configPath := newSettingsTestServer(t, 5)

	body := settingsBodyWithCurrentDirs(t, srv, `{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"},
		"downloads": {"max_concurrent": 3},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 9},
		"auth": {"trusted_networks": ["10.0.0.0/8"], "trusted_role": "admin", "trusted_proxies": []}
	}`)
	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code != 200 {
		t.Fatalf("PUT with locked field unchanged: got %d body %s", w.Code, w.Body.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("max_concurrent: 5")) {
		t.Errorf("file's own max_concurrent (5) was overwritten by the env-derived value: %s", raw)
	}
	if bytes.Contains(raw, []byte("interval_hours: 9")) == false {
		t.Errorf("unrelated field (interval_hours) was not persisted: %s", raw)
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
	srv, cfg, configPath := newSettingsTestServer(t, 3)
	configDir := filepath.Dir(configPath)
	root := filepath.Dir(configDir)
	origDownloads := srv.downloadsDirNow()
	origMaxConcurrent := cfg.Downloads.MaxConcurrent

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// A different, otherwise-valid (existing, writable) downloads dir — not
	// the server's current one — so a bug that applies live changes before
	// checking the persist result would actually move downloadsDirNow().
	altDownloads := filepath.Join(root, "downloads2")
	if err := os.MkdirAll(altDownloads, 0o755); err != nil {
		t.Fatal(err)
	}
	altTemp := filepath.Join(altDownloads, ".tmp")
	if err := os.MkdirAll(altTemp, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(configDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(configDir, 0o755) })

	body := fmt.Sprintf(`{
		"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"},
		"downloads": {"max_concurrent": 9},
		"maintenance": {"search_result_retention_days": 14, "index_max_files": 200000, "interval_hours": 6},
		"auth": {"trusted_networks": ["10.0.0.0/8"], "trusted_role": "admin", "trusted_proxies": []}
	}`, altDownloads, altTemp)

	w := adminDo(t, srv, "PUT", "/api/settings", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("PUT into read-only config dir: got %d body %s, want 500", w.Code, w.Body.String())
	}

	if got := srv.downloadsDirNow(); got != origDownloads {
		t.Errorf("downloadsDirNow changed despite persist failure: got %q want %q", got, origDownloads)
	}
	if cfg.Downloads.MaxConcurrent != origMaxConcurrent {
		t.Errorf("cfg.Downloads.MaxConcurrent changed despite persist failure: got %d want %d", cfg.Downloads.MaxConcurrent, origMaxConcurrent)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("config file changed despite persist failure:\nbefore: %s\nafter:  %s", before, after)
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

// An unchanged downloads dir that is currently unwritable (unmounted disk,
// env-locked Docker dir) must not block saving unrelated settings.
func TestSettingsPut_UnchangedBrokenDirDoesNotBlockSave(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	srv, cfg, _ := newSettingsTestServer(t, 3)
	dl := cfg.Storage.DownloadsDir
	if err := os.Chmod(dl, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dl, 0o755) })

	w := adminDo(t, srv, "PUT", "/api/settings", `{"maintenance": {"search_result_retention_days": 7, "index_max_files": 1000, "interval_hours": 3}}`)
	if w.Code != 200 {
		t.Fatalf("PUT changing only maintenance: got %d body %s", w.Code, w.Body.String())
	}
	if cfg.Maintenance.IntervalHours != 3 {
		t.Errorf("interval_hours: got %d want 3", cfg.Maintenance.IntervalHours)
	}

	// Changing to another unwritable dir is still rejected.
	body := fmt.Sprintf(`{"storage": {"downloads_dir": %q, "temp_dir": %q, "min_free_space": "1GB"}}`, filepath.Join(dl, "sub"), cfg.Storage.TempDir)
	if w := adminDo(t, srv, "PUT", "/api/settings", body); w.Code != 400 {
		t.Errorf("PUT with new unwritable downloads_dir: got %d want 400", w.Code)
	}
}

// Omitted sections/fields keep their current values; a bad body's decode
// error is reported; a rejected PUT leaves the live slices untouched.
func TestSettingsPut_PartialBodyKeepsCurrent(t *testing.T) {
	srv, cfg, configPath := newSettingsTestServer(t, 3)

	w := adminDo(t, srv, "PUT", "/api/settings", `{"downloads": {"max_concurrent": 4}}`)
	if w.Code != 200 {
		t.Fatalf("PUT without maintenance: got %d body %s", w.Code, w.Body.String())
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if m := loaded.Maintenance; m.SearchResultRetentionDays != 14 || m.IndexMaxFiles != 200000 || m.IntervalHours != 6 {
		t.Errorf("maintenance not kept: %+v", m)
	}
	if loaded.Downloads.MaxConcurrent != 4 || len(loaded.Auth.TrustedNetworks) != 1 {
		t.Errorf("persisted: max_concurrent %d, trusted_networks %v", loaded.Downloads.MaxConcurrent, loaded.Auth.TrustedNetworks)
	}

	w = adminDo(t, srv, "PUT", "/api/settings", `{"downloads": {"max_concurrent": "x"}}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "max_concurrent") {
		t.Errorf("bad body: got %d %s, want 400 naming the field", w.Code, w.Body.String())
	}

	w = adminDo(t, srv, "PUT", "/api/settings", `{"auth": {"trusted_networks": ["nope"]}}`)
	if w.Code != 400 {
		t.Fatalf("invalid CIDR: got %d", w.Code)
	}
	if got := cfg.Auth.TrustedNetworks; len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Errorf("rejected PUT mutated live trusted_networks: %v", got)
	}
}

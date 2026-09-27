# Admin Settings (live apply) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admins edit the runtime settings from config.yaml in the web UI (Settings → System); changes are validated, written back to config.yaml with comments preserved, and take effect immediately without a restart.

**Architecture:** `config` gains an `Editable` subset type, a comment-preserving key writer (`SaveKeys`, replacing the line-based `WriteStorageDirs`) and env-lock detection. Each consumer gets a thread-safe live setter: `Engine.SetRuntime`, `Auth.Update` (atomic policy pointer), `Maintenance.SetConfig` (ticker reset), `Server.setDownloadsDir`. A new admin-only `/api/settings` handler validates → persists → applies. Dead config keys that no code reads are deleted first.

**Tech Stack:** Go 1.27.1 (stdlib, gopkg.in/yaml.v3 already a dependency), Alpine.js SPA in web/.

**Spec:** No separate design doc. Requirements agreed with the user on 2026-09-27: live apply (no restart); editable = storage (downloads_dir, temp_dir, min_free_space), downloads.max_concurrent, maintenance.*, auth.*; server and database read-only; remove config keys that no code reads. The Goal/Architecture above and Global Constraints below are the binding requirements.

## Global Constraints

- Build/test only via `make build` / `make test` (Go at `~/go-install/go/bin/go`; never `/usr/bin/go`). A race check may run `~/go-install/go/bin/go test -race <pkgs>` directly. Never start the server. Don't touch `.maxwell/` or `data/`.
- No new dependencies.
- Env override naming stays `XIRC_<SECTION>_<YAML_KEY_UPPER>` (config/config.go `applyEnvToStruct`).
- Settings API is admin-only for all methods: add `{"/api/settings", false}` to `adminRules` in server/auth.go.
- The database DSN is never returned by any API (it may contain a password).
- Config writes are atomic (temp file in the same dir + rename), keep the existing file mode (new file: 0640), 2-space YAML indent, preserve comments and all keys not being set.
- Validate everything before persisting; persist before applying; on persist failure apply nothing.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`: `git commit -m "<subject>" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"`. Don't push.
- Docker: never run any prune command; remove only containers/images you created.

## Review Focus

1. Concurrent reads during a settings save (a transfer starting while `downloads_dir` changes, requests authenticating while `trusted_networks` changes) — must be race-free: `-race` test in Tasks 3 and 4.
2. Admin locks themselves out: removing the only trusted network while having no user account. Expected: allowed (login still works via the setup page / `--create-admin`), but the UI warns before saving when `me.via === 'network'` and the new list would no longer contain the admin's own network — Task 6 confirm dialog.
3. Config file not writable (Pi: `/opt/xirc` owned by another user; Docker without a `/data/config.yaml`): GET reports `persist.ok=false` with a reason, PUT fails with that reason and changes nothing — Task 5 test.
4. Env var set for a field (Docker compose sets `XIRC_AUTH_TRUSTED_NETWORKS`): the field is shown locked; a PUT that changes it is rejected; a PUT that leaves it equal succeeds — Task 5 test.
5. Hand-written config with comments / unusual layout (flow lists, missing sections, file absent): saving keeps comments and unrelated keys and creates missing sections — Task 2 tests.

---

### Task 1: Delete config keys no code reads

**Files:**
- Modify: `config/config.go` (remove `AutoExtractConfig`, `StorageConfig.CriticalFreeSpace`, `StorageConfig.AutoExtract`, `DCCConfig`, `NotificationsConfig`, `Config.DCC`, `Config.Notifications`, their defaults)
- Modify: `config/config_test.go` (drop the critical_free_space default assertion at ~59-60; the YAML fixture at ~101 may keep the key — it proves leniency)
- Modify: `config.example.yaml`, `testdata/config_full.yaml`, `scripts/preconfig.sh` (config heredoc ~203-218 and the DCC passive/external-IP prompts that feed `DCC_PASSIVE_BOOL`/`DCC_EXTERNAL_IP`), `scripts/install.sh` (~189)
- Modify: `README.md`, `docs/install.md` wherever they mention these keys or the preconfig DCC prompts

**Interfaces:**
- Produces: `config.Config` without `DCC`/`Notifications`; `config.StorageConfig{MediaDir, DownloadsDir, TempDir, MinFreeSpace, CategoriesFile}`.

Verified dead (git grep, 2026-09-27): no Go code outside config/ reads `CriticalFreeSpace`, `Storage.AutoExtract`, `PassiveEnabled`, `PassivePorts`, `ExternalIP`, `QuietHoursStart/End`. Per-category auto-extract lives in library/config.go and is unrelated. `notify.EventPassiveDCC` / `EventDiskCritical` are event types, not config — leave them.

- [ ] **Step 1: Write the failing test** — old config files with the removed keys must still load.

```go
// config/config_test.go
func TestLoad_IgnoresRemovedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	old := `storage:
  downloads_dir: /dl
  critical_free_space: 500MB
  auto_extract:
    enabled: true
dcc:
  passive_enabled: true
  passive_ports: "30000-30010"
notifications:
  quiet_hours_start: "22:00"
`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.DownloadsDir != "/dl" {
		t.Errorf("downloads_dir = %q", cfg.Storage.DownloadsDir)
	}
}
```

- [ ] **Step 2: Run it** — `make test` → this test passes already (yaml.Unmarshal is lenient); it is the guard that keeps it that way. Now delete the fields/types/defaults; `make build` fails on any remaining reference — fix each (tests, fixtures).
- [ ] **Step 3: Scripts and docs** — remove the keys from `config.example.yaml`, `testdata/config_full.yaml`, `scripts/install.sh`, `scripts/preconfig.sh` (including its DCC prompts and variables); update README.md / docs/install.md (preconfig prompt table, config reference). `bash -n scripts/*.sh` (loop: `for f in scripts/*.sh; do bash -n "$f"; done`).
- [ ] **Step 4: Verify** — `make test`, `make build`, and `git grep -nE 'passive_enabled|passive_ports|external_ip|quiet_hours|critical_free_space|CriticalFreeSpace|DCCConfig|NotificationsConfig|AutoExtractConfig|DCC_PASSIVE|DCC_EXTERNAL' -- ':!docs/superpowers' ':!config/config_test.go'` → no hits.
- [ ] **Step 5: Commit** — `chore(config): drop settings no code reads (dcc, notifications, critical_free_space, global auto_extract)`

---

### Task 2: Editable subset, env locks, comment-preserving writer

**Files:**
- Modify: `config/config.go` (json tags, `Editable`, `EditableStorage`, `Editable()`, `ApplyEditable`, `EnvLockedKeys`; delete `WriteStorageDirs`)
- Create: `config/save.go` (`SaveKeys`)
- Modify: `server/setup_handler.go:62-80` (`ApplyMappings` uses `SaveKeys`)
- Test: `config/save_test.go`, `config/config_test.go` (replace the two `WriteStorageDirs` tests with `SaveKeys` equivalents)

**Interfaces:**
- Produces:
  - `type EditableStorage struct { DownloadsDir string \`yaml:"downloads_dir" json:"downloads_dir"\`; TempDir string \`yaml:"temp_dir" json:"temp_dir"\`; MinFreeSpace string \`yaml:"min_free_space" json:"min_free_space"\` }`
  - `type Editable struct { Storage EditableStorage \`yaml:"storage" json:"storage"\`; Downloads DownloadsConfig \`yaml:"downloads" json:"downloads"\`; Maintenance MaintenanceConfig \`yaml:"maintenance" json:"maintenance"\`; Auth AuthConfig \`yaml:"auth" json:"auth"\` }`
  - json tags equal to the yaml tags on `DownloadsConfig`, `MaintenanceConfig`, `AuthConfig` fields.
  - `func (c *Config) Editable() Editable`, `func (c *Config) ApplyEditable(e Editable)`
  - `func EnvLockedKeys() []string` — dotted yaml keys (e.g. `"storage.downloads_dir"`, `"auth.trusted_networks"`) of `Editable` fields whose `XIRC_*` env var is set.
  - `func SaveKeys(path string, v any) error` — `v` is anything yaml-encodable to a mapping (a struct like `Editable`, or `map[string]any`).

- [ ] **Step 1: Write the failing tests**

```go
// config/save_test.go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commented = `# top comment
server:
  host: 127.0.0.1 # bind
  port: 8085
storage:
  media_dir: /media   # seeds categories
  downloads_dir: /old
patterns:
  - name: x
    regex: "a(b)"
auth:
  trusted_networks: [10.0.0.0/8]
`

func TestSaveKeys_PreservesCommentsAndOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(commented), 0600); err != nil {
		t.Fatal(err)
	}
	e := Editable{
		Storage:     EditableStorage{DownloadsDir: "/new", TempDir: "/new/.tmp", MinFreeSpace: "2GB"},
		Downloads:   DownloadsConfig{MaxConcurrent: 4},
		Maintenance: MaintenanceConfig{SearchResultRetentionDays: 7, IndexMaxFiles: 1000, IntervalHours: 2},
		Auth:        AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user", TrustedProxies: []string{"127.0.0.1/32"}},
	}
	if err := SaveKeys(path, e); err != nil {
		t.Fatalf("SaveKeys: %v", err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"# top comment", "# bind", "# seeds categories", "media_dir: /media", `regex: "a(b)"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %q:\n%s", want, s)
		}
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Editable(); got.Storage.DownloadsDir != "/new" || got.Downloads.MaxConcurrent != 4 ||
		got.Maintenance.IntervalHours != 2 || got.Auth.TrustedRole != "user" ||
		len(got.Auth.TrustedNetworks) != 1 || got.Auth.TrustedNetworks[0] != "192.168.0.0/16" {
		t.Errorf("round trip: %+v", got)
	}
	if cfg.Server.Port != 8085 || cfg.Storage.MediaDir != "/media" || len(cfg.Patterns) != 1 {
		t.Errorf("unrelated keys changed: %+v", cfg)
	}
}

func TestSaveKeys_CreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := SaveKeys(path, map[string]any{"downloads": map[string]int{"max_concurrent": 2}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0640 {
		t.Fatalf("stat: %v %v", st, err)
	}
	cfg, _ := Load(path)
	if cfg.Downloads.MaxConcurrent != 2 {
		t.Errorf("max_concurrent = %d", cfg.Downloads.MaxConcurrent)
	}
}

func TestSaveKeys_LeavesNoTempOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(": not yaml : [\n"), 0644)
	if err := SaveKeys(path, map[string]any{"a": 1}); err == nil {
		t.Fatal("want parse error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestEnvLockedKeys(t *testing.T) {
	t.Setenv("XIRC_STORAGE_DOWNLOADS_DIR", "/x")
	t.Setenv("XIRC_AUTH_TRUSTED_NETWORKS", "10.0.0.0/8")
	got := strings.Join(EnvLockedKeys(), ",")
	if got != "storage.downloads_dir,auth.trusted_networks" {
		t.Errorf("EnvLockedKeys = %q", got)
	}
}
```

Replace `TestWriteStorageDirs_UpdatesFields` / `_Atomic` in config_test.go with the same intent using `SaveKeys(path, map[string]any{"storage": map[string]string{"media_dir": "/new/media", "downloads_dir": "/new/downloads"}})`.

- [ ] **Step 2: Run** — `make test` → FAIL (undefined: Editable, SaveKeys, EnvLockedKeys).

- [ ] **Step 3: Implement**

```go
// config/save.go
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveKeys writes the keys of v (anything that encodes to a YAML mapping)
// into the config file at path, keeping comments and every other key. Nested
// mappings merge; any other value replaces the old one. A missing file is
// created (0640). The write is atomic: temp file in the same dir, then rename.
func SaveKeys(path string, v any) error {
	var doc yaml.Node
	mode := os.FileMode(0640)
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return fmt.Errorf("reading config: %w", err)
	default:
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("config: top level is not a mapping")
	}
	var src yaml.Node
	if err := src.Encode(v); err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}
	mergeMapping(root, &src)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	enc.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}

// mergeMapping sets every key of src into dst. Mappings on both sides merge
// recursively; otherwise src's value replaces dst's, keeping dst's trailing
// comment.
func mergeMapping(dst, src *yaml.Node) {
	for i := 0; i+1 < len(src.Content); i += 2 {
		k, v := src.Content[i], src.Content[i+1]
		j := -1
		for n := 0; n+1 < len(dst.Content); n += 2 {
			if dst.Content[n].Value == k.Value {
				j = n
				break
			}
		}
		switch {
		case j < 0:
			dst.Content = append(dst.Content, k, v)
		case v.Kind == yaml.MappingNode && dst.Content[j+1].Kind == yaml.MappingNode:
			mergeMapping(dst.Content[j+1], v)
		default:
			v.LineComment = dst.Content[j+1].LineComment
			dst.Content[j+1] = v
		}
	}
}
```

```go
// config/config.go — additions (json tags go on DownloadsConfig, MaintenanceConfig, AuthConfig fields too)

// EditableStorage is the part of storage the admin UI may change at runtime.
type EditableStorage struct {
	DownloadsDir string `yaml:"downloads_dir" json:"downloads_dir"`
	TempDir      string `yaml:"temp_dir" json:"temp_dir"`
	MinFreeSpace string `yaml:"min_free_space" json:"min_free_space"`
}

// Editable is the subset of Config the admin settings page edits and the
// server applies live. Server and database settings are deliberately absent.
type Editable struct {
	Storage     EditableStorage   `yaml:"storage" json:"storage"`
	Downloads   DownloadsConfig   `yaml:"downloads" json:"downloads"`
	Maintenance MaintenanceConfig `yaml:"maintenance" json:"maintenance"`
	Auth        AuthConfig        `yaml:"auth" json:"auth"`
}

func (c *Config) Editable() Editable {
	return Editable{
		Storage:     EditableStorage{c.Storage.DownloadsDir, c.Storage.TempDir, c.Storage.MinFreeSpace},
		Downloads:   c.Downloads,
		Maintenance: c.Maintenance,
		Auth:        c.Auth,
	}
}

func (c *Config) ApplyEditable(e Editable) {
	c.Storage.DownloadsDir, c.Storage.TempDir, c.Storage.MinFreeSpace = e.Storage.DownloadsDir, e.Storage.TempDir, e.Storage.MinFreeSpace
	c.Downloads, c.Maintenance, c.Auth = e.Downloads, e.Maintenance, e.Auth
}

// EnvLockedKeys lists the dotted yaml keys of Editable whose XIRC_* override
// is set: a value saved from the UI would be replaced by the env var on the
// next start, so the UI shows these read-only.
func EnvLockedKeys() []string {
	var out []string
	t := reflect.TypeOf(Editable{})
	for i := 0; i < t.NumField(); i++ {
		sec := t.Field(i)
		secName := strings.Split(sec.Tag.Get("yaml"), ",")[0]
		for j := 0; j < sec.Type.NumField(); j++ {
			key := strings.Split(sec.Type.Field(j).Tag.Get("yaml"), ",")[0]
			if _, ok := os.LookupEnv("XIRC_" + strings.ToUpper(secName) + "_" + strings.ToUpper(key)); ok {
				out = append(out, secName+"."+key)
			}
		}
	}
	return out
}
```

Delete `WriteStorageDirs`. In `server/setup_handler.go` `ApplyMappings`:

```go
	if err := config.SaveKeys(state.ConfigPath, map[string]any{
		"storage": map[string]string{"media_dir": newMediaDir, "downloads_dir": newDownloadsDir},
	}); err != nil {
		return err
	}
```

- [ ] **Step 4: Run** — `make test` → PASS (including server setup tests `TestSetupComplete_WritesConfig`, `TestApplyMappings_WriteFailureLeavesSetupRequired`).
- [ ] **Step 5: Commit** — `feat(config): editable settings subset, env locks, comment-preserving writer`

---

### Task 3: Live setters in the queue engine and maintenance job

**Files:**
- Modify: `queue/engine.go` (own a `config.StorageConfig` copy; every read of storage fields and `maxConcurrent` via a locked snapshot; `SetRuntime`)
- Modify: `maintenance/maintenance.go` (`SetConfig`, resettable loop, test-only `unit`)
- Test: `queue/engine_test.go` (or the existing engine test file), `maintenance/maintenance_test.go`

**Interfaces:**
- Consumes: `config.StorageConfig`, `config.MaintenanceConfig`.
- Produces:
  - `func (e *Engine) SetRuntime(downloadsDir, tempDir, minFreeSpace string, maxConcurrent int)` — updates live values under `e.mu`, then `go e.TryDispatchQueued()`.
  - `func (m *Maintenance) SetConfig(cfg config.MaintenanceConfig)` — swaps config, resets the periodic timer (interval ≤ 0 stops periodic runs; > 0 restarts them; no immediate run).

Engine: `NewEngine` keeps its signature (callers pass `&cfg.Storage` or nil) but copies the value: `storage config.StorageConfig` field (`if storageCfg != nil { e.storage = *storageCfg }`). Replace `storageCfg` uses at ~359, 360, 365, 409, 710-713 with a snapshot taken once per operation:

```go
// runtime returns the live storage settings and concurrency limit.
func (e *Engine) runtime() (config.StorageConfig, int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.storage, e.maxConcurrent
}

// SetRuntime changes storage dirs, the free-space floor and the concurrency
// limit live. Transfers already running keep the paths they started with.
func (e *Engine) SetRuntime(downloadsDir, tempDir, minFreeSpace string, maxConcurrent int) {
	e.mu.Lock()
	e.storage.DownloadsDir, e.storage.TempDir, e.storage.MinFreeSpace = downloadsDir, tempDir, minFreeSpace
	e.maxConcurrent = maxConcurrent
	e.mu.Unlock()
	go e.TryDispatchQueued() // a raised limit may free slots for waiting downloads
}
```

`TryDispatchQueued` reads `maxConcurrent` via `runtime()` once at the top (before taking its own `e.mu.Lock()` section — never call `runtime()` while holding `e.mu`). `downloadsDir()` becomes `st, _ := e.runtime(); return st.DownloadsDir`. `Queue.maxConcurrent` (queue.go) is only used by `NextAndMarkDownloading`, which has no production caller — leave it.

- [ ] **Step 1: Write the failing tests**

```go
// queue: engine runtime
func TestEngineSetRuntime(t *testing.T) {
	e := NewEngine(nil, nil, nil, &config.StorageConfig{DownloadsDir: "/a", TempDir: "/a/.tmp", MinFreeSpace: "1GB"}, 3)
	e.SetRuntime("/b", "/b/.tmp", "2GB", 5)
	st, max := e.runtime()
	if st.DownloadsDir != "/b" || st.TempDir != "/b/.tmp" || st.MinFreeSpace != "2GB" || max != 5 {
		t.Fatalf("runtime = %+v, %d", st, max)
	}
	if e.downloadsDir() != "/b" {
		t.Errorf("downloadsDir = %q", e.downloadsDir())
	}
}

func TestEngineSetRuntimeRace(t *testing.T) {
	e := NewEngine(nil, nil, nil, &config.StorageConfig{DownloadsDir: "/a"}, 3)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			e.SetRuntime("/b", "/t", "1GB", i%5+1)
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_ = e.downloadsDir()
	}
	<-done
}
```

(`TryDispatchQueued` returns early when `ircMgr == nil`, so the `go` call in `SetRuntime` is safe in these tests.)

```go
// maintenance: interval change takes effect
func TestSetConfigRestartsTicker(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	m := New(store, config.MaintenanceConfig{IntervalHours: 0})
	m.unit = time.Millisecond // test hook: IntervalHours counts milliseconds
	var runs int32
	m.onRun = func() { atomic.AddInt32(&runs, 1) }
	m.Start()
	defer m.Stop()
	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Fatalf("interval 0: runs = %d, want only the immediate pass", n)
	}
	m.SetConfig(config.MaintenanceConfig{IntervalHours: 5})
	time.Sleep(60 * time.Millisecond)
	if n := atomic.LoadInt32(&runs); n < 3 {
		t.Fatalf("after SetConfig(5ms): runs = %d, want periodic runs", n)
	}
	m.SetConfig(config.MaintenanceConfig{IntervalHours: 0})
	time.Sleep(10 * time.Millisecond)
	before := atomic.LoadInt32(&runs)
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&runs) != before {
		t.Fatal("interval 0 again: periodic runs continued")
	}
}
```

- [ ] **Step 2: Run** — `make test` → FAIL (undefined SetRuntime/runtime/SetConfig/unit/onRun).

- [ ] **Step 3: Implement maintenance**

```go
type Maintenance struct {
	store   db.Store
	mu      sync.Mutex
	cfg     config.MaintenanceConfig
	unit    time.Duration // one IntervalHours step; tests shorten it
	onRun   func()        // test hook, called after each pass
	resetCh chan struct{}
	once    sync.Once
	stopCh  chan struct{}
}

func New(store db.Store, cfg config.MaintenanceConfig) *Maintenance {
	return &Maintenance{store: store, cfg: cfg, unit: time.Hour,
		resetCh: make(chan struct{}, 1), stopCh: make(chan struct{})}
}

func (m *Maintenance) config() config.MaintenanceConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SetConfig swaps the settings; the periodic timer restarts with the new
// interval (<= 0 stops periodic runs). No immediate pass.
func (m *Maintenance) SetConfig(cfg config.MaintenanceConfig) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	select {
	case m.resetCh <- struct{}{}:
	default:
	}
}

// Start runs an immediate pass, then repeats at the configured interval.
func (m *Maintenance) Start() {
	m.runOnce()
	go m.loop()
}

func (m *Maintenance) loop() {
	for {
		var tick <-chan time.Time
		var t *time.Ticker
		if iv := time.Duration(m.config().IntervalHours) * m.unit; iv > 0 {
			t = time.NewTicker(iv)
			tick = t.C
		}
		select {
		case <-tick:
			m.runOnce()
		case <-m.resetCh:
		case <-m.stopCh:
			if t != nil {
				t.Stop()
			}
			return
		}
		if t != nil {
			t.Stop()
		}
	}
}
```

`runOnce` reads `cfg := m.config()` once at the top instead of `m.cfg`, and ends with `if m.onRun != nil { m.onRun() }`.

- [ ] **Step 4: Implement the engine changes** (code above), then run `make test` and `~/go-install/go/bin/go test -race ./queue ./maintenance` → PASS, no races.
- [ ] **Step 5: Commit** — `feat(queue,maintenance): live-updatable storage, concurrency and schedule`

---

### Task 4: Live auth policy and downloads-dir root in the server

**Files:**
- Modify: `server/auth.go` (policy in an `atomic.Pointer`, `Update`)
- Modify: `server/server.go`, `server/capabilities.go`, `server/download_handlers.go:~348` (guarded downloads dir; tempDir setter)
- Test: `server/auth_test.go`, `server/capabilities_test.go` (or a new small test file)

**Interfaces:**
- Consumes: `config.AuthConfig`.
- Produces:
  - `func (a *Auth) Update(cfg config.AuthConfig) error` — validates exactly like `NewAuth`; on error nothing changes.
  - `func ParseAuthPolicy(cfg config.AuthConfig) error` — validation only (for the settings handler to validate before persisting). Implement as `_, err := parsePolicy(cfg); return err`.
  - `func (s *Server) setStorageDirs(downloadsDir, tempDir string)` and `func (s *Server) downloadsDirNow() string` — all reads of the downloads root go through the getter.

```go
type authPolicy struct {
	trustedNets []*net.IPNet
	proxies     []*net.IPNet
	trustedRole string
}

func parsePolicy(cfg config.AuthConfig) (*authPolicy, error) {
	if cfg.TrustedRole != "admin" && cfg.TrustedRole != "user" {
		return nil, fmt.Errorf("auth.trusted_role must be admin or user, got %q", cfg.TrustedRole)
	}
	nets, err := parseCIDRs(cfg.TrustedNetworks)
	if err != nil {
		return nil, fmt.Errorf("auth.trusted_networks: %w", err)
	}
	proxies, err := parseCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("auth.trusted_proxies: %w", err)
	}
	return &authPolicy{nets, proxies, cfg.TrustedRole}, nil
}

// Update swaps the trusted-network policy live; in-flight requests finish
// with the policy they started with.
func (a *Auth) Update(cfg config.AuthConfig) error {
	p, err := parsePolicy(cfg)
	if err != nil {
		return err
	}
	a.policy.Store(p)
	return nil
}
```

`Auth` loses `trustedNets/proxies/trustedRole` and gains `policy atomic.Pointer[authPolicy]`; `NewAuth` calls `parsePolicy` and `a.policy.Store`. Every former field read (clientIP ~114/123/135, principal ~182-183, startup log ~265-268) does `p := a.policy.Load()` once per call and uses `p.…`.

Server: move `downloadsDir` next to `tempDir` under `s.caps.mu` (already a mutex guarding capability inputs). `SetDownloadsDir` becomes a wrapper over `setStorageDirs` or is replaced by it (update main.go caller). `RecheckCapabilities` copies both dirs under the lock, then probes without holding it. `download_handlers.go:~348` and `configuredRoots` use `downloadsDirNow()`.

- [ ] **Step 1: Write the failing tests**

```go
// server/auth_test.go
func TestAuthUpdateChangesTrustedNetworks(t *testing.T) {
	a, err := NewAuth(config.AuthConfig{TrustedRole: "admin"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/downloads", nil)
	req.RemoteAddr = "192.168.1.5:1234"
	if p := a.networkPrincipal(req); p != nil {
		t.Fatalf("before update: %+v", p)
	}
	if err := a.Update(config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"}); err != nil {
		t.Fatal(err)
	}
	if p := a.networkPrincipal(req); p == nil || p.Role != "user" {
		t.Fatalf("after update: %+v", p)
	}
	if err := a.Update(config.AuthConfig{TrustedNetworks: []string{"nope"}, TrustedRole: "user"}); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
	if p := a.networkPrincipal(req); p == nil || p.Role != "user" {
		t.Fatalf("failed update changed policy: %+v", p)
	}
}
```

Use whatever the existing function at ~180-184 is called (the one returning `&Principal{Username: "lan", …, Via: "network"}`); if it's inline in the middleware, extract it as `networkPrincipal(r *http.Request) *Principal` as part of this task. Add a `-race` test that calls `Update` in a loop while another goroutine calls `networkPrincipal`, and one that calls `setStorageDirs` while another calls `downloadsDirNow`.

- [ ] **Step 2: Run** — `make test` → FAIL.
- [ ] **Step 3: Implement** (code above).
- [ ] **Step 4: Run** — `make test`, `~/go-install/go/bin/go test -race ./server` → PASS.
- [ ] **Step 5: Commit** — `feat(server): live-updatable auth policy and downloads root`

---

### Task 5: `/api/settings` handler and wiring

**Files:**
- Create: `server/settings_handler.go`, `server/settings_handler_test.go`
- Modify: `server/server.go` (route, `SetSettings`), `server/auth.go` (`adminRules` gets `{"/api/settings", false}`), `main.go` (wire config path, cfg, maintenance)

**Interfaces:**
- Consumes: Task 2 `config.Editable`, `Config.Editable/ApplyEditable`, `EnvLockedKeys`, `SaveKeys`; Task 3 `Engine.SetRuntime`, `Maintenance.SetConfig`; Task 4 `Auth.Update`, `ParseAuthPolicy`, `setStorageDirs`; existing `writableOrCreatable` (capabilities.go), `dcc.ParseSize`, `RecheckCapabilities`.
- Produces: `func (s *Server) SetSettings(path string, cfg *config.Config, maint *maintenance.Maintenance)`; `GET/PUT /api/settings`:

```json
{
  "settings": { "storage": {...}, "downloads": {...}, "maintenance": {...}, "auth": {...} },
  "locked":   ["auth.trusted_networks"],
  "persist":  { "ok": true, "reason": "" },
  "readonly": { "server": {"host": "127.0.0.1", "port": 8085, "prefix": ""},
                "database": {"driver": "sqlite", "path": "./data/xirc.db"},
                "media_dir": "/srv/media", "config_path": "config.yaml" },
  "warnings": []
}
```
PUT body = the `settings` object only. PUT response = the GET body (with `warnings` filled). Errors: `400 {"error": "..."}` via `writeError`, one message listing every validation failure joined by "; ".

Handler logic (hold `s.settings.mu` for the whole PUT so saves serialize):

```go
type settingsState struct {
	mu    sync.Mutex
	path  string
	cfg   *config.Config
	maint *maintenance.Maintenance
}

func validateSettings(e config.Editable) []string {
	var errs []string
	for _, d := range []struct{ key, dir string }{
		{"storage.downloads_dir", e.Storage.DownloadsDir}, {"storage.temp_dir", e.Storage.TempDir},
	} {
		if !filepath.IsAbs(d.dir) {
			errs = append(errs, d.key+" must be an absolute path")
		} else if c := writableOrCreatable(d.dir); !c.OK {
			errs = append(errs, d.key+": "+c.Reason)
		}
	}
	if _, err := dcc.ParseSize(e.Storage.MinFreeSpace); err != nil {
		errs = append(errs, "storage.min_free_space: "+err.Error())
	}
	if n := e.Downloads.MaxConcurrent; n < 1 || n > 20 {
		errs = append(errs, "downloads.max_concurrent must be 1–20")
	}
	m := e.Maintenance
	if m.SearchResultRetentionDays < 0 || m.IndexMaxFiles < 0 || m.IntervalHours < 0 {
		errs = append(errs, "maintenance values must be 0 or more")
	}
	if err := ParseAuthPolicy(e.Auth); err != nil {
		errs = append(errs, err.Error())
	}
	return errs
}
```

PUT order: decode (400 on bad JSON) → `validateSettings` → env-lock check: for every key in `config.EnvLockedKeys()` the submitted value must equal the current one (compare via the same dotted key; `reflect.DeepEqual` on the field), else error `"<key> is set by environment variable XIRC_… and can't be changed here"` → `config.SaveKeys(path, e)` (on error: 500 with `fscheck.Describe`-style reason if available, nothing applied) → apply: `s.engine.SetRuntime(...)`, `s.setStorageDirs(...)`, `s.auth.Update(e.Auth)` (already validated; if s.auth is nil skip), `maint.SetConfig(e.Maintenance)`, `cfg.ApplyEditable(e)` → `go s.RecheckCapabilities()` → respond with GET body. Warning (not error) when `trusted_role == "admin"` and trusted_networks contains `0.0.0.0/0` or `::/0`: "every client that reaches xirc gets admin".

`persist`: `writableOrCreatable(filepath.Dir(path))` → `{ok, reason}`. `readonly.database` never includes the DSN. `readonly.media_dir` with the UI note that the live media root is the Library settings' media root.

main.go: after creating `maint` and `srv`: `srv.SetSettings(*configPath, cfg, maint)`.

- [ ] **Step 1: Write the failing tests** (use the existing server test constructor helpers in server/*_test.go; build a `Server` with an engine from `queue.NewEngine(store, nil, nil, &cfg.Storage, 3)` and a maintenance from `maintenance.New(store, cfg.Maintenance)`, config file in `t.TempDir()`, dirs under `t.TempDir()`):
  - `TestSettingsGet_Shape`: 200; `settings.downloads.max_concurrent` matches cfg; `readonly.database` has no `dsn` key even when `cfg.Database.DSN = "user:secret@tcp(x)/db"` (assert the body doesn't contain `secret`); `persist.ok` true.
  - `TestSettingsPut_AppliesLiveAndPersists`: PUT with new downloads dir (existing temp subdir), max_concurrent 5, maintenance interval 2, trusted_networks `["192.168.0.0/16"]` role `user` → 200; `config.Load(path)` returns the new values; engine `runtime()` returns them (same package? no — engine is in queue: assert via a request to `/api/downloads/targets` or expose nothing new: assert `s.downloadsDirNow()` and a request from 192.168.1.5 is now a `user` principal hitting an admin route → 403).
  - `TestSettingsPut_ValidationErrors`: relative dir, `min_free_space: "lots"`, `max_concurrent: 0`, `trusted_role: "root"`, CIDR `"nope"` → 400 whose error mentions each key; config file unchanged (compare bytes).
  - `TestSettingsPut_EnvLocked`: `t.Setenv("XIRC_DOWNLOADS_MAX_CONCURRENT", "3")`; PUT changing max_concurrent → 400 naming the env var; PUT with max_concurrent unchanged but another field changed → 200.
  - `TestSettingsPut_PersistFailureAppliesNothing`: config path inside a directory made read-only (`os.Chmod(dir, 0555)`; skip when running as root) → error response; `s.downloadsDirNow()` unchanged; GET shows `persist.ok == false` with a reason.
  - adminRules: add `{"/api/settings", false}`; the existing `TestUserRoleForbiddenOnAdminRules` covers the 403.
- [ ] **Step 2: Run** — `make test` → FAIL.
- [ ] **Step 3: Implement** handler, route (`s.mux.HandleFunc("/api/settings", s.handleSettings)`), `SetSettings`, adminRules entry, main.go wiring.
- [ ] **Step 4: Run** — `make test`, `~/go-install/go/bin/go test -race ./server` → PASS.
- [ ] **Step 5: Commit** — `feat(server): admin settings API with validation, env locks and live apply`

---

### Task 6: Settings → System tab in the web UI, docs

**Files:**
- Modify: `web/index.html` (new tab + panel, admin-only), `web/js/app.js` (state + load/save), `web/js/api.js` (`getSettings`, `saveSettings`), `web/css/style.css` only if a class is needed
- Modify: `docs/install.md` (short "Settings in the web UI" subsection: what's editable, live apply, env-locked fields, config file must be writable by the xirc user; server/database only via config.yaml), `README.md` if it lists config options

**Interfaces:**
- Consumes: Task 5 API shape.
- Produces: `api.getSettings()` → GET `/settings`; `api.saveSettings(s)` → PUT `/settings`.

UI requirements (follow the existing Users tab markup/styles, `web/index.html` ~986 and ~1369):
- Tab `System`, shown only when `isAdmin` (`x-show="isAdmin"` on the tab; panel `x-if="settingsTab === 'system' && isAdmin"`), click → `loadSystemSettings()`.
- Cards: **Storage** (Downloads folder, Temp folder, Minimum free space e.g. "1GB"), **Downloads** (Max parallel downloads, number 1–20), **Maintenance** (Keep search results (days, 0 = forever), Max indexed files (0 = no cap), Run every (hours, 0 = only at start)), **Login** (Networks that skip login — comma-separated CIDRs; Role for those networks — admin/user select; Trusted proxies — comma-separated). Lists are edited as comma-separated strings and split/trimmed on save (empty → `[]`).
- A field whose dotted key is in `locked` is `disabled` with `title="Set by environment variable XIRC_<SECTION>_<KEY>"`.
- If `persist.ok` is false: a warning line with `persist.reason` above the Save button, and Save disabled.
- Read-only card: server host/port/prefix, database driver/path, config file path, and "Media root: <media_dir> — change it under Settings → Library."
- Save → `api.saveSettings(...)`; on error show the message in a red line above Save (not `alert`); on success reload from the response, show "Saved — applied" for 3 s, and show `warnings` as a yellow line.
- Before saving: if `me.via === 'network'` and the new trusted-networks list is empty or differs from the current one, `confirm("You're signed in through a trusted network. After saving you may need to log in with a user account. Continue?")`.
- `node --check web/js/app.js web/js/api.js`.

- [ ] **Step 1: api.js** — add the two methods under an `// Settings` comment.
- [ ] **Step 2: app.js** — state `systemSettings: null, systemForm: null, systemError: '', systemNotice: ''`; `loadSystemSettings()`, `saveSystemSettings()`; helpers to convert lists ↔ comma strings.
- [ ] **Step 3: index.html** — tab + panel as specified.
- [ ] **Step 4: Verify** — `node --check`; `make build`; `make test`. Manual check is the controller's/user's (server is not started by agents): describe in the report exactly which elements to click.
- [ ] **Step 5: Docs** — docs/install.md subsection; README if applicable.
- [ ] **Step 6: Commit** — `feat(web): admin System settings tab with live apply`

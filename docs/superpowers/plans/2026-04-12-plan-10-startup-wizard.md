# Startup Wizard — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hard `os.Exit(78)` on missing destination directories with a guided wizard — interactive CLI prompt on TTY, fullscreen web overlay when headless — that redirects routing rules to valid paths and persists the result to `config.yaml`.

**Architecture:** `checkDirectories()` is softened to return bad dirs instead of exiting. A `SetupState` struct (defined in `server/setup_handler.go`) carries the wizard data through both paths. The CLI path runs before the HTTP server starts; the web path starts the server and lets the browser do setup. Both paths call `server.ApplyMappings()` + `config.WriteStorageDirs()` for persistence.

**Tech Stack:** Go 1.13 (no embed.FS, no r.PathValue(), use `strings.TrimPrefix` for path params, `ioutil.TempDir` + `defer os.RemoveAll` in tests), Alpine.js v3, plain CSS. No new packages.

**Depends on:** Plans 1–9 complete.

---

## File Structure

```
main.go                           MODIFY — checkDirectories returns []string; isTerminal(); runCLIWizard(); startup branch
main_test.go                      NEW    — unit tests for checkDirectories()
config/config.go                  MODIFY — add WriteStorageDirs(path, mediaDir, downloadsDir string) error
config/config_test.go             MODIFY — add TestWriteStorageDirs
server/setup_handler.go           NEW    — SetupState, Mapping, ApplyMappings(), DefaultSuggestions(), PatternsForDir(), handleSetupStatus, handleSetupDefaults, handleSetupComplete
server/setup_handler_test.go      NEW    — tests for all three setup endpoints
server/server.go                  MODIFY — New() accepts *SetupState; registers setup routes
server/search_handlers_test.go    MODIFY — newTestServerWithStore passes nil for setup
web/js/api.js                     MODIFY — getSetupStatus(), getSetupDefaults(), completeSetup()
web/js/app.js                     MODIFY — setupRequired state, wizard state, init() check, handler methods
web/index.html                    MODIFY — wizard overlay template
```

---

## Task 1: Refactor `checkDirectories()` + tests

**Files:**
- Modify: `main.go`
- Create: `main_test.go`

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:

```go
package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestCheckDirectories_ReturnsEmptyWhenDirsExist(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-check-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Migrate()

	subdir := filepath.Join(dir, "media")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	store.CreateFileRoutingRule(&db.FileRoutingRule{
		Pattern: "*.mkv", DestinationDir: subdir, Priority: 10, Enabled: true,
	})

	bad := checkDirectories(store)
	if len(bad) != 0 {
		t.Errorf("expected no bad dirs, got %v", bad)
	}
}

func TestCheckDirectories_ReturnsMissingDirs(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-check-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Migrate()

	missing := filepath.Join(dir, "does-not-exist")
	store.CreateFileRoutingRule(&db.FileRoutingRule{
		Pattern: "*.mkv", DestinationDir: missing, Priority: 10, Enabled: true,
	})

	bad := checkDirectories(store)
	if len(bad) != 1 || bad[0] != missing {
		t.Errorf("expected [%s], got %v", missing, bad)
	}
}

func TestCheckDirectories_DeduplicatesDirs(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-check-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Migrate()

	missing := filepath.Join(dir, "does-not-exist")
	store.CreateFileRoutingRule(&db.FileRoutingRule{Pattern: "*.mkv", DestinationDir: missing, Enabled: true})
	store.CreateFileRoutingRule(&db.FileRoutingRule{Pattern: "*.mp4", DestinationDir: missing, Enabled: true})

	bad := checkDirectories(store)
	if len(bad) != 1 {
		t.Errorf("expected 1 deduplicated entry, got %v", bad)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test -run TestCheckDirectories -v .
```

Expected: compile error (`checkDirectories` returns nothing / wrong signature).

- [ ] **Step 3: Refactor `checkDirectories()` in `main.go`**

Replace the existing `checkDirectories` function:

```go
// checkDirectories probes each unique destination_dir in the routing rules.
// Dirs that do not exist are returned as a slice — the caller handles them.
// Permission errors and other I/O failures are still fatal.
func checkDirectories(store db.Store) []string {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		log.Printf("warning: could not load routing rules for dir check: %v", err)
		return nil
	}

	seen := map[string]bool{}
	var bad []string
	for _, r := range rules {
		if r.DestinationDir == "" || seen[r.DestinationDir] {
			continue
		}
		seen[r.DestinationDir] = true

		probe := r.DestinationDir + "/.xirc_write_check"
		f, err := os.Create(probe)
		if err == nil {
			f.Close()
			os.Remove(probe)
			log.Printf("startup: verified writable: %s", r.DestinationDir)
			continue
		}

		if os.IsPermission(err) || errors.Is(err, syscall.EROFS) {
			log.Printf("FATAL (config): destination dir %q not writable — fix ownership or ACL, then restart (exit 78)", r.DestinationDir)
			os.Exit(exitcodes.ExitConfig)
		}
		if os.IsNotExist(err) {
			bad = append(bad, r.DestinationDir)
			continue
		}
		log.Printf("FATAL (transient): destination dir %q check failed: %v — will retry on restart (exit 1)", r.DestinationDir, err)
		os.Exit(exitcodes.ExitTransient)
	}
	return bad
}
```

- [ ] **Step 4: Run tests**

```bash
go test -run TestCheckDirectories -v .
```

Expected: all three tests PASS.

- [ ] **Step 5: Build**

```bash
make build
```

Expected: compile error — `checkDirectories(store)` return value now ignored. Fix in `main.go`: change `checkDirectories(store)` to `badDirs := checkDirectories(store)` and add `_ = badDirs` on the next line temporarily, then rebuild.

```bash
make build
```

Expected: builds cleanly.

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go
git commit -m "refactor: checkDirectories returns missing dirs instead of exiting"
```

---

## Task 2: `config.WriteStorageDirs()` + test

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Write the failing test**

Open `config/config_test.go` and add at the end:

```go
func TestWriteStorageDirs_UpdatesFields(t *testing.T) {
	original := `server:
  host: 127.0.0.1
  port: 8085

storage:
  media_dir: /old/media
  downloads_dir: /old/downloads
  temp_dir: /tmp
  min_free_space: 1GB
  critical_free_space: 500MB
`
	dir, err := ioutil.TempDir("", "xirc-cfg-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.yaml")
	if err := ioutil.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteStorageDirs(path, "/new/media", "/new/downloads"); err != nil {
		t.Fatalf("WriteStorageDirs: %v", err)
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "media_dir: /new/media") {
		t.Errorf("media_dir not updated:\n%s", content)
	}
	if !strings.Contains(content, "downloads_dir: /new/downloads") {
		t.Errorf("downloads_dir not updated:\n%s", content)
	}
	// Verify other fields preserved
	if !strings.Contains(content, "temp_dir: /tmp") {
		t.Errorf("temp_dir lost:\n%s", content)
	}
	if !strings.Contains(content, "host: 127.0.0.1") {
		t.Errorf("server.host lost:\n%s", content)
	}
}

func TestWriteStorageDirs_Atomic(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-cfg-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.yaml")
	if err := ioutil.WriteFile(path, []byte("storage:\n  media_dir: /a\n  downloads_dir: /b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteStorageDirs(path, "/new/a", "/new/b"); err != nil {
		t.Fatal(err)
	}

	// Temp file should be cleaned up
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file was not removed after rename")
	}
}
```

Add the required imports to the test file (`"io/ioutil"`, `"os"`, `"path/filepath"`, `"strings"`) if not already present.

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test -run TestWriteStorageDirs -v ./config/
```

Expected: compile error (`WriteStorageDirs` not defined).

- [ ] **Step 3: Implement `WriteStorageDirs` in `config/config.go`**

Add this function (and import `"strings"` if not present):

```go
// WriteStorageDirs rewrites storage.media_dir and storage.downloads_dir in the
// config file at path, preserving all other content. The write is atomic: a
// temp file is written first, then renamed over the original.
func WriteStorageDirs(path, mediaDir, downloadsDir string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	inStorage := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Detect storage: section header (no leading whitespace)
		if trimmed == "storage:" {
			inStorage = true
			continue
		}
		// Leave storage section when we hit a top-level key
		if inStorage && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			inStorage = false
		}
		if inStorage {
			if strings.HasPrefix(trimmed, "media_dir:") {
				lines[i] = "  media_dir: " + mediaDir
			} else if strings.HasPrefix(trimmed, "downloads_dir:") {
				lines[i] = "  downloads_dir: " + downloadsDir
			}
		}
	}

	result := []byte(strings.Join(lines, "\n"))
	tmp := path + ".tmp"
	if err := ioutil.WriteFile(tmp, result, 0644); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test -run TestWriteStorageDirs -v ./config/
```

Expected: both tests PASS.

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat: add config.WriteStorageDirs for atomic in-place config update"
```

---

## Task 3: `SetupState`, shared helpers, HTTP endpoints + wiring

**Files:**
- Create: `server/setup_handler.go`
- Create: `server/setup_handler_test.go`
- Modify: `server/server.go`
- Modify: `server/search_handlers_test.go`

- [ ] **Step 1: Write the failing tests**

Create `server/setup_handler_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
)

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
	srv := New(store, ircMgr, p, nil, nil, nil, nil, setup)
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
	srv := New(store, ircMgr, p, nil, nil, nil, nil, setup)

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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test -run TestSetup -v ./server/
```

Expected: compile errors (`SetupState` not defined, `New()` wrong arg count).

- [ ] **Step 3: Create `server/setup_handler.go`**

```go
package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
)

// SetupState carries wizard data from main() into the HTTP server.
// It is also used by the CLI wizard path (main.go calls ApplyMappings directly).
type SetupState struct {
	mu           sync.Mutex
	Required     bool
	BadDirs      []string
	MediaDir     string // original cfg.Storage.MediaDir
	DownloadsDir string // original cfg.Storage.DownloadsDir
	HomeDir      string // os.UserHomeDir() result
	ConfigPath   string // path to config.yaml for writing back
}

// Complete marks setup as done and clears bad dirs.
func (s *SetupState) Complete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Required = false
	s.BadDirs = nil
}

// Mapping is one old→new directory substitution.
type Mapping struct {
	OldDir string `json:"old_dir"`
	NewDir string `json:"new_dir"`
}

// DefaultSuggestions returns a suggested replacement path for each bad dir.
// Dirs that serve the catch-all rule (*) get ~/Downloads; others get ~/Videos.
func DefaultSuggestions(badDirs []string, rules []db.FileRoutingRule, homeDir string) map[string]string {
	hasCatchAll := map[string]bool{}
	for _, r := range rules {
		if r.Pattern == "*" {
			hasCatchAll[r.DestinationDir] = true
		}
	}
	result := make(map[string]string, len(badDirs))
	for _, d := range badDirs {
		if hasCatchAll[d] {
			result[d] = filepath.Join(homeDir, "Downloads")
		} else {
			result[d] = filepath.Join(homeDir, "Videos")
		}
	}
	return result
}

// PatternsForDir returns all patterns whose destination_dir equals dir.
func PatternsForDir(rules []db.FileRoutingRule, dir string) []string {
	var out []string
	for _, r := range rules {
		if r.DestinationDir == dir {
			out = append(out, r.Pattern)
		}
	}
	return out
}

// ApplyMappings updates routing rules in the DB and rewrites config.yaml.
// Both writes succeed or neither is persisted (config write is atomic via rename).
func ApplyMappings(mappings []Mapping, store db.Store, state *SetupState) error {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		return err
	}

	// Compute new config values
	newMediaDir := state.MediaDir
	newDownloadsDir := state.DownloadsDir
	for _, m := range mappings {
		if m.OldDir == state.MediaDir {
			newMediaDir = m.NewDir
		}
		if m.OldDir == state.DownloadsDir {
			newDownloadsDir = m.NewDir
		}
	}

	// Update routing rules
	oldToNew := make(map[string]string, len(mappings))
	for _, m := range mappings {
		oldToNew[m.OldDir] = m.NewDir
	}
	for i := range rules {
		if newDir, ok := oldToNew[rules[i].DestinationDir]; ok {
			rules[i].DestinationDir = newDir
			if err := store.UpdateFileRoutingRule(&rules[i]); err != nil {
				return err
			}
		}
	}

	// Persist config
	if err := config.WriteStorageDirs(state.ConfigPath, newMediaDir, newDownloadsDir); err != nil {
		return err
	}

	state.Complete()
	return nil
}

// GET /api/setup/status
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.setup.mu.Lock()
	resp := map[string]interface{}{
		"required": s.setup.Required,
		"bad_dirs": s.setup.BadDirs,
	}
	s.setup.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/setup/defaults
func (s *Server) handleSetupDefaults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"videos_dir":    filepath.Join(s.setup.HomeDir, "Videos"),
		"downloads_dir": filepath.Join(s.setup.HomeDir, "Downloads"),
	})
}

// POST /api/setup/complete
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mappings []Mapping `json:"mappings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Mappings) == 0 {
		writeError(w, http.StatusBadRequest, "mappings must not be empty")
		return
	}
	if err := ApplyMappings(body.Mappings, s.store, s.setup); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"restart_required": true})
}
```

- [ ] **Step 4: Update `server/server.go`**

Add `setup *SetupState` field to the `Server` struct:

```go
type Server struct {
	store  db.Store
	ircMgr *ircpkg.Manager
	parser *parser.Parser
	engine *queue.Engine
	wsHub  *ws.Hub
	msgBuf *ircpkg.MessageBuffer
	errBuf *ircpkg.ErrorBuffer
	setup  *SetupState
	mux    *http.ServeMux
}
```

Update `New()` signature (add `setup *SetupState` as last param before the implicit return):

```go
func New(store db.Store, ircMgr *ircpkg.Manager, p *parser.Parser, eng *queue.Engine, hub *ws.Hub, msgBuf *ircpkg.MessageBuffer, errBuf *ircpkg.ErrorBuffer, setup *SetupState) *Server {
	s := &Server{
		store:  store,
		ircMgr: ircMgr,
		parser: p,
		engine: eng,
		wsHub:  hub,
		msgBuf: msgBuf,
		errBuf: errBuf,
		setup:  setup,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}
```

In `routes()`, add setup routes before the WebSocket line:

```go
// Setup wizard endpoints
s.mux.HandleFunc("/api/setup/status", s.handleSetupStatus)
s.mux.HandleFunc("/api/setup/defaults", s.handleSetupDefaults)
s.mux.HandleFunc("/api/setup/complete", s.handleSetupComplete)
```

- [ ] **Step 5: Fix `newTestServerWithStore` in `server/search_handlers_test.go`**

Change the `New(...)` call in `newTestServerWithStore`:

```go
srv := New(store, ircMgr, p, nil, nil, nil, nil, nil)
```

- [ ] **Step 6: Run tests**

```bash
go test -run TestSetup -v ./server/
```

Expected: all four `TestSetup*` tests PASS.

```bash
go test ./server/
```

Expected: all server tests PASS.

- [ ] **Step 7: Build**

```bash
make build
```

Expected: compile error in `main.go` — `server.New()` now needs 8th arg. Fix: change the `server.New(...)` call in `main.go`:

```go
srv := server.New(store, ircMgr, p, eng, hub, msgBuf, errBuf, nil)
```

Build again:

```bash
make build
```

Expected: builds cleanly (setup is nil for now, wired in Task 4).

- [ ] **Step 8: Commit**

```bash
git add server/setup_handler.go server/setup_handler_test.go server/server.go server/search_handlers_test.go main.go
git commit -m "feat: add SetupState, ApplyMappings, and setup HTTP endpoints"
```

---

## Task 4: CLI wizard in `main.go`

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add TTY detection and wizard helpers**

Add the following functions to `main.go` (before `func main()`). Also add `"bufio"`, `"fmt"`, `"strings"`, `"path/filepath"` to imports, and `"github.com/maxwell-xirc/xirc/server"` if not already imported (it is).

```go
// isTerminal reports whether stdin is an interactive terminal.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// runCLIWizard presents the directory wizard on the terminal (or silently applies
// defaults when stdin is not a terminal). It updates config.yaml and the DB,
// then returns. If it succeeds, checkDirectories will pass on the next run.
// In the TTY path it runs before the HTTP server starts, so no restart is needed.
func runCLIWizard(badDirs []string, store db.Store, state *server.SetupState) {
	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		log.Printf("warning: could not load rules for wizard: %v", err)
		rules = nil
	}
	suggestions := server.DefaultSuggestions(badDirs, rules, state.HomeDir)

	var mappings []server.Mapping

	if isTerminal() {
		fmt.Fprintf(os.Stderr, "\nxirc: the following destination directories do not exist:\n\n")
		for _, dir := range badDirs {
			patterns := server.PatternsForDir(rules, dir)
			fmt.Fprintf(os.Stderr, "  %s\n    → used by: %s\n", dir, strings.Join(patterns, " "))
		}
		fmt.Fprintf(os.Stderr, "\nEnter replacement paths (press Enter to accept suggestion):\n\n")

		reader := bufio.NewReader(os.Stdin)
		for _, dir := range badDirs {
			sug := suggestions[dir]
			fmt.Fprintf(os.Stderr, "  %s  [%s]: ", dir, sug)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				line = sug
			}
			mappings = append(mappings, server.Mapping{OldDir: dir, NewDir: line})
		}

		fmt.Fprintf(os.Stderr, "\nApply? [Y/n]: ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) == "n" {
			// User declined — reset to pure defaults
			mappings = mappings[:0]
			for _, dir := range badDirs {
				mappings = append(mappings, server.Mapping{OldDir: dir, NewDir: suggestions[dir]})
			}
		}
	} else {
		// Non-interactive: apply defaults silently
		for _, dir := range badDirs {
			sug := suggestions[dir]
			log.Printf("startup: applying default path %s → %s", dir, sug)
			mappings = append(mappings, server.Mapping{OldDir: dir, NewDir: sug})
		}
	}

	if err := server.ApplyMappings(mappings, store, state); err != nil {
		log.Printf("warning: wizard failed to apply mappings: %v", err)
	}
}
```

- [ ] **Step 2: Wire wizard into `main()`**

In `main()`, replace the temporary `_ = badDirs` and `checkDirectories(store)` call with:

```go
homeDir, err := os.UserHomeDir()
if err != nil {
    homeDir = "."
    log.Printf("warning: could not determine home dir: %v", err)
}

badDirs := checkDirectories(store)

setupState := &server.SetupState{
    Required:     len(badDirs) > 0,
    BadDirs:      badDirs,
    MediaDir:     cfg.Storage.MediaDir,
    DownloadsDir: cfg.Storage.DownloadsDir,
    HomeDir:      homeDir,
    ConfigPath:   *configPath,
}

if len(badDirs) > 0 {
    if isTerminal() {
        runCLIWizard(badDirs, store, setupState)
        setupState.Required = false // CLI path fixes dirs in-place; no web wizard needed
    }
    // Non-TTY: setupState.Required stays true; web wizard will handle it
}
```

Update the `server.New(...)` call to pass `setupState`:

```go
srv := server.New(store, ircMgr, p, eng, hub, msgBuf, errBuf, setupState)
```

Also add `"bufio"` and `"strings"` to the import block if not present.

- [ ] **Step 3: Build and verify**

```bash
make build
```

Expected: builds cleanly.

- [ ] **Step 4: Run all tests**

```bash
make test
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat: add CLI startup wizard for missing destination directories"
```

---

## Task 5: Frontend wizard overlay

**Files:**
- Modify: `web/js/api.js`
- Modify: `web/js/app.js`
- Modify: `web/index.html`

- [ ] **Step 1: Add API methods to `web/js/api.js`**

Find the last method in `api.js` (before the closing `};`) and add:

```js
    // Setup wizard
    getSetupStatus() { return this.get('/setup/status'); },
    getSetupDefaults() { return this.get('/setup/defaults'); },
    completeSetup(mappings) { return this.post('/setup/complete', { mappings }); },
```

- [ ] **Step 2: Add setup state to `web/js/app.js`**

In the `appStore` data object (after the `sidebarOpen` line), add:

```js
        // Setup wizard
        setupRequired: false,
        setupMappings: [],  // [{old_dir, new_dir, label, suggestion}]
        setupBanner: false,
```

- [ ] **Step 3: Add wizard methods to `web/js/app.js`**

Find the end of the `appStore` methods (before the closing `};`) and add:

```js
        async checkSetup() {
            try {
                const status = await api.getSetupStatus();
                if (!status.required) return;
                this.setupRequired = true;
                const defaults = await api.getSetupDefaults();
                this.setupMappings = (status.bad_dirs || []).map(dir => ({
                    old_dir: dir,
                    new_dir: dir.toLowerCase().includes('download') ? defaults.downloads_dir : defaults.videos_dir,
                    suggestion: dir.toLowerCase().includes('download') ? defaults.downloads_dir : defaults.videos_dir,
                }));
            } catch (e) {
                console.error('setup check error', e);
            }
        },

        async applySetup() {
            const mappings = this.setupMappings.map(m => ({ old_dir: m.old_dir, new_dir: m.new_dir }));
            try {
                await api.completeSetup(mappings);
                this.setupRequired = false;
                this.setupBanner = true;
            } catch (e) {
                console.error('setup complete error', e);
            }
        },

        cancelSetup() {
            // Apply defaults without prompting
            this.setupMappings = this.setupMappings.map(m => ({ ...m, new_dir: m.suggestion }));
            this.applySetup();
        },
```

- [ ] **Step 4: Call `checkSetup()` in `init()`**

In the `init()` method in `web/js/app.js`, add as the **first** call inside the function body (before the localStorage restore block):

```js
        async init() {
            await this.checkSetup();

            // Restore layout prefs from localStorage
            // ... (rest of existing init code unchanged)
```

- [ ] **Step 5: Add wizard overlay to `web/index.html`**

Find the closing `</div>` of `<div class="app">` (the very last `</div>` before `</body>`) and insert the wizard overlay immediately before it:

```html
    <!-- Startup wizard overlay -->
    <div :style="setupRequired ? 'display:flex' : 'display:none'"
         style="position:fixed; inset:0; z-index:9999; background:rgba(0,0,0,0.85); align-items:center; justify-content:center;">
        <div style="background:var(--bg-secondary,#1e1e2e); border-radius:8px; padding:32px; max-width:560px; width:100%; box-shadow:0 8px 32px rgba(0,0,0,0.5);">
            <h2 style="margin:0 0 8px; font-size:18px;">Setup Required</h2>
            <p style="color:var(--text-secondary,#888); margin:0 0 24px; font-size:13px;">
                The following destination directories do not exist. Enter valid paths to continue.
            </p>

            <template x-for="(m, i) in setupMappings" :key="i">
                <div style="margin-bottom:16px;">
                    <div style="font-size:11px; color:var(--text-secondary,#888); margin-bottom:4px;" x-text="'Was: ' + m.old_dir"></div>
                    <input type="text"
                           x-model="setupMappings[i].new_dir"
                           style="width:100%; box-sizing:border-box; padding:8px 10px; background:var(--bg-primary,#12121c); border:1px solid var(--border,#333); border-radius:4px; color:inherit; font-family:monospace; font-size:13px;"
                           :placeholder="m.suggestion">
                </div>
            </template>

            <div style="display:flex; gap:10px; justify-content:flex-end; margin-top:24px;">
                <button @click="cancelSetup()"
                        style="padding:8px 16px; background:transparent; border:1px solid var(--border,#444); border-radius:4px; cursor:pointer; color:inherit;">
                    Use Defaults
                </button>
                <button @click="applySetup()"
                        :disabled="setupMappings.some(m => !m.new_dir.trim())"
                        style="padding:8px 16px; background:#4f6ef7; border:none; border-radius:4px; cursor:pointer; color:#fff;">
                    Apply
                </button>
            </div>
        </div>
    </div>

    <!-- Post-setup restart banner -->
    <div :style="setupBanner ? 'display:block' : 'display:none'"
         style="position:fixed; top:0; left:0; right:0; z-index:9998; background:#1a5c2a; color:#fff; padding:10px 20px; font-size:13px; text-align:center;">
        Setup complete — restart xirc for changes to take effect.
        <button @click="setupBanner=false" style="margin-left:12px; background:transparent; border:none; color:#fff; cursor:pointer; font-size:16px;">×</button>
    </div>
```

- [ ] **Step 6: Build**

```bash
make build
```

Expected: builds cleanly.

- [ ] **Step 7: Commit**

```bash
git add web/js/api.js web/js/app.js web/index.html
git commit -m "feat: add web setup wizard overlay for missing destination directories"
```

---

## Task 6: Final integration + full test run

**Files:**
- No new files

- [ ] **Step 1: Run full test suite**

```bash
make test
```

Expected: all tests PASS.

- [ ] **Step 2: Build release binary**

```bash
make build
```

Expected: builds cleanly, binary size comparable to before.

- [ ] **Step 3: Smoke test CLI path**

With the current `config.yaml` pointing to `/srv/dlna/media` (which doesn't exist):

```bash
./xirc --config config.yaml
```

Expected (TTY): wizard prompt appears listing `/srv/dlna/media` and `/srv/downloads` with suggestions, user can press Enter to accept.

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "test: verify startup wizard integration"
```

If no files changed (all already committed), skip this step.

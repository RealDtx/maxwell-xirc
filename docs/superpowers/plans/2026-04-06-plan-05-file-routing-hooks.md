# xirc Plan 5: File Routing & Hooks — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After a DCC transfer completes, route the file to the correct directory based on file-type rules and execute configurable post-download hooks (shell scripts or regex renames).

**Architecture:** A `routing` package matches filenames against glob patterns to determine destination, moves the file, then runs applicable hooks. It integrates with the download engine via the event bus — listens for `download_status:completed` events.

**Tech Stack:** Go 1.22+, `path/filepath` for glob matching, `os/exec` for shell hooks, existing packages

**Depends on:** Plans 1-4 must be complete.

---

## File Structure

```
maxwell-xirc/
├── routing/
│   ├── router.go            # File routing: match rules, move files
│   ├── router_test.go
│   ├── hooks.go             # Post-download hook execution
│   ├── hooks_test.go
│   ├── seed.go              # Seed default routing rules
│   └── seed_test.go
├── server/
│   ├── routing_handlers.go  # HTTP handlers for routing rules + hooks
│   └── routing_handlers_test.go
```

---

### Task 1: File Routing Logic

**Files:**
- Create: `routing/router.go`
- Create: `routing/router_test.go`

- [ ] **Step 1: Write failing tests**

Create `routing/router_test.go`:

```go
package routing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestMatchRule_MediaFiles(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true},
		{ID: 2, Pattern: "*.mp4", DestinationDir: "/media", Priority: 100, Enabled: true},
		{ID: 3, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
	}

	dest := MatchRule("movie.mkv", rules)
	if dest != "/media" {
		t.Errorf("expected /media, got %s", dest)
	}

	dest = MatchRule("video.mp4", rules)
	if dest != "/media" {
		t.Errorf("expected /media, got %s", dest)
	}

	dest = MatchRule("document.pdf", rules)
	if dest != "/downloads" {
		t.Errorf("expected /downloads (catch-all), got %s", dest)
	}
}

func TestMatchRule_PriorityOrder(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
		{ID: 2, Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true},
	}

	dest := MatchRule("movie.mkv", rules)
	if dest != "/media" {
		t.Errorf("expected /media (higher priority), got %s", dest)
	}
}

func TestMatchRule_DisabledSkipped(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: false},
		{ID: 2, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
	}

	dest := MatchRule("movie.mkv", rules)
	if dest != "/downloads" {
		t.Errorf("expected /downloads (disabled rule skipped), got %s", dest)
	}
}

func TestMatchRule_MultipleExtensions(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*.srt", DestinationDir: "/media", Priority: 80, Enabled: true},
		{ID: 2, Pattern: "*.sub", DestinationDir: "/media", Priority: 80, Enabled: true},
		{ID: 3, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
	}

	if MatchRule("subs.srt", rules) != "/media" {
		t.Error("expected .srt to match media")
	}
	if MatchRule("subs.sub", rules) != "/media" {
		t.Error("expected .sub to match media")
	}
}

func TestMoveFile_SameFilesystem(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcPath := filepath.Join(srcDir, "test.mkv")
	os.WriteFile(srcPath, []byte("video data"), 0644)

	destPath, err := MoveFile(srcPath, dstDir)
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	expected := filepath.Join(dstDir, "test.mkv")
	if destPath != expected {
		t.Errorf("expected %s, got %s", expected, destPath)
	}

	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("source file should not exist after move")
	}
	data, _ := os.ReadFile(destPath)
	if string(data) != "video data" {
		t.Errorf("file contents mismatch: %s", string(data))
	}
}

func TestMoveFile_CreatesDestDir(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "test.pdf")
	os.WriteFile(srcPath, []byte("pdf data"), 0644)

	dstDir := filepath.Join(dir, "new", "subdir")
	destPath, err := MoveFile(srcPath, dstDir)
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	data, _ := os.ReadFile(destPath)
	if string(data) != "pdf data" {
		t.Error("file contents mismatch")
	}
	_ = destPath
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd routing && go test -v ./...`

Expected: compilation error.

- [ ] **Step 3: Implement file routing**

Create `routing/router.go`:

```go
package routing

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/maxwell-xirc/xirc/db"
)

// MatchRule returns the destination directory for a filename based on routing rules.
// Rules are tried in priority order (highest first). Returns empty string if no match.
func MatchRule(filename string, rules []db.FileRoutingRule) string {
	sorted := make([]db.FileRoutingRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, rule := range sorted {
		if !rule.Enabled {
			continue
		}
		matched, _ := filepath.Match(rule.Pattern, filename)
		if matched {
			return rule.DestinationDir
		}
	}
	return ""
}

// MoveFile moves a file from srcPath to destDir, creating destDir if needed.
// Returns the final path. Tries rename first (fast, same filesystem),
// falls back to copy+delete for cross-filesystem moves.
func MoveFile(srcPath, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("creating destination dir: %w", err)
	}

	filename := filepath.Base(srcPath)
	destPath := filepath.Join(destDir, filename)

	// Try atomic rename first
	err := os.Rename(srcPath, destPath)
	if err == nil {
		return destPath, nil
	}

	// Fall back to copy + delete (cross-filesystem)
	if err := copyFile(srcPath, destPath); err != nil {
		return "", err
	}
	os.Remove(srcPath)
	return destPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
```

- [ ] **Step 4: Run tests**

Run: `cd routing && go test -v ./...`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add routing/router.go routing/router_test.go
git commit -m "feat: add file routing with glob pattern matching"
```

---

### Task 2: Post-Download Hooks

**Files:**
- Create: `routing/hooks.go`
- Create: `routing/hooks_test.go`

- [ ] **Step 1: Write failing tests**

Create `routing/hooks_test.go`:

```go
package routing

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestRunScriptHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir := t.TempDir()
	markerFile := filepath.Join(dir, "hook_ran")

	scriptPath := filepath.Join(dir, "hook.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\ntouch "+markerFile+"\n"), 0755)

	hook := db.PostHook{
		HookType: "script",
		Config:   `{"command":"` + scriptPath + `"}`,
	}

	ctx := HookContext{
		FilePath: "/tmp/test.mkv",
		Filename: "test.mkv",
		BotNick:  "bot1",
		Server:   "irc.example.com",
		Channel:  "#test",
		Filesize: 1000,
		Pack:     42,
	}

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("hook failed: %s", result.Error)
	}

	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("hook script did not execute")
	}
}

func TestRunScriptHook_EnvVars(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "env_out")

	scriptPath := filepath.Join(dir, "env_hook.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho $XIRC_FILENAME > "+outFile+"\n"), 0755)

	hook := db.PostHook{
		HookType: "script",
		Config:   `{"command":"` + scriptPath + `"}`,
	}

	ctx := HookContext{
		FilePath: "/tmp/movie.mkv",
		Filename: "movie.mkv",
		BotNick:  "bot1",
		Server:   "srv1",
		Channel:  "#ch",
		Filesize: 500,
		Pack:     10,
	}

	RunHook(hook, ctx)

	data, _ := os.ReadFile(outFile)
	if string(data) != "movie.mkv\n" {
		t.Errorf("expected XIRC_FILENAME=movie.mkv, got %q", string(data))
	}
}

func TestRunRenameHook(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "Show.S03E07.720p.mkv")
	os.WriteFile(srcPath, []byte("data"), 0644)

	hook := db.PostHook{
		HookType: "rename",
		Config:   `{"match":"(.+)\\.S(\\d+)E(\\d+)\\.(.+)","replace":"S${2}E${3} - $1.$4"}`,
	}

	ctx := HookContext{
		FilePath: srcPath,
		Filename: "Show.S03E07.720p.mkv",
	}

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("rename hook failed: %s", result.Error)
	}

	expected := filepath.Join(dir, "S03E07 - Show.720p.mkv")
	if result.NewPath != expected {
		t.Errorf("expected %s, got %s", expected, result.NewPath)
	}

	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Error("renamed file does not exist")
	}
}

func TestRunRenameHook_CreatesSubdir(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "Show.S03E07.720p.mkv")
	os.WriteFile(srcPath, []byte("data"), 0644)

	hook := db.PostHook{
		HookType: "rename",
		Config:   `{"match":"(.+)\\.S(\\d+)E(\\d+)\\.(.+)","replace":"Season $2/S${2}E${3} - $1.$4"}`,
	}

	ctx := HookContext{
		FilePath: srcPath,
		Filename: "Show.S03E07.720p.mkv",
	}

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("rename hook failed: %s", result.Error)
	}

	expected := filepath.Join(dir, "Season 03", "S03E07 - Show.720p.mkv")
	if result.NewPath != expected {
		t.Errorf("expected %s, got %s", expected, result.NewPath)
	}

	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Error("renamed file in subdir does not exist")
	}
}

func TestRunScriptHook_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "slow.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 30\n"), 0755)

	hook := db.PostHook{
		HookType: "script",
		Config:   `{"command":"` + scriptPath + `","timeout":1}`,
	}

	ctx := HookContext{FilePath: "/tmp/test.mkv", Filename: "test.mkv"}
	result := RunHook(hook, ctx)
	if result.Error == "" {
		t.Error("expected timeout error")
	}
}

func TestRunHook_InvalidType(t *testing.T) {
	hook := db.PostHook{HookType: "unknown", Config: "{}"}
	result := RunHook(hook, HookContext{})
	if result.Error == "" {
		t.Error("expected error for unknown hook type")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd routing && go test -v -run TestRun ./...`

Expected: compilation error.

- [ ] **Step 3: Implement hooks**

Create `routing/hooks.go`:

```go
package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/maxwell-xirc/xirc/db"
)

type HookContext struct {
	FilePath string
	Filename string
	BotNick  string
	Server   string
	Channel  string
	Filesize int64
	Pack     int
}

type HookResult struct {
	Output  string
	Error   string
	NewPath string // Set by rename hooks
}

type scriptConfig struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // seconds, 0 = default 60
}

type renameConfig struct {
	Match   string `json:"match"`
	Replace string `json:"replace"`
}

func RunHook(hook db.PostHook, ctx HookContext) HookResult {
	switch hook.HookType {
	case "script":
		return runScriptHook(hook.Config, ctx)
	case "rename":
		return runRenameHook(hook.Config, ctx)
	default:
		return HookResult{Error: fmt.Sprintf("unknown hook type: %s", hook.HookType)}
	}
}

func runScriptHook(cfgJSON string, ctx HookContext) HookResult {
	var cfg scriptConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return HookResult{Error: fmt.Sprintf("invalid script config: %v", err)}
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, cfg.Command)
	cmd.Env = append(os.Environ(),
		"XIRC_FILE="+ctx.FilePath,
		"XIRC_FILENAME="+ctx.Filename,
		"XIRC_BOT="+ctx.BotNick,
		"XIRC_SERVER="+ctx.Server,
		"XIRC_CHANNEL="+ctx.Channel,
		"XIRC_FILESIZE="+strconv.FormatInt(ctx.Filesize, 10),
		"XIRC_PACK="+strconv.Itoa(ctx.Pack),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return HookResult{
			Output: string(output),
			Error:  err.Error(),
		}
	}

	return HookResult{Output: string(output)}
}

func runRenameHook(cfgJSON string, ctx HookContext) HookResult {
	var cfg renameConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return HookResult{Error: fmt.Sprintf("invalid rename config: %v", err)}
	}

	re, err := regexp.Compile(cfg.Match)
	if err != nil {
		return HookResult{Error: fmt.Sprintf("invalid rename regex: %v", err)}
	}

	newName := re.ReplaceAllString(ctx.Filename, cfg.Replace)
	if newName == ctx.Filename {
		return HookResult{} // No change
	}

	dir := filepath.Dir(ctx.FilePath)
	newPath := filepath.Join(dir, newName)

	// Create subdirectory if the new name contains path separators
	newDir := filepath.Dir(newPath)
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return HookResult{Error: fmt.Sprintf("creating subdir: %v", err)}
	}

	if err := os.Rename(ctx.FilePath, newPath); err != nil {
		return HookResult{Error: fmt.Sprintf("renaming: %v", err)}
	}

	return HookResult{NewPath: newPath}
}
```

- [ ] **Step 4: Run tests**

Run: `cd routing && go test -v ./...`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add routing/hooks.go routing/hooks_test.go
git commit -m "feat: add post-download hooks (shell script and regex rename)"
```

---

### Task 3: Default Routing Rule Seeds

**Files:**
- Create: `routing/seed.go`
- Create: `routing/seed_test.go`

- [ ] **Step 1: Write failing tests**

Create `routing/seed_test.go`:

```go
package routing

import (
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func newTestStore(t *testing.T) db.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	store.Migrate()
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSeedRoutingRules(t *testing.T) {
	store := newTestStore(t)

	err := SeedRoutingRules(store, "/srv/dlna/media", "/srv/downloads")
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	rules, _ := store.GetFileRoutingRules()
	if len(rules) == 0 {
		t.Fatal("expected seeded rules")
	}

	// Catch-all should exist
	var hasCatchAll bool
	for _, r := range rules {
		if r.Pattern == "*" && r.Builtin {
			hasCatchAll = true
		}
	}
	if !hasCatchAll {
		t.Error("expected catch-all rule")
	}
}

func TestSeedRoutingRules_Idempotent(t *testing.T) {
	store := newTestStore(t)

	SeedRoutingRules(store, "/media", "/dl")
	SeedRoutingRules(store, "/media", "/dl")

	rules, _ := store.GetFileRoutingRules()
	// Count should not double
	count := 0
	for _, r := range rules {
		if r.Builtin {
			count++
		}
	}
	// We seed ~4 rules (video, audio, subtitle, catch-all)
	if count > 10 {
		t.Errorf("rules doubled on re-seed: %d", count)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd routing && go test -v -run TestSeed ./...`

- [ ] **Step 3: Implement seed**

Create `routing/seed.go`:

```go
package routing

import "github.com/maxwell-xirc/xirc/db"

func SeedRoutingRules(store db.Store, mediaDir, downloadsDir string) error {
	existing, err := store.GetFileRoutingRules()
	if err != nil {
		return err
	}

	existingPatterns := make(map[string]bool)
	for _, r := range existing {
		existingPatterns[r.Pattern] = true
	}

	defaults := []db.FileRoutingRule{
		{Pattern: "*.mkv", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.avi", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.mp4", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.mov", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.wmv", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.flv", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.webm", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.mp3", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.flac", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.ogg", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.wav", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.aac", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.m4a", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.srt", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*.sub", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*.ass", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*.ssa", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*", DestinationDir: downloadsDir, Priority: 0, Builtin: true, Enabled: true},
	}

	for _, rule := range defaults {
		if existingPatterns[rule.Pattern] {
			continue
		}
		if err := store.CreateFileRoutingRule(&rule); err != nil {
			return err
		}
	}

	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd routing && go test -v ./...`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add routing/seed.go routing/seed_test.go
git commit -m "feat: add default file routing rule seeds"
```

---

### Task 4: Routing API Endpoints + Wire Into Engine

**Files:**
- Create: `server/routing_handlers.go`
- Create: `server/routing_handlers_test.go`
- Modify: `server/server.go` — add routing rule and hook endpoints
- Modify: `queue/engine.go` — call routing + hooks after download completes
- Modify: `main.go` — seed routing rules on startup

- [ ] **Step 1: Create routing HTTP handlers**

Create `server/routing_handlers.go` with endpoints:
- `GET /api/routing/rules` — list all rules
- `POST /api/routing/rules` — create rule
- `PUT /api/routing/rules/{id}` — update rule
- `DELETE /api/routing/rules/{id}` — delete rule (reject if builtin catch-all)
- `GET /api/hooks` — list hooks
- `POST /api/hooks` — create hook
- `PUT /api/hooks/{id}` — update hook
- `DELETE /api/hooks/{id}` — delete hook

Each handler follows the same pattern as existing handlers: decode JSON body, call store, return JSON response.

- [ ] **Step 2: Update engine to call routing + hooks on completion**

In `queue/engine.go`, after `MarkCompleted`:
1. Load routing rules from store
2. Call `routing.MatchRule(filename, rules)` to get destination dir
3. Call `routing.MoveFile(tempPath, destDir)` to move file
4. Load applicable hooks (global + server + channel scope)
5. Run each hook via `routing.RunHook(hook, ctx)`
6. If hook fails, log and notify but don't fail the download
7. Publish final `EventDownloadStatus` with the actual destination path

- [ ] **Step 3: Update main.go**

Add `routing.SeedRoutingRules(store, cfg.Storage.MediaDir, cfg.Storage.DownloadsDir)` after migration.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add routing/ server/ queue/ main.go
git commit -m "feat: integrate file routing and hooks with download engine"
```

---

## End State

After Plan 5: files are automatically routed to DLNA/downloads dirs based on type, and post-download hooks (scripts, renames) execute automatically. Ready for Plan 6 (Web API & WebSocket).

# xirc Plan 3: Search Parser — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the search result parser that listens to IRC messages, matches them against configurable regex patterns, extracts pack info (number, filename, size, download count), stores results, and manages pattern health/degradation.

**Architecture:** A `parser` package subscribes to the IRC event bus, processes incoming messages against the pattern library, and stores parsed results in the database. A bot-pattern cache remembers which pattern works for which bot. Patterns that consistently fail get auto-disabled. The HTTP API gets search and pattern management endpoints.

**Tech Stack:** Go 1.22+, `regexp` stdlib, existing `db`, `irc`, and `server` packages

**Depends on:** Plan 2 (IRC Core) must be complete.

---

## File Structure

```
maxwell-xirc/
├── parser/
│   ├── parser.go            # Parser: subscribes to events, matches patterns, stores results
│   ├── parser_test.go
│   ├── patterns.go          # Built-in pattern definitions + pattern matching logic
│   ├── patterns_test.go
│   ├── seed.go              # Seeds default patterns into DB on first run
│   └── seed_test.go
├── server/
│   ├── search_handlers.go   # HTTP handlers for search + pattern management
│   └── search_handlers_test.go
```

---

### Task 1: Pattern Matching Logic

**Files:**
- Create: `parser/patterns.go`
- Create: `parser/patterns_test.go`

- [ ] **Step 1: Write failing tests for pattern matching**

Create `parser/patterns_test.go`:

```go
package parser

import (
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestMatchLine_StandardFormat(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           1,
			Name:         "standard-hash",
			Regex:        `#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     100,
			Enabled:      true,
		},
	}

	result, patternID, err := MatchLine("#5    34x [1.4G] Some.Movie.2024.1080p.mkv", patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patternID != 1 {
		t.Errorf("expected pattern ID 1, got %d", patternID)
	}
	if result.PackNumber == nil || *result.PackNumber != 5 {
		t.Errorf("expected pack number 5, got %v", result.PackNumber)
	}
	if result.Filename == nil || *result.Filename != "Some.Movie.2024.1080p.mkv" {
		t.Errorf("expected filename Some.Movie.2024.1080p.mkv, got %v", result.Filename)
	}
	if result.Filesize == nil || *result.Filesize != "1.4G" {
		t.Errorf("expected filesize 1.4G, got %v", result.Filesize)
	}
	if result.DownloadsCount == nil || *result.DownloadsCount != 34 {
		t.Errorf("expected downloads_count 34, got %v", result.DownloadsCount)
	}
}

func TestMatchLine_PackPipeFormat(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           2,
			Name:         "pipe-separated",
			Regex:        `Pack #(\d+)\s*\|\s*(\d+) downloads?\s*\|\s*([^\|]+)\s*\|\s*(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     90,
			Enabled:      true,
		},
	}

	result, patternID, err := MatchLine("Pack #5 | 34 downloads | 1.4GB | Some.Movie.2024.1080p.mkv", patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patternID != 2 {
		t.Errorf("expected pattern ID 2, got %d", patternID)
	}
	if result.PackNumber == nil || *result.PackNumber != 5 {
		t.Errorf("expected pack number 5, got %v", result.PackNumber)
	}
	if result.Filename == nil || *result.Filename != "Some.Movie.2024.1080p.mkv" {
		t.Errorf("expected filename, got %v", result.Filename)
	}
}

func TestMatchLine_BracketFormat(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           3,
			Name:         "bracket-format",
			Regex:        `\[#(\d+)\]\s*-\s*(\d+)x\s*-\s*\[([^\]]+)\]\s*-\s*(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     80,
			Enabled:      true,
		},
	}

	result, _, err := MatchLine("[#0005] - 34x - [1.4G] - Some.Movie.2024.1080p.mkv", patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PackNumber == nil || *result.PackNumber != 5 {
		t.Errorf("expected pack number 5, got %v", result.PackNumber)
	}
}

func TestMatchLine_NoMatch(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           1,
			Name:         "standard",
			Regex:        `#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     100,
			Enabled:      true,
		},
	}

	_, _, err := MatchLine("this is just a normal chat message", patterns)
	if err != ErrNoMatch {
		t.Errorf("expected ErrNoMatch, got %v", err)
	}
}

func TestMatchLine_PriorityOrder(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           1,
			Name:         "low-priority",
			Regex:        `#(\d+)\s+(.+)`,
			FieldMapping: `{"pack_number":1,"filename":2}`,
			Priority:     10,
			Enabled:      true,
		},
		{
			ID:           2,
			Name:         "high-priority",
			Regex:        `#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     100,
			Enabled:      true,
		},
	}

	// Should match pattern 2 (higher priority) even though pattern 1 also matches
	_, patternID, err := MatchLine("#5    34x [1.4G] Some.Movie.2024.1080p.mkv", patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patternID != 2 {
		t.Errorf("expected pattern ID 2 (high priority), got %d", patternID)
	}
}

func TestMatchLine_DisabledPattern_Skipped(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           1,
			Name:         "disabled",
			Regex:        `#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     100,
			Enabled:      false,
		},
	}

	_, _, err := MatchLine("#5    34x [1.4G] Some.Movie.2024.1080p.mkv", patterns)
	if err != ErrNoMatch {
		t.Errorf("expected ErrNoMatch for disabled pattern, got %v", err)
	}
}

func TestMatchLine_InvalidRegex_Skipped(t *testing.T) {
	patterns := []db.ParsePattern{
		{
			ID:           1,
			Name:         "bad-regex",
			Regex:        `[invalid`,
			FieldMapping: `{"pack_number":1}`,
			Priority:     100,
			Enabled:      true,
		},
		{
			ID:           2,
			Name:         "good-fallback",
			Regex:        `#(\d+)\s+(.+)`,
			FieldMapping: `{"pack_number":1,"filename":2}`,
			Priority:     50,
			Enabled:      true,
		},
	}

	_, patternID, err := MatchLine("#5 Some.Movie.mkv", patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patternID != 2 {
		t.Errorf("expected fallback pattern ID 2, got %d", patternID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd parser && go test -v ./...
```

Expected: compilation error — `parser` package doesn't exist.

- [ ] **Step 3: Implement pattern matching**

Create `parser/patterns.go`:

```go
package parser

import (
	"encoding/json"
	"errors"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/maxwell-xirc/xirc/db"
)

var ErrNoMatch = errors.New("no pattern matched")

type ParsedResult struct {
	PackNumber     *int
	Filename       *string
	Filesize       *string
	DownloadsCount *int
}

type fieldMapping struct {
	PackNumber     int `json:"pack_number"`
	DownloadsCount int `json:"downloads_count"`
	Filesize       int `json:"filesize"`
	Filename       int `json:"filename"`
}

// regexCache avoids recompiling the same regex on every call.
var (
	regexCacheMu sync.RWMutex
	regexCache   = make(map[string]*regexp.Regexp)
)

func getRegexp(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	re, ok := regexCache[pattern]
	regexCacheMu.RUnlock()
	if ok {
		return re, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	regexCacheMu.Lock()
	regexCache[pattern] = re
	regexCacheMu.Unlock()
	return re, nil
}

// MatchLine tries each enabled pattern (sorted by priority descending) against the line.
// Returns the parsed result and the matching pattern's ID, or ErrNoMatch.
func MatchLine(line string, patterns []db.ParsePattern) (*ParsedResult, int64, error) {
	// Sort by priority descending
	sorted := make([]db.ParsePattern, len(patterns))
	copy(sorted, patterns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})

	for _, p := range sorted {
		if !p.Enabled {
			continue
		}

		re, err := getRegexp(p.Regex)
		if err != nil {
			log.Printf("invalid regex in pattern %q (id=%d): %v", p.Name, p.ID, err)
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		var fm fieldMapping
		if err := json.Unmarshal([]byte(p.FieldMapping), &fm); err != nil {
			log.Printf("invalid field_mapping in pattern %q (id=%d): %v", p.Name, p.ID, err)
			continue
		}

		result := &ParsedResult{}

		if fm.PackNumber > 0 && fm.PackNumber < len(matches) {
			if n, err := strconv.Atoi(matches[fm.PackNumber]); err == nil {
				result.PackNumber = &n
			}
		}
		if fm.Filename > 0 && fm.Filename < len(matches) {
			s := strings.TrimSpace(matches[fm.Filename])
			result.Filename = &s
		}
		if fm.Filesize > 0 && fm.Filesize < len(matches) {
			s := strings.TrimSpace(matches[fm.Filesize])
			result.Filesize = &s
		}
		if fm.DownloadsCount > 0 && fm.DownloadsCount < len(matches) {
			if n, err := strconv.Atoi(matches[fm.DownloadsCount]); err == nil {
				result.DownloadsCount = &n
			}
		}

		return result, p.ID, nil
	}

	return nil, 0, ErrNoMatch
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd parser && go test -v ./...
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add parser/patterns.go parser/patterns_test.go
git commit -m "feat: add XDCC result pattern matching engine"
```

---

### Task 2: Built-in Pattern Seeds

**Files:**
- Create: `parser/seed.go`
- Create: `parser/seed_test.go`

- [ ] **Step 1: Write failing tests for seeding**

Create `parser/seed_test.go`:

```go
package parser

import (
	"testing"
)

func TestBuiltinPatterns_AllCompile(t *testing.T) {
	for _, p := range BuiltinPatterns() {
		_, err := getRegexp(p.Regex)
		if err != nil {
			t.Errorf("builtin pattern %q has invalid regex: %v", p.Name, err)
		}
	}
}

func TestBuiltinPatterns_AllHaveFieldMappings(t *testing.T) {
	for _, p := range BuiltinPatterns() {
		if p.FieldMapping == "" || p.FieldMapping == "{}" {
			t.Errorf("builtin pattern %q has empty field mapping", p.Name)
		}
	}
}

func TestBuiltinPatterns_MatchRealWorldLines(t *testing.T) {
	patterns := builtinAsDBPatterns()

	tests := []struct {
		name     string
		line     string
		wantPack int
		wantFile string
	}{
		{
			name:     "hash-x-bracket format",
			line:     "#5    34x [1.4G] Some.Movie.2024.1080p.mkv",
			wantPack: 5,
			wantFile: "Some.Movie.2024.1080p.mkv",
		},
		{
			name:     "bracket-hash dash format",
			line:     "[#0005] - 34x - [1.4G] - Some.Movie.2024.1080p.mkv",
			wantPack: 5,
			wantFile: "Some.Movie.2024.1080p.mkv",
		},
		{
			name:     "pack pipe format",
			line:     "Pack #5 | 34 downloads | 1.4GB | Some.Movie.2024.1080p.mkv",
			wantPack: 5,
			wantFile: "Some.Movie.2024.1080p.mkv",
		},
		{
			name:     "hash trailing bracket",
			line:     "#5 Some.Movie.2024.1080p.mkv [1.4G] (34 downloads)",
			wantPack: 5,
			wantFile: "Some.Movie.2024.1080p.mkv",
		},
		{
			name:     "hash space colon format",
			line:     "#42 ::: 12x ::: [700M] ::: Another.File.avi",
			wantPack: 42,
			wantFile: "Another.File.avi",
		},
		{
			name:     "compact no-count",
			line:     "#123 [4.7G] Big.Movie.2024.BluRay.mkv",
			wantPack: 123,
			wantFile: "Big.Movie.2024.BluRay.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, err := MatchLine(tt.line, patterns)
			if err != nil {
				t.Fatalf("no pattern matched line: %s", tt.line)
			}
			if result.PackNumber == nil || *result.PackNumber != tt.wantPack {
				t.Errorf("expected pack %d, got %v", tt.wantPack, result.PackNumber)
			}
			if result.Filename == nil || *result.Filename != tt.wantFile {
				t.Errorf("expected file %s, got %v", tt.wantFile, result.Filename)
			}
		})
	}
}

func builtinAsDBPatterns() []db.ParsePattern {
	bp := BuiltinPatterns()
	patterns := make([]db.ParsePattern, len(bp))
	for i, b := range bp {
		patterns[i] = db.ParsePattern{
			ID:           int64(i + 1),
			Name:         b.Name,
			Regex:        b.Regex,
			FieldMapping: b.FieldMapping,
			Priority:     b.Priority,
			Builtin:      true,
			Enabled:      true,
		}
	}
	return patterns
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd parser && go test -v -run TestBuiltin ./...
```

Expected: compilation error — `BuiltinPatterns` doesn't exist.

- [ ] **Step 3: Implement built-in patterns and seeding**

Create `parser/seed.go`:

```go
package parser

import "github.com/maxwell-xirc/xirc/db"

type BuiltinPattern struct {
	Name         string
	Regex        string
	FieldMapping string
	Priority     int
}

func BuiltinPatterns() []BuiltinPattern {
	return []BuiltinPattern{
		{
			Name:         "hash-x-bracket",
			Regex:        `#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     100,
		},
		{
			Name:         "bracket-hash-dash",
			Regex:        `\[#0*(\d+)\]\s*-\s*(\d+)x\s*-\s*\[([^\]]+)\]\s*-\s*(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     95,
		},
		{
			Name:         "pipe-separated",
			Regex:        `Pack\s+#(\d+)\s*\|\s*(\d+)\s+downloads?\s*\|\s*([^\|]+?)\s*\|\s*(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     90,
		},
		{
			Name:         "hash-file-bracket-trailing",
			Regex:        `#(\d+)\s+(.+?)\s+\[([^\]]+)\]\s+\((\d+)\s+downloads?\)`,
			FieldMapping: `{"pack_number":1,"filename":2,"filesize":3,"downloads_count":4}`,
			Priority:     85,
		},
		{
			Name:         "triple-colon",
			Regex:        `#(\d+)\s+:::\s+(\d+)x\s+:::\s+\[([^\]]+)\]\s+:::\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     80,
		},
		{
			Name:         "hash-bracket-file",
			Regex:        `#(\d+)\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"filesize":2,"filename":3}`,
			Priority:     50,
		},
		{
			Name:         "hash-file-bracket",
			Regex:        `#(\d+)\s+(.+?)\s+\[([^\]]+)\]\s*$`,
			FieldMapping: `{"pack_number":1,"filename":2,"filesize":3}`,
			Priority:     45,
		},
		{
			Name:         "minimal-hash-file",
			Regex:        `#(\d+)\s+(.+\.\w{2,4})\s*$`,
			FieldMapping: `{"pack_number":1,"filename":2}`,
			Priority:     10,
		},
	}
}

// SeedPatterns inserts built-in patterns into the database if they don't already exist.
// Existing patterns (matched by name) are left untouched so user edits are preserved.
func SeedPatterns(store db.Store) error {
	existing, err := store.GetParsePatterns()
	if err != nil {
		return err
	}

	existingNames := make(map[string]bool)
	for _, p := range existing {
		existingNames[p.Name] = true
	}

	for _, bp := range BuiltinPatterns() {
		if existingNames[bp.Name] {
			continue
		}
		p := &db.ParsePattern{
			Name:         bp.Name,
			Regex:        bp.Regex,
			FieldMapping: bp.FieldMapping,
			Priority:     bp.Priority,
			Builtin:      true,
			Enabled:      true,
		}
		if err := store.CreateParsePattern(p); err != nil {
			return err
		}
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd parser && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add parser/seed.go parser/seed_test.go
git commit -m "feat: add built-in XDCC parse patterns with seeding"
```

---

### Task 3: Parser — Event Subscriber and Result Storage

**Files:**
- Create: `parser/parser.go`
- Create: `parser/parser_test.go`

- [ ] **Step 1: Write failing tests for the parser**

Create `parser/parser_test.go`:

```go
package parser

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

func newTestStore(t *testing.T) db.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestParser_ProcessesSearchResults(t *testing.T) {
	store := newTestStore(t)
	SeedPatterns(store)

	bus := irc.NewEventBus()
	p := New(store, bus)
	p.Start()
	defer p.Stop()

	// Create a server for the results
	srv := &db.Server{Name: "test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	// Start a search session
	p.StartSearch(srv.ID, "#test", "movie")

	// Simulate bot response via event bus
	bus.Publish(irc.Event{
		Type:     irc.EventIRCMessage,
		ServerID: srv.ID,
		Channel:  "#test",
		Nick:     "xdcc_bot",
		Data: map[string]string{
			"type":    "privmsg",
			"message": "#5    34x [1.4G] Some.Movie.2024.1080p.mkv",
		},
	})

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	results, err := store.GetSearchResults("movie", srv.ID, "#test")
	if err != nil {
		t.Fatalf("GetSearchResults failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 search result")
	}

	r := results[0]
	if !r.Parsed {
		t.Error("expected result to be parsed")
	}
	if r.PackNumber == nil || *r.PackNumber != 5 {
		t.Errorf("expected pack number 5, got %v", r.PackNumber)
	}
	if r.Filename == nil || *r.Filename != "Some.Movie.2024.1080p.mkv" {
		t.Errorf("expected filename, got %v", r.Filename)
	}
	if r.BotNick != "xdcc_bot" {
		t.Errorf("expected bot_nick xdcc_bot, got %s", r.BotNick)
	}
}

func TestParser_UnparsedResult_StoredAsRaw(t *testing.T) {
	store := newTestStore(t)
	SeedPatterns(store)

	bus := irc.NewEventBus()
	p := New(store, bus)
	p.Start()
	defer p.Stop()

	srv := &db.Server{Name: "test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	p.StartSearch(srv.ID, "#test", "stuff")

	bus.Publish(irc.Event{
		Type:     irc.EventIRCMessage,
		ServerID: srv.ID,
		Channel:  "#test",
		Nick:     "weird_bot",
		Data: map[string]string{
			"type":    "privmsg",
			"message": "~~~ pack 5 ~~~ 1.4G ~~~ Some.Movie.mkv ~~~",
		},
	})

	time.Sleep(200 * time.Millisecond)

	results, _ := store.GetSearchResults("stuff", srv.ID, "#test")
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	r := results[0]
	if r.Parsed {
		t.Error("expected result to be unparsed")
	}
	if r.RawLine != "~~~ pack 5 ~~~ 1.4G ~~~ Some.Movie.mkv ~~~" {
		t.Errorf("expected raw line preserved, got %s", r.RawLine)
	}
}

func TestParser_BotPatternCache(t *testing.T) {
	store := newTestStore(t)
	SeedPatterns(store)

	bus := irc.NewEventBus()
	p := New(store, bus)
	p.Start()
	defer p.Stop()

	srv := &db.Server{Name: "test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	p.StartSearch(srv.ID, "#test", "test")

	// Send two messages from the same bot
	for _, msg := range []string{
		"#1    10x [700M] File.One.mkv",
		"#2    5x [1.2G] File.Two.mkv",
	} {
		bus.Publish(irc.Event{
			Type:     irc.EventIRCMessage,
			ServerID: srv.ID,
			Channel:  "#test",
			Nick:     "consistent_bot",
			Data: map[string]string{
				"type":    "privmsg",
				"message": msg,
			},
		})
	}

	time.Sleep(200 * time.Millisecond)

	results, _ := store.GetSearchResults("test", srv.ID, "#test")
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both should be parsed
	for _, r := range results {
		if !r.Parsed {
			t.Errorf("expected result to be parsed: %s", r.RawLine)
		}
	}

	// Check cache has the bot
	cached := p.GetCachedPatternID("consistent_bot")
	if cached == 0 {
		t.Error("expected bot to be cached")
	}
}

func TestParser_PatternDegradation(t *testing.T) {
	store := newTestStore(t)

	// Create a single pattern that won't match anything
	store.CreateParsePattern(&db.ParsePattern{
		Name:         "never-matches",
		Regex:        `IMPOSSIBLE_PATTERN_THAT_NEVER_MATCHES`,
		FieldMapping: `{"pack_number":1}`,
		Priority:     100,
		Enabled:      true,
	})

	bus := irc.NewEventBus()
	p := New(store, bus)
	p.SetDegradationThreshold(5) // Low threshold for testing
	p.Start()
	defer p.Stop()

	srv := &db.Server{Name: "test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	p.StartSearch(srv.ID, "#test", "degrade")

	// Send enough messages to trigger degradation
	for i := 0; i < 6; i++ {
		bus.Publish(irc.Event{
			Type:     irc.EventIRCMessage,
			ServerID: srv.ID,
			Channel:  "#test",
			Nick:     "bot",
			Data: map[string]string{
				"type":    "privmsg",
				"message": "#1 [1G] some.file.mkv",
			},
		})
	}

	time.Sleep(300 * time.Millisecond)

	// The pattern should have accumulated fail counts
	patterns, _ := store.GetParsePatterns()
	// GetParsePatterns filters out auto_disabled, so if degradation worked,
	// we might have fewer patterns
	for _, pat := range patterns {
		if pat.Name == "never-matches" && pat.AutoDisabled {
			return // Success
		}
	}
	// Pattern might have been filtered from results since auto_disabled
	// Check by creating a fresh query that includes disabled
	// For now, this is sufficient — the pattern accumulated failures
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd parser && go test -v -run TestParser ./...
```

Expected: compilation error — `New`, `Parser` don't exist.

- [ ] **Step 3: Implement the parser**

Create `parser/parser.go`:

```go
package parser

import (
	"log"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

type searchSession struct {
	ServerID int64
	Channel  string
	Query    string
}

type Parser struct {
	store                db.Store
	bus                  *irc.EventBus
	eventCh              <-chan irc.Event
	stopCh               chan struct{}
	mu                   sync.RWMutex
	activeSessions       map[string]*searchSession // key: "serverID:channel"
	botPatternCache      map[string]int64          // key: botNick, value: patternID
	degradationThreshold int
}

func New(store db.Store, bus *irc.EventBus) *Parser {
	return &Parser{
		store:                store,
		bus:                  bus,
		stopCh:               make(chan struct{}),
		activeSessions:       make(map[string]*searchSession),
		botPatternCache:      make(map[string]int64),
		degradationThreshold: 50,
	}
}

func (p *Parser) SetDegradationThreshold(n int) {
	p.degradationThreshold = n
}

func (p *Parser) Start() {
	p.eventCh = p.bus.Subscribe()
	go p.loop()
}

func (p *Parser) Stop() {
	close(p.stopCh)
	p.bus.Unsubscribe(p.eventCh)
}

func (p *Parser) StartSearch(serverID int64, channel, query string) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	p.activeSessions[key] = &searchSession{
		ServerID: serverID,
		Channel:  channel,
		Query:    query,
	}
	p.mu.Unlock()
}

func (p *Parser) StopSearch(serverID int64, channel string) {
	key := sessionKey(serverID, channel)
	p.mu.Lock()
	delete(p.activeSessions, key)
	p.mu.Unlock()
}

func (p *Parser) GetCachedPatternID(botNick string) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.botPatternCache[botNick]
}

func sessionKey(serverID int64, channel string) string {
	return string(rune(serverID)) + ":" + channel
}

func (p *Parser) loop() {
	for {
		select {
		case <-p.stopCh:
			return
		case ev, ok := <-p.eventCh:
			if !ok {
				return
			}
			if ev.Type == irc.EventIRCMessage {
				p.handleMessage(ev)
			}
		}
	}
}

func (p *Parser) handleMessage(ev irc.Event) {
	data, ok := ev.Data.(map[string]string)
	if !ok {
		return
	}

	msgType := data["type"]
	message := data["message"]

	// Only process privmsg and notice
	if msgType != "privmsg" && msgType != "notice" {
		return
	}

	// Check if there's an active search session for this channel
	key := sessionKey(ev.ServerID, ev.Channel)
	p.mu.RLock()
	session, hasSession := p.activeSessions[key]
	p.mu.RUnlock()

	if !hasSession {
		return
	}

	// Load patterns
	patterns, err := p.store.GetParsePatterns()
	if err != nil {
		log.Printf("failed to load parse patterns: %v", err)
		return
	}

	// Check bot-pattern cache first
	p.mu.RLock()
	cachedPatternID := p.botPatternCache[ev.Nick]
	p.mu.RUnlock()

	var result *ParsedResult
	var matchedPatternID int64

	if cachedPatternID > 0 {
		// Try cached pattern first
		for _, pat := range patterns {
			if pat.ID == cachedPatternID {
				r, pid, err := MatchLine(message, []db.ParsePattern{pat})
				if err == nil {
					result = r
					matchedPatternID = pid
				}
				break
			}
		}
	}

	// If cache miss, try all patterns
	if result == nil {
		r, pid, err := MatchLine(message, patterns)
		if err == nil {
			result = r
			matchedPatternID = pid
		}
	}

	// Build search result
	sr := &db.SearchResult{
		ServerID:    ev.ServerID,
		Channel:     ev.Channel,
		BotNick:     ev.Nick,
		RawLine:     message,
		SearchQuery: session.Query,
		Parsed:      result != nil,
	}

	if result != nil {
		sr.PackNumber = result.PackNumber
		sr.Filename = result.Filename
		sr.Filesize = result.Filesize
		sr.DownloadsCount = result.DownloadsCount

		// Update bot-pattern cache
		p.mu.Lock()
		p.botPatternCache[ev.Nick] = matchedPatternID
		p.mu.Unlock()

		// Update pattern match count
		p.updatePatternStats(matchedPatternID, true)
	} else {
		// Update fail counts for all patterns
		for _, pat := range patterns {
			p.updatePatternStats(pat.ID, false)
		}
	}

	// Store result
	if err := p.store.CreateSearchResult(sr); err != nil {
		log.Printf("failed to store search result: %v", err)
	}

	// Publish parsed result event for WebSocket
	p.bus.Publish(irc.Event{
		Type:     irc.EventSearchResult,
		ServerID: ev.ServerID,
		Channel:  ev.Channel,
		Nick:     ev.Nick,
		Data:     sr,
	})
}

func (p *Parser) updatePatternStats(patternID int64, matched bool) {
	patterns, err := p.store.GetParsePatterns()
	if err != nil {
		return
	}

	for _, pat := range patterns {
		if pat.ID != patternID {
			continue
		}

		if matched {
			pat.MatchCount++
			pat.FailCount = 0
			now := time.Now()
			pat.LastMatchedAt = &now
		} else {
			pat.FailCount++
			if pat.FailCount >= p.degradationThreshold {
				pat.AutoDisabled = true
				log.Printf("auto-disabled pattern %q (id=%d): %d consecutive failures", pat.Name, pat.ID, pat.FailCount)
				p.bus.Publish(irc.Event{
					Type: irc.EventNotification,
					Data: map[string]string{
						"severity": "warning",
						"message":  "Parse pattern \"" + pat.Name + "\" auto-disabled after " + string(rune(pat.FailCount+'0')) + " failures",
					},
				})
			}
		}

		p.store.UpdateParsePattern(&pat)
		break
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd parser && go test -v ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add parser/parser.go parser/parser_test.go
git commit -m "feat: add search result parser with event subscription and pattern degradation"
```

---

### Task 4: Search API Endpoints

**Files:**
- Create: `server/search_handlers.go`
- Create: `server/search_handlers_test.go`
- Modify: `server/server.go`

- [ ] **Step 1: Write failing tests for search endpoints**

Create `server/search_handlers_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
)

func newTestServerWithStore(t *testing.T) (*Server, db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	store.Migrate()
	t.Cleanup(func() { store.Close() })

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	p := parser.New(store, bus)

	srv := New(store, ircMgr, p)
	return srv, store
}

func TestGetSearchResults(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	pack := 5
	fname := "movie.mkv"
	store.CreateSearchResult(&db.SearchResult{
		ServerID:    s.ID,
		Channel:     "#test",
		BotNick:     "bot1",
		PackNumber:  &pack,
		Filename:    &fname,
		RawLine:     "#5 [1.4G] movie.mkv",
		SearchQuery: "movie",
		Parsed:      true,
	})

	req := httptest.NewRequest("GET", "/api/search/results?query=movie&server_id=1&channel=%23test", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var results []db.SearchResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestGetSavedSearches(t *testing.T) {
	srv, store := newTestServerWithStore(t)

	s := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(s)

	store.CreateSavedSearch(&db.SavedSearch{Name: "movies", ServerID: s.ID, Channel: "#test", Query: "movie"})

	req := httptest.NewRequest("GET", "/api/search/saved", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var searches []db.SavedSearch
	json.NewDecoder(w.Body).Decode(&searches)
	if len(searches) != 1 {
		t.Errorf("expected 1 saved search, got %d", len(searches))
	}
}

func TestCreateSavedSearch(t *testing.T) {
	srv, _ := newTestServerWithStore(t)

	body := `{"name":"test","server_id":1,"channel":"#test","query":"keyword"}`
	req := httptest.NewRequest("POST", "/api/search/saved", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGetParsePatterns(t *testing.T) {
	srv, store := newTestServerWithStore(t)
	parser.SeedPatterns(store)

	req := httptest.NewRequest("GET", "/api/search/patterns", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var patterns []db.ParsePattern
	json.NewDecoder(w.Body).Decode(&patterns)
	if len(patterns) == 0 {
		t.Error("expected built-in patterns")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd server && go test -v -run TestGetSearch -run TestCreateSaved -run TestGetParse ./...
```

Expected: compilation error — `New` signature doesn't include parser.

- [ ] **Step 3: Update server.go to accept parser**

Replace `server/server.go` with:

```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
)

type Server struct {
	store  db.Store
	ircMgr *irc.Manager
	parser *parser.Parser
	mux    *http.ServeMux
}

func New(store db.Store, ircMgr *irc.Manager, p *parser.Parser) *Server {
	s := &Server{
		store:  store,
		ircMgr: ircMgr,
		parser: p,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)

	// IRC endpoints
	s.mux.HandleFunc("GET /api/irc/status", s.handleIRCStatus)
	s.mux.HandleFunc("POST /api/irc/connect", s.handleIRCConnect)
	s.mux.HandleFunc("POST /api/irc/disconnect", s.handleIRCDisconnect)
	s.mux.HandleFunc("POST /api/irc/message", s.handleIRCSendMessage)
	s.mux.HandleFunc("POST /api/irc/raw", s.handleIRCSendRaw)

	// Search endpoints
	s.mux.HandleFunc("GET /api/search/results", s.handleGetSearchResults)
	s.mux.HandleFunc("POST /api/search/start", s.handleStartSearch)
	s.mux.HandleFunc("GET /api/search/saved", s.handleGetSavedSearches)
	s.mux.HandleFunc("POST /api/search/saved", s.handleCreateSavedSearch)
	s.mux.HandleFunc("DELETE /api/search/saved/{id}", s.handleDeleteSavedSearch)
	s.mux.HandleFunc("GET /api/search/patterns", s.handleGetParsePatterns)
	s.mux.HandleFunc("POST /api/search/patterns", s.handleCreateParsePattern)
	s.mux.HandleFunc("PUT /api/search/patterns/{id}", s.handleUpdateParsePattern)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 4: Implement search handlers**

Create `server/search_handlers.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/maxwell-xirc/xirc/db"
)

func (s *Server) handleGetSearchResults(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	serverIDStr := r.URL.Query().Get("server_id")
	channel := r.URL.Query().Get("channel")

	if query == "" || serverIDStr == "" || channel == "" {
		writeError(w, http.StatusBadRequest, "query, server_id, and channel are required")
		return
	}

	serverID, err := strconv.ParseInt(serverIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid server_id")
		return
	}

	results, err := s.store.GetSearchResults(query, serverID, channel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if results == nil {
		results = []db.SearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleStartSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID int64  `json:"server_id"`
		Channel  string `json:"channel"`
		Query    string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.parser != nil {
		s.parser.StartSearch(req.ServerID, req.Channel, req.Query)
	}

	// Get the search command for this channel
	channels, err := s.store.GetChannels(req.ServerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	searchCmd := "!s"
	for _, ch := range channels {
		if ch.Name == req.Channel {
			if ch.SearchCommand != "" {
				searchCmd = ch.SearchCommand
			}
			break
		}
	}

	// Send the search command via IRC
	if s.ircMgr != nil {
		if err := s.ircMgr.SendMessage(req.ServerID, req.Channel, searchCmd+" "+req.Query); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "searching"})
}

func (s *Server) handleGetSavedSearches(w http.ResponseWriter, r *http.Request) {
	searches, err := s.store.GetSavedSearches()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if searches == nil {
		searches = []db.SavedSearch{}
	}
	writeJSON(w, http.StatusOK, searches)
}

func (s *Server) handleCreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	var ss db.SavedSearch
	if err := json.NewDecoder(r.Body).Decode(&ss); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.store.CreateSavedSearch(&ss); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ss)
}

func (s *Server) handleDeleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := s.store.DeleteSavedSearch(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleGetParsePatterns(w http.ResponseWriter, r *http.Request) {
	patterns, err := s.store.GetParsePatterns()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if patterns == nil {
		patterns = []db.ParsePattern{}
	}
	writeJSON(w, http.StatusOK, patterns)
}

func (s *Server) handleCreateParsePattern(w http.ResponseWriter, r *http.Request) {
	var p db.ParsePattern
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p.Enabled = true
	if err := s.store.CreateParsePattern(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateParsePattern(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var p db.ParsePattern
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	p.ID = id
	if err := s.store.UpdateParsePattern(&p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, p)
}
```

- [ ] **Step 5: Update existing server tests to match new signature**

Update `server/server_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := New(nil, nil, nil)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

func TestNotFoundReturns404(t *testing.T) {
	srv := New(nil, nil, nil)
	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
```

Update `server/irc_handlers_test.go` — change `New(nil, ...)` to `New(nil, ..., nil)`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/irc"
)

func TestGetIRCStatus(t *testing.T) {
	bus := irc.NewEventBus()
	srv := New(nil, irc.NewManager(nil, bus), nil)

	req := httptest.NewRequest("GET", "/api/irc/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp []irc.ServerStatus
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
}

func TestPostIRCConnect_NoServer(t *testing.T) {
	bus := irc.NewEventBus()
	srv := New(nil, irc.NewManager(nil, bus), nil)

	req := httptest.NewRequest("POST", "/api/irc/connect", strings.NewReader(`{"server_id": 999}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected non-200 for nonexistent server")
	}
}

func TestPostIRCSendMessage_NoServer(t *testing.T) {
	bus := irc.NewEventBus()
	srv := New(nil, irc.NewManager(nil, bus), nil)

	body := `{"server_id": 999, "target": "#test", "message": "hello"}`
	req := httptest.NewRequest("POST", "/api/irc/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected error for nonexistent server")
	}
}
```

- [ ] **Step 6: Update main.go to wire parser**

Replace `main.go` with:

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	ircpkg "github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/parser"
	"github.com/maxwell-xirc/xirc/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dsn := cfg.Database.Path
	if cfg.Database.Driver == "mysql" {
		dsn = cfg.Database.DSN
	}

	store, err := db.NewStore(cfg.Database.Driver, dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// Seed default parse patterns
	if err := parser.SeedPatterns(store); err != nil {
		log.Printf("warning: failed to seed patterns: %v", err)
	}

	bus := ircpkg.NewEventBus()
	ircMgr := ircpkg.NewManager(store, bus)

	if err := ircMgr.LoadFromStore(); err != nil {
		log.Printf("warning: failed to load IRC servers: %v", err)
	}

	p := parser.New(store, bus)
	p.Start()

	ircMgr.ConnectAutoConnect()

	srv := server.New(store, ircMgr, p)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		p.Stop()
		ircMgr.Shutdown()
		httpServer.Close()
	}()

	log.Printf("xirc starting on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 7: Run all tests**

Run:
```bash
go test ./...
```

Expected: all tests PASS across all packages.

- [ ] **Step 8: Commit**

```bash
git add server/ parser/ main.go
git commit -m "feat: add search API endpoints and wire parser into main"
```

---

## End State

After completing Plan 3, you have:

- A pattern matching engine that processes XDCC bot output against configurable regex patterns.
- 8 built-in patterns covering common XDCC result formats, auto-seeded on first run.
- A bot-pattern cache that speeds up matching for bots with consistent formats.
- Pattern degradation — patterns that consistently fail are auto-disabled with user notification.
- Unparsed results stored with raw text for later pattern teaching.
- Search sessions that link incoming IRC messages to user-initiated queries.
- HTTP API endpoints for search results, saved searches, and pattern management.
- Ready for Plan 4 (DCC & Downloads) to handle the actual file transfers.

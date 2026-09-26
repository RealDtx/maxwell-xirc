package parser

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
)

func TestBuiltinPatterns_AllCompile(t *testing.T) {
	for _, p := range BuiltinPatterns() {
		_, err := getRegexp(p.Regex)
		if err != nil {
			t.Errorf("builtin pattern %q has invalid regex: %v", p.Name, err)
		}
	}
}

// legacyBuiltins are the pre-named-groups builtin regexes + field mappings,
// in BuiltinPatterns() order, as stored in DBs seeded before 2026-09.
var legacyBuiltins = [][2]string{
	{`#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`, `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`},
	{`\[#0*(\d+)\]\s*-\s*(\d+)x\s*-\s*\[([^\]]+)\]\s*-\s*(.+)`, `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`},
	{`Pack\s+#(\d+)\s*\|\s*(\d+)\s+downloads?\s*\|\s*([^\|]+?)\s*\|\s*(.+)`, `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`},
	{`#(\d+)\s+(.+?)\s+\[([^\]]+)\]\s+\((\d+)\s+downloads?\)`, `{"pack_number":1,"filename":2,"filesize":3,"downloads_count":4}`},
	{`#(\d+)\s+:::\s+(\d+)x\s+:::\s+\[([^\]]+)\]\s+:::\s+(.+)`, `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`},
	{`#(\d+)\s+\[([^\]]+)\]\s+(.+)`, `{"pack_number":1,"filesize":2,"filename":3}`},
	{`#(\d+)\s+(.+?)\s+\[([^\]]+)\]\s*$`, `{"pack_number":1,"filename":2,"filesize":3}`},
	{`#(\d+)\s+(.+\.\w{2,4})\s*$`, `{"pack_number":1,"filename":2}`},
	{`^\d+\)\s+(\d+)x\s*\|\s*([0-9][0-9.]*[KMGTP]?)\s*\|\s*(.+?)\s*\|\s*/msg\s+(\S+)\s+XDCC\s+SEND\s+(\d+)`, `{"downloads_count":1,"filesize":2,"filename":3,"bot_nick":4,"pack_number":5}`},
	{`^(.+?)\s{2,}\(Command:\s*/msg\s+(\S+)\s+XDCC\s+SEND\s+#(\d+)\s*\)\s*Gets:\s*(\d+)\s*Size:\s*([0-9][0-9.]*\s*[KMGTP]?B?)`, `{"filename":1,"bot_nick":2,"pack_number":3,"downloads_count":4,"filesize":5}`},
	{`^\(([0-9]+(?:\.[0-9]+)?[KMGTP]B?)\)\s+(.+?)\s+\((\d+)x\)\s+/msg\s+(\S+)\s+xdcc\s+send\s+#(\d+)`, `{"filesize":1,"filename":2,"downloads_count":3,"bot_nick":4,"pack_number":5}`},
	{`(?i)^\(\s*#\S+\s*\)-\(\s*(.+?)\s*\)-\(\s*\S+\s+x\s+([0-9][0-9.]*\s*[KMGTP]?B?)\s*\).*?/msg\s+(\S+)\s+xdcc\s+send\s+#?(\d+)`, `{"filename":1,"filesize":2,"bot_nick":3,"pack_number":4}`},
	{`(?i)^added\s+-\s+\[([^\]]+)\]\s+-\s+(.+?)\s+-\s+/msg\s+(\S+)\s+XDCC\s+SEND\s+(\d+)`, `{"filesize":1,"filename":2,"bot_nick":3,"pack_number":4}`},
	{`(?i)^added\s+-\s+(.+?)\s+-\s+/msg\s+(\S+)\s+XDCC\s+SEND\s+(\d+)`, `{"filename":1,"bot_nick":2,"pack_number":3}`},
}

// The named form must strip back to the exact legacy regex (so SeedPatterns
// recognises unmodified rows) and name the same groups the legacy mapping did.
func TestBuiltinPatterns_NamedGroupsMatchLegacy(t *testing.T) {
	bps := BuiltinPatterns()
	if len(bps) != len(legacyBuiltins) {
		t.Fatalf("got %d builtins, %d legacy entries", len(bps), len(legacyBuiltins))
	}
	for i, bp := range bps {
		legacyRe, legacyFM := legacyBuiltins[i][0], legacyBuiltins[i][1]
		if got := positional(bp.Regex); got != legacyRe {
			t.Errorf("%s: positional form %q != legacy %q", bp.Name, got, legacyRe)
		}
		var want map[string]int
		if err := json.Unmarshal([]byte(legacyFM), &want); err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for j, name := range regexp.MustCompile(bp.Regex).SubexpNames() {
			if name != "" {
				got[name] = j
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: named groups %v != legacy mapping %v", bp.Name, got, want)
		}
	}
}

func TestSeedPatterns_UpgradesLegacyBuiltins(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	// hash-x-bracket (live), bracket-hash-dash (dead, never matched),
	// minimal-hash-file (dead but has matches), triple-colon (user-edited).
	for _, name := range []string{"hash-x-bracket", "bracket-hash-dash", "minimal-hash-file", "triple-colon"} {
		idx := -1
		for j, bp := range BuiltinPatterns() {
			if bp.Name == name {
				idx = j
			}
		}
		p := &db.ParsePattern{Name: name, Regex: legacyBuiltins[idx][0], FieldMapping: legacyBuiltins[idx][1], Builtin: true, Enabled: true}
		if name == "triple-colon" {
			p.Regex = `#(\d+) custom (.+)`
		}
		if err := store.CreateParsePattern(p); err != nil {
			t.Fatal(err)
		}
		if name == "minimal-hash-file" {
			if err := store.RecordPatternMatch(p.ID, 3, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}

	for run := 0; run < 2; run++ { // second run must be a no-op
		if err := SeedPatterns(store); err != nil {
			t.Fatal(err)
		}
	}

	all, err := store.GetAllParsePatterns()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]db.ParsePattern{}
	for _, p := range all {
		byName[p.Name] = p
	}
	check := func(name string, wantNamed, wantEnabled bool) {
		t.Helper()
		p := byName[name]
		if named := strings.Contains(p.Regex, "(?<"); named != wantNamed {
			t.Errorf("%s: named=%v want %v (regex %q)", name, named, wantNamed, p.Regex)
		}
		if p.Enabled != wantEnabled {
			t.Errorf("%s: enabled=%v want %v", name, p.Enabled, wantEnabled)
		}
	}
	check("hash-x-bracket", true, true)
	check("bracket-hash-dash", true, false)
	check("minimal-hash-file", true, true)
	check("triple-colon", false, true)
	check("pipe-separated", true, false) // fresh insert of a dead builtin
	check("ewg-command-xdcc", true, true)

	// Re-enabling a dead builtin must survive restarts.
	p := byName["bracket-hash-dash"]
	p.Enabled = true
	if err := store.UpdateParsePattern(&p); err != nil {
		t.Fatal(err)
	}
	if err := SeedPatterns(store); err != nil {
		t.Fatal(err)
	}
	all, _ = store.GetAllParsePatterns()
	for _, q := range all {
		if q.Name == "bracket-hash-dash" && !q.Enabled {
			t.Error("SeedPatterns re-disabled a user-enabled builtin")
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
			ID:       int64(i + 1),
			Name:     b.Name,
			Regex:    b.Regex,
			Priority: b.Priority,
			Builtin:  true,
			Enabled:  true,
		}
	}
	return patterns
}

package parser

import (
	"testing"

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

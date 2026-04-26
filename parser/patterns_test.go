package parser

import (
	"testing"

	"github.com/RealDtx/maxwell-irc/db"
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

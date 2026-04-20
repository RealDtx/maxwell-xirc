package parser

import (
	"encoding/json"
	"testing"
)

func TestSyncPatterns_EmptySlice(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	n, err := SyncPatterns(store, nil)
	if err != nil {
		t.Fatalf("SyncPatterns(nil): unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 inserted, got %d", n)
	}

	n, err = SyncPatterns(store, []ConfigPattern{})
	if err != nil {
		t.Fatalf("SyncPatterns([]): unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 inserted for empty slice, got %d", n)
	}
}

func TestSyncPatterns_InsertsNewPatterns(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	patterns := []ConfigPattern{
		{Name: "p1", Regex: `#(\d+)`, FieldMapping: map[string]int{"pack_number": 1}, Priority: 10},
		{Name: "p2", Regex: `(\w+\.mkv)`, FieldMapping: map[string]int{"filename": 1}, Priority: 5},
	}

	n, err := SyncPatterns(store, patterns)
	if err != nil {
		t.Fatalf("SyncPatterns: unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 inserted, got %d", n)
	}

	all, err := store.GetAllParsePatterns()
	if err != nil {
		t.Fatalf("GetAllParsePatterns: %v", err)
	}
	found := map[string]bool{}
	for _, p := range all {
		found[p.Name] = true
	}
	if !found["p1"] || !found["p2"] {
		t.Errorf("expected both p1 and p2 in DB, got %v", found)
	}
}

func TestSyncPatterns_SkipsExistingNames(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	// Pre-insert "foo" using SyncPatterns to keep the test self-contained.
	if _, err := SyncPatterns(store, []ConfigPattern{
		{Name: "foo", Regex: `#(\d+)`, FieldMapping: map[string]int{"pack_number": 1}},
	}); err != nil {
		t.Fatalf("pre-insert: %v", err)
	}

	// Now sync a slice containing the existing "foo" and a new "bar".
	n, err := SyncPatterns(store, []ConfigPattern{
		{Name: "foo", Regex: `#(\d+)`, FieldMapping: map[string]int{"pack_number": 1}},
		{Name: "bar", Regex: `(\w+)`, FieldMapping: map[string]int{"filename": 1}},
	})
	if err != nil {
		t.Fatalf("SyncPatterns: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 inserted (bar only), got %d", n)
	}

	all, err := store.GetAllParsePatterns()
	if err != nil {
		t.Fatalf("GetAllParsePatterns: %v", err)
	}
	var fooCount, barCount int
	for _, p := range all {
		if p.Name == "foo" {
			fooCount++
		}
		if p.Name == "bar" {
			barCount++
		}
	}
	if fooCount != 1 {
		t.Errorf("expected exactly 1 'foo', got %d", fooCount)
	}
	if barCount != 1 {
		t.Errorf("expected exactly 1 'bar', got %d", barCount)
	}
}

func TestSyncPatterns_Idempotent(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	patterns := []ConfigPattern{
		{Name: "idem1", Regex: `#(\d+)`, FieldMapping: map[string]int{"pack_number": 1}, Priority: 20},
		{Name: "idem2", Regex: `(\w+\.mkv)`, FieldMapping: map[string]int{"filename": 1}, Priority: 15},
	}

	n1, err := SyncPatterns(store, patterns)
	if err != nil {
		t.Fatalf("first SyncPatterns: %v", err)
	}
	if n1 != 2 {
		t.Errorf("first call: expected 2 inserted, got %d", n1)
	}

	n2, err := SyncPatterns(store, patterns)
	if err != nil {
		t.Fatalf("second SyncPatterns: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second call: expected 0 inserted (idempotent), got %d", n2)
	}
}

func TestSyncPatterns_FieldMappingSerializedToJSON(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	fm := map[string]int{"pack_number": 1, "filename": 2}
	patterns := []ConfigPattern{
		{Name: "with-fm", Regex: `#(\d+)\s+(.+)`, FieldMapping: fm, Priority: 50},
	}

	if _, err := SyncPatterns(store, patterns); err != nil {
		t.Fatalf("SyncPatterns: %v", err)
	}

	all, err := store.GetAllParsePatterns()
	if err != nil {
		t.Fatalf("GetAllParsePatterns: %v", err)
	}
	var stored string
	for _, p := range all {
		if p.Name == "with-fm" {
			stored = p.FieldMapping
			break
		}
	}
	if stored == "" {
		t.Fatal("pattern 'with-fm' not found in DB")
	}

	// Must be valid JSON.
	var decoded map[string]int
	if err := json.Unmarshal([]byte(stored), &decoded); err != nil {
		t.Fatalf("stored field_mapping is not valid JSON: %q: %v", stored, err)
	}
	// Must round-trip correctly.
	if decoded["pack_number"] != 1 || decoded["filename"] != 2 {
		t.Errorf("unexpected decoded field_mapping: %v", decoded)
	}
}

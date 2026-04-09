package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func newTestStore(t *testing.T) db.Store {
	t.Helper()
	dir, err := ioutil.TempDir("", "seed_test")
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed: %v", err)
	}
	store.Migrate()
	// We can't use t.Cleanup in Go 1.13, so we just defer in tests
	// Store the dir in a channel or just rely on test process cleanup
	// Actually use a finalizer approach: just return store without cleanup
	// The OS will clean up temp files after the test process exits
	return store
}

func TestSeedRoutingRules(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

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
	defer store.Close()

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
	// We seed 18 rules total
	if count > 20 {
		t.Errorf("rules doubled on re-seed: %d", count)
	}
	if count != 18 {
		t.Errorf("expected 18 rules, got %d", count)
	}
}

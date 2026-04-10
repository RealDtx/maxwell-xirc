package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func newTestStore(t *testing.T) (db.Store, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "seed_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to open store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		store.Close()
		os.RemoveAll(dir)
		t.Fatalf("migrate failed: %v", err)
	}
	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return store, cleanup
}

func TestSeedRoutingRules(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

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
	store, cleanup := newTestStore(t)
	defer cleanup()

	if err := SeedRoutingRules(store, "/media", "/dl"); err != nil {
		t.Fatalf("first seed failed: %v", err)
	}
	if err := SeedRoutingRules(store, "/media", "/dl"); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}

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

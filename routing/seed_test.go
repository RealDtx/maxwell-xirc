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

func TestSyncBuiltinRuleDirs(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	if err := SeedRoutingRules(store, "/old/media", "/old/dl"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if err := SyncBuiltinRuleDirs(store, "/new/media", "/new/dl"); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		t.Fatalf("failed to get rules: %v", err)
	}

	var foundCatchAll bool
	for _, r := range rules {
		if !r.Builtin {
			continue
		}
		if r.Pattern == "*" {
			foundCatchAll = true
			if r.DestinationDir != "/new/dl" {
				t.Errorf("expected catch-all destination /new/dl, got %q", r.DestinationDir)
			}
			continue
		}
		if r.DestinationDir != "/new/media" {
			t.Errorf("expected builtin %q destination /new/media, got %q", r.Pattern, r.DestinationDir)
		}
	}
	if !foundCatchAll {
		t.Fatal("expected catch-all builtin rule")
	}
}

func TestSyncBuiltinRuleDirs_UserRulesUnchanged(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	if err := store.CreateFileRoutingRule(&db.FileRoutingRule{
		Pattern:        "*.custom",
		DestinationDir: "/user/dir",
		Priority:       200,
		Builtin:        false,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("failed to create user rule: %v", err)
	}
	if err := SeedRoutingRules(store, "/media", "/dl"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	if err := SyncBuiltinRuleDirs(store, "/new/media", "/new/dl"); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	rules, err := store.GetAllFileRoutingRules()
	if err != nil {
		t.Fatalf("failed to get rules: %v", err)
	}

	var foundUserRule bool
	for _, r := range rules {
		if r.Pattern == "*.custom" && !r.Builtin {
			foundUserRule = true
			if r.DestinationDir != "/user/dir" {
				t.Errorf("expected user rule destination /user/dir, got %q", r.DestinationDir)
			}
		}
	}
	if !foundUserRule {
		t.Fatal("expected user rule to exist")
	}
}

func TestSyncBuiltinRuleDirs_NoOpWhenSame(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	if err := SeedRoutingRules(store, "/media", "/dl"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	if err := SyncBuiltinRuleDirs(store, "/media", "/dl"); err != nil {
		t.Fatalf("sync should be a no-op but failed: %v", err)
	}
}

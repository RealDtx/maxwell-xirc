package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/db"
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

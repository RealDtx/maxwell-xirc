package db

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestNewStore_SQLite(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-factory-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "factory.db")

	store, err := NewStore("sqlite", path)
	if err != nil {
		t.Fatalf("NewStore sqlite failed: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify it works by creating a server
	err = store.CreateServer(&Server{Name: "test", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true})
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	servers, err := store.GetServers()
	if err != nil {
		t.Fatalf("GetServers failed: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("expected 1 server, got %d", len(servers))
	}
}

func TestNewStore_UnknownDriver(t *testing.T) {
	_, err := NewStore("postgres", "something")
	if err == nil {
		t.Error("expected error for unknown driver")
	}
}

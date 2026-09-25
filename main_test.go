package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/library"
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

func TestLoadLibrary_MissingFileDetectsAndSaves(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-library-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mediaRoot := filepath.Join(dir, "media")
	if err := os.MkdirAll(filepath.Join(mediaRoot, "Movies"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{Storage: config.StorageConfig{MediaDir: mediaRoot}}

	mgr := loadLibrary(configPath, cfg)

	categoriesPath := filepath.Join(dir, "categories.yaml")
	if _, err := os.Stat(categoriesPath); err != nil {
		t.Fatalf("expected categories.yaml to be created next to config.yaml: %v", err)
	}
	if got := mgr.Get().MediaRoot; got != mediaRoot {
		t.Errorf("media_root = %q, want %q", got, mediaRoot)
	}
}

func TestLoadLibrary_ExistingFileLoaded(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-library-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mediaRoot := filepath.Join(dir, "media")
	os.MkdirAll(mediaRoot, 0755)
	configPath := filepath.Join(dir, "config.yaml")
	categoriesPath := filepath.Join(dir, "categories.yaml")
	want := library.Config{AutoOrganize: false, SearchDepth: 7, MediaRoot: mediaRoot}
	if err := library.Save(categoriesPath, &want); err != nil {
		t.Fatal(err)
	}

	mgr := loadLibrary(configPath, &config.Config{Storage: config.StorageConfig{MediaDir: mediaRoot}})

	got := mgr.Get()
	if got.SearchDepth != 7 || got.AutoOrganize {
		t.Errorf("got %+v, want the saved config to be loaded as-is", got)
	}
}

func TestLoadLibrary_UnparseableFileFallsBackWithoutOverwriting(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-library-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mediaRoot := filepath.Join(dir, "media")
	os.MkdirAll(mediaRoot, 0755)
	configPath := filepath.Join(dir, "config.yaml")
	categoriesPath := filepath.Join(dir, "categories.yaml")
	badContent := []byte("not: valid: yaml: [")
	if err := os.WriteFile(categoriesPath, badContent, 0644); err != nil {
		t.Fatal(err)
	}

	mgr := loadLibrary(configPath, &config.Config{Storage: config.StorageConfig{MediaDir: mediaRoot}})

	if got := mgr.Get().MediaRoot; got != mediaRoot {
		t.Errorf("expected in-memory fallback to detected defaults, got media_root %q", got)
	}
	onDisk, err := os.ReadFile(categoriesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(badContent) {
		t.Error("the unparseable file on disk should not have been overwritten")
	}
}

func TestLoadLibrary_CustomCategoriesFile(t *testing.T) {
	dir, err := ioutil.TempDir("", "xirc-library-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mediaRoot := filepath.Join(dir, "media")
	os.MkdirAll(mediaRoot, 0755)
	custom := filepath.Join(dir, "custom-categories.yaml")
	configPath := filepath.Join(dir, "config.yaml")

	mgr := loadLibrary(configPath, &config.Config{Storage: config.StorageConfig{MediaDir: mediaRoot, CategoriesFile: custom}})

	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("expected the configured categories_file path to be used: %v", err)
	}
	_ = mgr
}

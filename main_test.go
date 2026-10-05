package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
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

	media := filepath.Join(dir, "media")
	downloads := filepath.Join(dir, "downloads")
	if err := os.Mkdir(media, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(downloads, 0755); err != nil {
		t.Fatal(err)
	}

	bad := checkDirectories(media, downloads)
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

	media := filepath.Join(dir, "media")
	if err := os.Mkdir(media, 0755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "does-not-exist")

	bad := checkDirectories(media, missing)
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

	missing := filepath.Join(dir, "does-not-exist")

	bad := checkDirectories(missing, missing)
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

func TestRunCreateAdmin_EnvPassword(t *testing.T) {
	store, err := db.NewSQLiteStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.Migrate()
	t.Setenv("XIRC_ADMIN_PASSWORD", "password123")
	if err := runCreateAdmin(store, "Boss", strings.NewReader(""), false); err != nil {
		t.Fatal(err)
	}
	u, _ := store.GetUserByName("boss")
	if u == nil || u.Role != "admin" {
		t.Fatalf("admin not created: %+v", u)
	}
}

func TestRunCreateAdmin_NoPasswordNonTTY(t *testing.T) {
	store, _ := db.NewSQLiteStore(filepath.Join(t.TempDir(), "t.db"))
	defer store.Close()
	store.Migrate()
	t.Setenv("XIRC_ADMIN_PASSWORD", "")
	if err := runCreateAdmin(store, "boss", strings.NewReader(""), false); err == nil {
		t.Fatal("expected error without password source")
	}
}

// A stray </div> once nested the login overlay inside the hidden directory
// picker: fresh installs got the app without a login and without admin.
func TestIndexHTML_OverlaysAreTopLevel(t *testing.T) {
	src, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)[strings.Index(string(src), "<body"):]
	depth := 0 // ponytail: counts <div> tags only, enough for this file's layout
	for _, line := range strings.Split(body, "\n") {
		for _, m := range []string{`display: authMode ?`, `display: setupRequired ?`, `display: setupBanner ?`} {
			if strings.Contains(line, m) && depth != 0 {
				t.Errorf("%s overlay is nested %d divs deep, want a direct child of <body>", m, depth)
			}
		}
		depth += strings.Count(line, "<div") - strings.Count(line, "</div>")
	}
	if depth != 0 {
		t.Errorf("unbalanced <div>s in index.html: %+d", depth)
	}
}

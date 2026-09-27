package library

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestLoad_UpgradesOldCategoriesFile checks the one-time migration: a
// categories.yaml written before archive support (Version 0) gets the
// missing tar/zip/rar/7z extensions added to its series/movie categories,
// and extraction + archive deletion are turned on only where the file does
// not mention them — other kinds are left untouched, and the file's Version
// is bumped so this runs only once.
func TestLoad_UpgradesOldCategoriesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.yaml")

	old := "media_root: " + dir + `
categories:
  - {id: series, kind: series, extensions: [mkv, mp4]}
  - {id: movie, kind: movie, extensions: [mkv]}
  - {id: ebook, kind: ebook, extensions: [pdf, epub]}
`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, upgraded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !upgraded {
		t.Fatal("expected upgraded=true for a Version-0 file")
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, CurrentVersion)
	}

	byID := map[string]Category{}
	for _, c := range cfg.Categories {
		byID[c.ID] = c
	}

	for _, id := range []string{"series", "movie"} {
		cat := byID[id]
		for _, ext := range []string{"tar", "zip", "rar", "7z"} {
			if !hasExt(cat.Extensions, ext) {
				t.Errorf("%s: expected extension %q added, got %v", id, ext, cat.Extensions)
			}
		}
		if !cat.AutoExtract {
			t.Errorf("%s: expected auto_extract=true after upgrade (key absent)", id)
		}
		if !cat.DeleteArchive {
			t.Errorf("%s: expected delete_archive=true after upgrade (key absent)", id)
		}
	}

	ebook := byID["ebook"]
	if hasExt(ebook.Extensions, "zip") {
		t.Errorf("ebook: unrelated kind must not gain archive extensions, got %v", ebook.Extensions)
	}
	if len(ebook.Extensions) != 2 || ebook.AutoExtract || ebook.DeleteArchive {
		t.Errorf("ebook: changed by upgrade: %+v", ebook)
	}
}

// TestLoad_UpgradeKeepsExplicitArchiveFlags checks that the v0 migration
// never flips an explicit auto_extract/delete_archive false to true: doing so
// could extract and then permanently delete a user's archives.
func TestLoad_UpgradeKeepsExplicitArchiveFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.yaml")

	old := Config{
		MediaRoot: dir,
		Categories: []Category{
			{ID: "series", Kind: "series", Extensions: []string{"mkv"}},
			{ID: "movie", Kind: "movie", Extensions: []string{"mkv"}},
		},
	}
	if err := Save(path, &old); err != nil { // writes explicit false for both flags
		t.Fatal(err)
	}

	cfg, upgraded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !upgraded || cfg.Version != CurrentVersion {
		t.Fatalf("expected version upgrade, got upgraded=%v version=%d", upgraded, cfg.Version)
	}
	for _, cat := range cfg.Categories {
		if cat.AutoExtract || cat.DeleteArchive {
			t.Errorf("%s: explicit false flipped: auto_extract=%v delete_archive=%v", cat.ID, cat.AutoExtract, cat.DeleteArchive)
		}
		if !hasExt(cat.Extensions, "zip") {
			t.Errorf("%s: expected archive extensions still added, got %v", cat.ID, cat.Extensions)
		}
	}
}

// TestLoad_CurrentVersionNotReupgraded checks that a file already at
// CurrentVersion is left alone even if it (deliberately) lacks the archive
// extensions — the migration is one-time, not an ongoing default-enforcer.
func TestLoad_CurrentVersionNotReupgraded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.yaml")

	current := Config{
		Version:   CurrentVersion,
		MediaRoot: dir,
		Categories: []Category{
			{ID: "series", Kind: "series", Extensions: []string{"mkv"}, AutoExtract: false},
		},
	}
	if err := Save(path, &current); err != nil {
		t.Fatal(err)
	}

	cfg, upgraded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if upgraded {
		t.Error("expected upgraded=false for a file already at CurrentVersion")
	}
	if hasExt(cfg.Categories[0].Extensions, "zip") {
		t.Errorf("expected extensions untouched, got %v", cfg.Categories[0].Extensions)
	}
	if cfg.Categories[0].AutoExtract {
		t.Error("expected auto_extract left as saved (false)")
	}
}

// TestManagerSet_Concurrent checks that concurrent Set calls never trip over
// a shared temp file and that the last write wins on disk and in memory alike.
func TestManagerSet_Concurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.yaml")
	m := NewManager(path, Config{MediaRoot: dir})

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- m.Set(Config{Version: CurrentVersion, MediaRoot: dir, SearchDepth: i})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Set: %v", err)
		}
	}

	disk, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if mem := m.Get(); disk.SearchDepth != mem.SearchDepth {
		t.Errorf("disk search_depth %d != memory %d", disk.SearchDepth, mem.SearchDepth)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

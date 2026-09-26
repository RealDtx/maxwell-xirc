package library

import (
	"path/filepath"
	"testing"
)

// TestLoad_UpgradesOldCategoriesFile checks the one-time migration: a
// categories.yaml written before archive support (Version 0) gets the
// missing tar/zip/rar/7z extensions added to its series/movie categories,
// with extraction and archive deletion turned on — other kinds are left
// untouched, and the file's Version is bumped so this runs only once.
func TestLoad_UpgradesOldCategoriesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.yaml")

	old := Config{
		MediaRoot: dir,
		Categories: []Category{
			{ID: "series", Kind: "series", Extensions: []string{"mkv", "mp4"}},
			{ID: "movie", Kind: "movie", Extensions: []string{"mkv"}, AutoExtract: false},
			{ID: "ebook", Kind: "ebook", Extensions: []string{"pdf", "epub"}},
		},
	}
	if err := Save(path, &old); err != nil {
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
			t.Errorf("%s: expected auto_extract=true after upgrade", id)
		}
		if !cat.DeleteArchive {
			t.Errorf("%s: expected delete_archive=true after upgrade", id)
		}
	}

	ebook := byID["ebook"]
	if hasExt(ebook.Extensions, "zip") {
		t.Errorf("ebook: unrelated kind must not gain archive extensions, got %v", ebook.Extensions)
	}
	if len(ebook.Extensions) != 2 {
		t.Errorf("ebook: extensions changed, got %v", ebook.Extensions)
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

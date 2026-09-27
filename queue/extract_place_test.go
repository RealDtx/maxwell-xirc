package queue

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/library"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// An extracted file whose name collides with one already in the category
// folder must not overwrite it, and the archive must survive (even with
// delete_archive on) so nothing is lost.
func TestPlaceExtracted_CollisionKeepsOriginalAndArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pack.zip")
	writeZip(t, archive, map[string]string{"sub/ep01.mkv": "new", "sub/ep02.mkv": "two"})
	existing := filepath.Join(dir, "ep01.mkv")
	if err := os.WriteFile(existing, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	extracted, err := library.Extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	warning := placeExtracted(archive, dir, extracted, true)

	if got := readFile(t, existing); got != "original" {
		t.Errorf("existing file clobbered: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "ep01 (1).mkv")); got != "new" {
		t.Errorf("colliding extracted file not kept under a unique name: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "ep02.mkv")); got != "two" {
		t.Errorf("ep02.mkv: %q", got)
	}
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("archive deleted after a collision: %v", err)
	}
	if warning == "" {
		t.Error("expected a warning naming the collision")
	}
}

// Without collisions every file lands flat and delete_archive removes the
// archive, with no warning.
func TestPlaceExtracted_CleanDeletesArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pack.zip")
	writeZip(t, archive, map[string]string{"sub/ep01.mkv": "one"})

	extracted, err := library.Extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if w := placeExtracted(archive, dir, extracted, true); w != "" {
		t.Errorf("unexpected warning: %s", w)
	}
	if got := readFile(t, filepath.Join(dir, "ep01.mkv")); got != "one" {
		t.Errorf("ep01.mkv: %q", got)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Errorf("archive should be deleted, stat err=%v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected only ep01.mkv left, got %v", entries)
	}
}

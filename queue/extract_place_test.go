package queue

import (
	"archive/tar"
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/library"
	"github.com/RealDtx/maxwell-irc/routing"
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

	extracted, staging, err := library.Extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	warning := placeExtracted(archive, dir, staging, extracted, true)

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

	extracted, staging, err := library.Extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if w := placeExtracted(archive, dir, staging, extracted, true); w != "" {
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

func writeTar(t *testing.T, path string, hdrs []*tar.Header, bodies []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for i, h := range hdrs {
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(bodies[i]))
		}
		if h.Mode == 0 {
			h.Mode = 0644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(bodies[i]))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func assertNoStaging(t *testing.T, dir string) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(dir, ".extract_*")); len(m) != 0 {
		t.Errorf("staging dir left behind: %v", m)
	}
}

// tar -v under the C locale escapes non-ASCII names; the file list must come
// from the filesystem so the file is placed, not stranded while the archive
// is deleted. An unrelated empty user folder must survive.
func TestPlaceExtracted_NonASCIITarCLocale(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	dir := t.TempDir()
	userDir := filepath.Join(dir, "Season 2")
	if err := os.Mkdir(userDir, 0755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "pack.tar")
	writeTar(t, archive, []*tar.Header{{Name: "sub/Größe.mkv", Typeflag: tar.TypeReg}}, []string{"data"})

	extracted, staging, err := routing.Extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if w := placeExtracted(archive, dir, staging, extracted, true); w != "" {
		t.Errorf("unexpected warning: %s", w)
	}
	if got := readFile(t, filepath.Join(dir, "Größe.mkv")); got != "data" {
		t.Errorf("Größe.mkv: %q", got)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Errorf("archive should be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(userDir); err != nil {
		t.Errorf("empty user folder removed: %v", err)
	}
	assertNoStaging(t, dir)
}

// Anything that can't be placed (here a symlink, which is not a regular
// file) stays in staging and keeps the archive.
func TestPlaceExtracted_LeftoverKeepsArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pack.tar")
	writeTar(t, archive, []*tar.Header{
		{Name: "a.mkv", Typeflag: tar.TypeReg},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "a.mkv"},
	}, []string{"x", ""})

	extracted, staging, err := routing.Extract(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	if w := placeExtracted(archive, dir, staging, extracted, true); w == "" {
		t.Error("expected a warning about leftovers")
	}
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("archive deleted despite leftovers: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "a.mkv")); got != "x" {
		t.Errorf("a.mkv: %q", got)
	}
}

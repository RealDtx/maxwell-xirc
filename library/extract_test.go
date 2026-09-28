package library

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "test.zip")
	writeZip(t, archive, map[string]string{
		"good.txt":       "hello",
		"sub/nested.txt": "world",
	})

	destDir := filepath.Join(dir, "dest")
	files, _, err := Extract(archive, destDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 extracted files, got %d: %v", len(files), files)
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("extracted file %q missing: %v", f, err)
		}
	}
}

// TestExtractZip_ZipSlipRejected checks that an entry whose path would land
// outside the extraction dir (e.g. "../../evil.txt") is dropped rather than
// extracted, while sibling entries still extract normally.
func TestExtractZip_ZipSlipRejected(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	writeZip(t, archive, map[string]string{
		"good.txt":         "hello",
		"../../evil.txt":   "pwned",
		"../../../x/y.txt": "also pwned",
	})

	destDir := filepath.Join(dir, "dest")
	files, _, err := Extract(archive, destDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "good.txt" {
		t.Fatalf("expected only good.txt to be extracted, got %v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Error("zip-slip entry escaped destDir")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); err == nil {
		t.Error("zip-slip entry escaped destDir")
	}
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func Test7zRoundTrip(t *testing.T) {
	if !toolAvailable("7z") {
		t.Skip("7z not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "test.7z")
	if out, err := exec.Command("7z", "a", archive, src).CombinedOutput(); err != nil {
		t.Fatalf("creating test 7z archive: %v: %s", err, out)
	}

	destDir := filepath.Join(dir, "dest")
	files, _, err := Extract(archive, destDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "payload.txt" {
		t.Fatalf("expected [payload.txt], got %v", files)
	}
}

func TestUnrarRoundTrip(t *testing.T) {
	if !toolAvailable("rar") {
		t.Skip("rar (creator) not installed — only extraction (unrar/7z) is required at runtime")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "test.rar")
	if out, err := exec.Command("rar", "a", archive, src).CombinedOutput(); err != nil {
		t.Fatalf("creating test rar archive: %v: %s", err, out)
	}

	destDir := filepath.Join(dir, "dest")
	files, _, err := Extract(archive, destDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "payload.txt" {
		t.Fatalf("expected [payload.txt], got %v", files)
	}
}

func TestArchiveVolumes(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"Show.part01.rar", "Show.part02.rar", "Show.part10.rar",
		"Old.rar", "Old.r00", "Old.r01",
		"Pack.zip", "Pack.z01",
		"Big.7z.001", "Big.7z.002",
		"Solo.7z", "Src.tar.gz", "movie.mkv",
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name  string
		ok    bool
		first bool
		stem  string
		vols  []string
	}{
		{"Show.part01.rar", true, true, "Show", []string{"Show.part01.rar", "Show.part02.rar", "Show.part10.rar"}},
		{"Show.part02.rar", true, false, "Show", []string{"Show.part01.rar", "Show.part02.rar", "Show.part10.rar"}},
		{"Old.rar", true, true, "Old", []string{"Old.r00", "Old.r01", "Old.rar"}},
		{"Old.r00", true, false, "Old", []string{"Old.r00", "Old.r01", "Old.rar"}},
		{"Pack.zip", true, true, "Pack", []string{"Pack.z01", "Pack.zip"}},
		{"Big.7z.001", true, true, "Big", []string{"Big.7z.001", "Big.7z.002"}},
		{"Big.7z.002", true, false, "Big", []string{"Big.7z.001", "Big.7z.002"}},
		{"Solo.7z", true, true, "Solo", []string{"Solo.7z"}},
		{"Src.tar.gz", true, true, "Src", []string{"Src.tar.gz"}},
		{"movie.mkv", false, false, "", nil},
	}
	for _, c := range cases {
		got, ok := ArchiveVolumes(dir, c.name)
		if ok != c.ok || got.First != c.first || got.Stem != c.stem || strings.Join(got.Volumes, ",") != strings.Join(c.vols, ",") {
			t.Errorf("ArchiveVolumes(%q) = %+v,%v; want first=%v stem=%q vols=%v ok=%v", c.name, got, ok, c.first, c.stem, c.vols, c.ok)
		}
	}
}

func TestExtractAny_Zip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, map[string]string{"x.txt": "hi"})
	files, staging, err := ExtractAny(archive, dir)
	if err != nil || len(files) != 1 || filepath.Dir(staging) != dir {
		t.Fatalf("ExtractAny = %v %q %v", files, staging, err)
	}
}

// TestExtractAny_SplitZipRoutesToTool checks that a split zip set
// (Pack.zip + Pack.z01) is routed to extractWithTool rather than
// archive/zip, which can't read split zips. With no 7z/unrar on PATH the
// tool path fails with its own "no archive tool available" error, proving
// the route taken; a lone (non-split) zip must still use archive/zip.
func TestExtractAny_SplitZipRoutesToTool(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "Pack.zip")
	writeZip(t, archive, map[string]string{"x.txt": "hi"})
	if err := os.WriteFile(filepath.Join(dir, "Pack.z01"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	if _, _, err := ExtractAny(archive, dir); err == nil || !strings.Contains(err.Error(), "no archive tool available") {
		t.Fatalf("split zip should route to extractWithTool, got err=%v", err)
	}

	lone := filepath.Join(dir, "solo.zip")
	writeZip(t, lone, map[string]string{"y.txt": "hi"})
	files, _, err := ExtractAny(lone, dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("lone zip should still extract via archive/zip: %v %v", files, err)
	}
}

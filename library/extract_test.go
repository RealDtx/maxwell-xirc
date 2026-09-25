package library

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
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
	files, err := Extract(archive, destDir)
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
	files, err := Extract(archive, destDir)
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
	files, err := Extract(archive, destDir)
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
	files, err := Extract(archive, destDir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "payload.txt" {
		t.Fatalf("expected [payload.txt], got %v", files)
	}
}

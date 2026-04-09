package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestMatchRule_MediaFiles(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true},
		{ID: 2, Pattern: "*.mp4", DestinationDir: "/media", Priority: 100, Enabled: true},
		{ID: 3, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
	}

	dest := MatchRule("movie.mkv", rules)
	if dest != "/media" {
		t.Errorf("expected /media, got %s", dest)
	}

	dest = MatchRule("video.mp4", rules)
	if dest != "/media" {
		t.Errorf("expected /media, got %s", dest)
	}

	dest = MatchRule("document.pdf", rules)
	if dest != "/downloads" {
		t.Errorf("expected /downloads (catch-all), got %s", dest)
	}
}

func TestMatchRule_PriorityOrder(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
		{ID: 2, Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: true},
	}

	dest := MatchRule("movie.mkv", rules)
	if dest != "/media" {
		t.Errorf("expected /media (higher priority), got %s", dest)
	}
}

func TestMatchRule_DisabledSkipped(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*.mkv", DestinationDir: "/media", Priority: 100, Enabled: false},
		{ID: 2, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
	}

	dest := MatchRule("movie.mkv", rules)
	if dest != "/downloads" {
		t.Errorf("expected /downloads (disabled rule skipped), got %s", dest)
	}
}

func TestMatchRule_MultipleExtensions(t *testing.T) {
	rules := []db.FileRoutingRule{
		{ID: 1, Pattern: "*.srt", DestinationDir: "/media", Priority: 80, Enabled: true},
		{ID: 2, Pattern: "*.sub", DestinationDir: "/media", Priority: 80, Enabled: true},
		{ID: 3, Pattern: "*", DestinationDir: "/downloads", Priority: 0, Enabled: true},
	}

	if MatchRule("subs.srt", rules) != "/media" {
		t.Error("expected .srt to match media")
	}
	if MatchRule("subs.sub", rules) != "/media" {
		t.Error("expected .sub to match media")
	}
}

func TestMoveFile_SameFilesystem(t *testing.T) {
	// Go 1.13 compat: use ioutil.TempDir
	dir, err := ioutil.TempDir("", "routing_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	srcDir := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "dst")
	os.MkdirAll(srcDir, 0755)
	os.MkdirAll(dstDir, 0755)

	srcPath := filepath.Join(srcDir, "test.mkv")
	ioutil.WriteFile(srcPath, []byte("video data"), 0644)

	destPath, err := MoveFile(srcPath, dstDir)
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	expected := filepath.Join(dstDir, "test.mkv")
	if destPath != expected {
		t.Errorf("expected %s, got %s", expected, destPath)
	}

	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("source file should not exist after move")
	}
	data, _ := ioutil.ReadFile(destPath)
	if string(data) != "video data" {
		t.Errorf("file contents mismatch: %s", string(data))
	}
}

func TestMoveFile_CreatesDestDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "routing_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	srcPath := filepath.Join(dir, "test.pdf")
	ioutil.WriteFile(srcPath, []byte("pdf data"), 0644)

	dstDir := filepath.Join(dir, "new", "subdir")
	destPath, err := MoveFile(srcPath, dstDir)
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	data, _ := ioutil.ReadFile(destPath)
	if string(data) != "pdf data" {
		t.Error("file contents mismatch")
	}
	_ = destPath
}

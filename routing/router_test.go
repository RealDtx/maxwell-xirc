package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/db"
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

func TestInferSubdir(t *testing.T) {
	dir, err := ioutil.TempDir("", "infersubdir_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	for _, sub := range []string{
		"Scrubs", "S1", ".hidden",
		"Series/Star Trek Strange New Worlds/Season 4",
		"Series/Star Trek Strange New Worlds/S03",
		"Series/Andor/S02",
		"a/b/c/Deep Show",
	} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	snw := filepath.Join(dir, "Series", "Star Trek Strange New Worlds")

	tests := []struct {
		name     string
		filename string
		depth    int
		want     string
	}{
		{"matches existing subdir", "Scrubs.2026.S01E02.mkv", 3, filepath.Join(dir, "Scrubs")},
		{"no false match on short names", "S1.movie.mkv", 3, dir},
		{"no match at all falls back to ruleDir", "Unrelated.Movie.2026.mkv", 3, dir},
		{"nested show + Season N folder", "Star.Trek.Strange.New.Worlds.S04E10.GERMAN.2160p.mkv", 3, filepath.Join(snw, "Season 4")},
		{"nested show + SNN folder", "Star.Trek.Strange.New.Worlds.S03E01.mkv", 3, filepath.Join(snw, "S03")},
		{"show without matching season folder", "Star.Trek.Strange.New.Worlds.S01E01.mkv", 3, snw},
		{"season folder beyond depth still found", "Andor.S02E01.mkv", 2, filepath.Join(dir, "Series", "Andor", "S02")},
		{"show beyond depth not found", "Deep.Show.S01E01.mkv", 3, dir},
		{"show within larger depth found", "Deep.Show.S01E01.mkv", 4, filepath.Join(dir, "a", "b", "c", "Deep Show")},
		{"depth 0 disables search", "Scrubs.S01E02.mkv", 0, dir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferSubdir(dir, tt.filename, tt.depth)
			if got != tt.want {
				t.Errorf("InferSubdir(%q, %d) = %q, want %q", tt.filename, tt.depth, got, tt.want)
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	dir, err := ioutil.TempDir("", "copyfile_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	ioutil.WriteFile(src, []byte("hello world"), 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	data, _ := ioutil.ReadFile(dst)
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
	// Source must still exist (copyFile does not remove src)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		t.Error("copyFile should not remove the source file")
	}
}

func TestMoveFile_PermissionDeniedDoesNotCopy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir, err := ioutil.TempDir("", "movefile_perm")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "a.mkv")
	locked := filepath.Join(dir, "locked")
	os.WriteFile(src, []byte("x"), 0644)
	os.Mkdir(locked, 0555)
	defer os.Chmod(locked, 0755)

	if _, err := MoveFile(src, locked); err == nil {
		t.Fatal("expected permission error")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source must stay put: %v", err)
	}
}

package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

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

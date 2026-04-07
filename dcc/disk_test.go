package dcc

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestGetAvailableSpace_CurrentDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-disk-space-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	avail, err := GetAvailableSpace(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if avail == 0 {
		t.Error("expected non-zero available space")
	}
}

func TestGetAvailableSpace_NonexistentDir(t *testing.T) {
	_, err := GetAvailableSpace("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"1GB", 1073741824, false},
		{"1gb", 1073741824, false},
		{"500MB", 524288000, false},
		{"500mb", 524288000, false},
		{"1TB", 1099511627776, false},
		{"100KB", 102400, false},
		{"1024", 1024, false},
		{"", 0, true},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseSize(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestCheckDiskSpace_Sufficient(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-disk-space-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Asking for 1 byte should always succeed on any system with disk space
	err = CheckDiskSpace(dir, 1, "1KB")
	if err != nil {
		t.Errorf("unexpected error for tiny file: %v", err)
	}
}

func TestCheckDiskSpace_ExcessiveSize(t *testing.T) {
	dir, err := ioutil.TempDir("", "test-disk-space-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Asking for an absurd amount should fail
	err = CheckDiskSpace(dir, 1<<62, "0")
	if err == nil {
		t.Error("expected error for absurdly large file")
	}
}

func TestCheckDiskSpace_NonexistentDir(t *testing.T) {
	err := CheckDiskSpace("/nonexistent/dir", 100, "0")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
	_ = os.Remove("/nonexistent")
}

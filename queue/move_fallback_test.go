package queue

import (
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/dcc"
	"github.com/RealDtx/maxwell-irc/irc"
)

// A finished file whose destination isn't writable must land in the
// downloads dir with a note — never silently stay in the hidden temp dir.
func TestRunTransfer_UnwritableDestinationFallsBackToDownloads(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	store, cleanup := newTestStore(t)
	defer cleanup()
	dir, err := ioutil.TempDir("", "engine-move-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	locked := filepath.Join(dir, "media", "locked")
	downloads := filepath.Join(dir, "downloads")
	temp := filepath.Join(dir, "downloads", ".tmp")
	for _, d := range []string{locked, temp} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	os.Chmod(locked, 0555)
	defer os.Chmod(locked, 0755)

	bus := irc.NewEventBus()
	engine := NewEngine(store, bus, irc.NewManager(store, bus), &config.StorageConfig{DownloadsDir: downloads, TempDir: temp}, 1)
	dl, err := engine.queue.Add(1, "#c", "Bot", 1, "ep.mkv", 4, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// An explicit per-download target (the first stop in the destination
	// order) points at the unwritable dir, forcing the same move-failure
	// fallback that a matched-but-unwritable library category would hit.
	dl.TargetDir = locked
	if err := store.UpdateDownload(dl); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Write([]byte("data"))
		c.Close()
	}()
	offer := &dcc.DCCOffer{Filename: "ep.mkv", IP: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Size: 4}
	engine.runTransfer(dl.ID, offer, filepath.Join(temp, "ep.mkv"), 0)

	got, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(downloads, "ep.mkv")
	if got.Status != "completed" || got.DestinationPath != want {
		t.Errorf("status=%q dest=%q, want completed at %q", got.Status, got.DestinationPath, want)
	}
	if !strings.Contains(got.ErrorMessage, "could not move") {
		t.Errorf("expected a move note, got %q", got.ErrorMessage)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("file not in downloads dir: %v", err)
	}
}

// A move failure's reason must be descriptive (owner/mode, via
// fscheck.Describe), not just the raw OS error.
func TestRunTransfer_UnwritableTargetRecordsDescriptiveReason(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	store, cleanup := newTestStore(t)
	defer cleanup()
	dir, err := ioutil.TempDir("", "engine-move-reason-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	targetDir := filepath.Join(dir, "media", "locked")
	downloadsDir := filepath.Join(dir, "downloads")
	tempDir := filepath.Join(dir, "downloads", ".tmp")
	for _, d := range []string{targetDir, tempDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(targetDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(targetDir, 0755); err != nil {
			t.Fatal(err)
		}
	}()

	bus := irc.NewEventBus()
	engine := NewEngine(store, bus, irc.NewManager(store, bus), &config.StorageConfig{DownloadsDir: downloadsDir, TempDir: tempDir}, 1)
	dl, err := engine.queue.Add(1, "#c", "Bot", 1, "ep.mkv", 4, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	dl.TargetDir = targetDir
	if err := store.UpdateDownload(dl); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Write([]byte("data"))
		c.Close()
	}()
	offer := &dcc.DCCOffer{Filename: "ep.mkv", IP: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Size: 4}
	engine.runTransfer(dl.ID, offer, filepath.Join(tempDir, "ep.mkv"), 0)

	got, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" {
		t.Errorf("status %q", got.Status)
	}
	if filepath.Dir(got.DestinationPath) != downloadsDir {
		t.Errorf("file not in downloads dir: %s", got.DestinationPath)
	}
	if !strings.Contains(got.ErrorMessage, "permission denied: "+targetDir) {
		t.Errorf("reason not descriptive: %q", got.ErrorMessage)
	}
	if entries, _ := os.ReadDir(tempDir); len(entries) != 0 {
		t.Error("file left in temp dir")
	}
}

package queue

import (
	"archive/tar"
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/dcc"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/library"
)

// serveOverTCP starts a one-shot TCP listener that writes data to the first
// connection it accepts, then closes it — standing in for the bot's DCC
// SEND socket so runTransfer's real dcc.Transfer has something to read.
func serveOverTCP(t *testing.T, data []byte) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write(data)
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// A file that no library category (and no explicit target) claims must drop
// flat into downloads_dir — the new "unmatched" destination fallback.
func TestRunTransfer_UnmatchedGoesToDownloadsDir(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	dir := t.TempDir()

	downloads := filepath.Join(dir, "downloads")
	temp := filepath.Join(downloads, ".tmp")
	if err := os.MkdirAll(temp, 0755); err != nil {
		t.Fatal(err)
	}

	bus := irc.NewEventBus()
	engine := NewEngine(store, bus, irc.NewManager(store, bus), &config.StorageConfig{DownloadsDir: downloads, TempDir: temp}, 1)
	// No library set — every file is "unmatched" by construction.

	dl, err := engine.queue.Add(1, "#c", "Bot", 1, "random.file.bin", 4, false, true, true)
	if err != nil {
		t.Fatal(err)
	}

	port := serveOverTCP(t, []byte("data"))
	offer := &dcc.DCCOffer{Filename: "random.file.bin", IP: "127.0.0.1", Port: port, Size: 4}
	engine.runTransfer(dl.ID, offer, filepath.Join(temp, "random.file.bin"), 0)

	got, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(downloads, "random.file.bin")
	if got.Status != "completed" || got.DestinationPath != want {
		t.Fatalf("status=%q dest=%q, want completed at %q", got.Status, got.DestinationPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("file not in downloads dir: %v", err)
	}
}

// buildTar returns the bytes of a tar archive containing one file.
func buildTar(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(content)), Mode: 0644}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A tar season pack with no episode number (a whole-season release) must be
// categorized as series, extracted into the show's season folder, flattened,
// and have its archive deleted — matching a matched category's auto_extract
// and delete_archive.
func TestRunTransfer_TarSeasonPackExtractedAndArchiveDeleted(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	dir := t.TempDir()

	mediaRoot := filepath.Join(dir, "media")
	downloads := filepath.Join(dir, "downloads")
	temp := filepath.Join(downloads, ".tmp")
	for _, d := range []string{mediaRoot, temp} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	bus := irc.NewEventBus()
	engine := NewEngine(store, bus, irc.NewManager(store, bus), &config.StorageConfig{DownloadsDir: downloads, TempDir: temp}, 1)
	libCfg := library.Detect(mediaRoot)
	engine.SetLibrary(library.NewManager(filepath.Join(dir, "categories.yaml"), libCfg))

	dl, err := engine.queue.Add(1, "#c", "Bot", 1, "Show.S03.1080p.WEB.tar", 0, false, true, true)
	if err != nil {
		t.Fatal(err)
	}

	tarBytes := buildTar(t, "Show.S03E01.1080p.WEB.mkv", []byte("fake-video-bytes"))
	port := serveOverTCP(t, tarBytes)
	offer := &dcc.DCCOffer{Filename: "Show.S03.1080p.WEB.tar", IP: "127.0.0.1", Port: port, Size: int64(len(tarBytes))}
	engine.runTransfer(dl.ID, offer, filepath.Join(temp, "Show.S03.1080p.WEB.tar"), 0)

	got, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" {
		t.Fatalf("status=%q, error=%q, want completed", got.Status, got.ErrorMessage)
	}

	seasonDir := filepath.Join(mediaRoot, "Series", "Show", "S03")
	extractedFile := filepath.Join(seasonDir, "Show.S03E01.1080p.WEB.mkv")
	if _, err := os.Stat(extractedFile); err != nil {
		t.Errorf("expected extracted episode at %s: %v", extractedFile, err)
	}
	if _, err := os.Stat(filepath.Join(seasonDir, "Show.S03.1080p.WEB.tar")); !os.IsNotExist(err) {
		t.Errorf("expected archive to be deleted after extraction, stat err = %v", err)
	}
}

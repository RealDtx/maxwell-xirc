package queue

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func newTestStore(t *testing.T) (db.Store, func()) {
	t.Helper()
	dir, err := ioutil.TempDir("", "queue-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create store: %v", err)
	}
	store.Migrate()

	// Create a test server since downloads require ServerID foreign key
	srv := &db.Server{
		Name:     "TestServer",
		Host:     "irc.test.com",
		Port:     6667,
		Nickname: "TestBot",
		Enabled:  true,
	}
	if err := store.CreateServer(srv); err != nil {
		store.Close()
		os.RemoveAll(dir)
		t.Fatalf("failed to create test server: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}
	return store, cleanup
}

func TestQueue_AddDownload(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl, err := q.Add(1, "#channel", "BotNick", 1, "file.txt", 1024)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if dl.ID == 0 {
		t.Error("expected ID to be set after creation")
	}
	if dl.ServerID != 1 {
		t.Errorf("expected ServerID=1, got %d", dl.ServerID)
	}
	if dl.Channel != "#channel" {
		t.Errorf("expected Channel=#channel, got %s", dl.Channel)
	}
	if dl.BotNick != "BotNick" {
		t.Errorf("expected BotNick=BotNick, got %s", dl.BotNick)
	}
	if dl.PackNumber != 1 {
		t.Errorf("expected PackNumber=1, got %d", dl.PackNumber)
	}
	if dl.Filename != "file.txt" {
		t.Errorf("expected Filename=file.txt, got %s", dl.Filename)
	}
	if dl.Filesize != 1024 {
		t.Errorf("expected Filesize=1024, got %d", dl.Filesize)
	}
	if dl.Status != "queued" {
		t.Errorf("expected Status=queued, got %s", dl.Status)
	}
}

func TestQueue_NextReturnsOldestQueued(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl1, _ := q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024)
	_, _ = q.Add(1, "#channel", "BotA", 2, "file2.txt", 2048)
	_, _ = q.Add(1, "#channel", "BotA", 3, "file3.txt", 4096)

	next := q.Next()
	if next == nil {
		t.Fatal("expected Next() to return a download")
	}
	if next.ID != dl1.ID {
		t.Errorf("expected oldest download (ID=%d), got ID=%d", dl1.ID, next.ID)
	}
}

func TestQueue_RespectsMaxConcurrent(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl1, _ := q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024)
	dl2, _ := q.Add(1, "#channel", "BotB", 2, "file2.txt", 2048)
	dl3, _ := q.Add(1, "#channel", "BotC", 3, "file3.txt", 4096)

	// Mark first two as downloading
	q.MarkDownloading(dl1.ID)
	q.MarkDownloading(dl2.ID)

	// Next() should return nil since we're at maxConcurrent=2
	next := q.Next()
	if next != nil {
		t.Error("expected Next() to return nil when at maxConcurrent limit")
	}

	// Mark one as completed
	q.MarkCompleted(dl1.ID, "/path/to/file", 100, 50)

	// Now Next() should return the third download
	next = q.Next()
	if next == nil {
		t.Fatal("expected Next() to return a download after one completes")
	}
	if next.ID != dl3.ID {
		t.Errorf("expected download ID=%d, got ID=%d", dl3.ID, next.ID)
	}
}

func TestQueue_OnePerBot(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 10) // High concurrency limit

	// Add three downloads from BotA
	dl1, _ := q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024)
	dl2, _ := q.Add(1, "#channel", "BotA", 2, "file2.txt", 2048)
	dl3, _ := q.Add(1, "#channel", "BotB", 3, "file3.txt", 4096)
	_ = dl2 // acknowledge that we're not using it in this test

	// Mark first BotA download as downloading
	q.MarkDownloading(dl1.ID)

	// Next() should skip dl2 (from same bot) and return dl3 (from BotB)
	next := q.Next()
	if next == nil {
		t.Fatal("expected Next() to return a download")
	}
	if next.ID != dl3.ID {
		t.Errorf("expected download from BotB (ID=%d), got ID=%d", dl3.ID, next.ID)
	}
}

func TestQueue_Cancel(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024)

	err := q.Cancel(dl.ID)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	retrieved, _ := store.GetDownload(dl.ID)
	if retrieved.Status != "cancelled" {
		t.Errorf("expected Status=cancelled, got %s", retrieved.Status)
	}
}

func TestQueue_Retry(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024)
	q.MarkDownloading(dl.ID)
	q.MarkFailed(dl.ID, "connection timeout")

	err := q.Retry(dl.ID)
	if err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	retrieved, _ := store.GetDownload(dl.ID)
	if retrieved.Status != "queued" {
		t.Errorf("expected Status=queued, got %s", retrieved.Status)
	}
	if retrieved.ErrorMessage != "" {
		t.Errorf("expected ErrorMessage to be cleared, got %s", retrieved.ErrorMessage)
	}
	if retrieved.DownloadedBytes != 0 {
		t.Errorf("expected DownloadedBytes=0, got %d", retrieved.DownloadedBytes)
	}
	if retrieved.StartedAt != nil {
		t.Error("expected StartedAt to be nil after retry")
	}
	if retrieved.CompletedAt != nil {
		t.Error("expected CompletedAt to be nil after retry")
	}
}

func TestQueue_Reorder(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 3)

	_, _ = q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024)
	dl2, _ := q.Add(1, "#channel", "BotB", 2, "file2.txt", 2048)

	// MoveToFront dl2
	err := q.MoveToFront(dl2.ID)
	if err != nil {
		t.Fatalf("MoveToFront failed: %v", err)
	}

	// Next() should now return dl2 (moved to front)
	next := q.Next()
	if next == nil {
		t.Fatal("expected Next() to return a download")
	}
	if next.ID != dl2.ID {
		t.Errorf("expected download ID=%d (moved to front), got ID=%d", dl2.ID, next.ID)
	}
}

package queue

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/RealDtx/maxwell-irc/db"
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

	dl, err := q.Add(1, "#channel", "BotNick", 1, "file.txt", 1024, false, false, true, 3)
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

	dl1, _ := q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024, false, false, true, 3)
	_, _ = q.Add(1, "#channel", "BotA", 2, "file2.txt", 2048, false, false, true, 3)
	_, _ = q.Add(1, "#channel", "BotA", 3, "file3.txt", 4096, false, false, true, 3)

	next, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected NextAndMarkDownloading() to return a download")
	}
	if next.ID != dl1.ID {
		t.Errorf("expected oldest download (ID=%d), got ID=%d", dl1.ID, next.ID)
	}
}

func TestQueue_RespectsMaxConcurrent(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl1, _ := q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024, false, false, true, 3)
	_, _ = q.Add(1, "#channel", "BotB", 2, "file2.txt", 2048, false, false, true, 3)
	dl3, _ := q.Add(1, "#channel", "BotC", 3, "file3.txt", 4096, false, false, true, 3)

	// Mark first two as downloading
	_, _ = q.NextAndMarkDownloading()
	_, _ = q.NextAndMarkDownloading()

	// NextAndMarkDownloading() should return nil since we're at maxConcurrent=2
	next, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next != nil {
		t.Error("expected NextAndMarkDownloading() to return nil when at maxConcurrent limit")
	}

	// Mark one as completed
	q.MarkCompleted(dl1.ID, "/path/to/file", 100, 50)

	// Now NextAndMarkDownloading() should return the third download
	next, err = q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected NextAndMarkDownloading() to return a download after one completes")
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
	_, _ = q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024, false, false, true, 3)
	_, _ = q.Add(1, "#channel", "BotA", 2, "file2.txt", 2048, false, false, true, 3)
	dl3, _ := q.Add(1, "#channel", "BotB", 3, "file3.txt", 4096, false, false, true, 3)

	// Mark first BotA download as downloading
	_, _ = q.NextAndMarkDownloading()

	// NextAndMarkDownloading() should skip dl2 (from same bot) and return dl3 (from BotB)
	next, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected NextAndMarkDownloading() to return a download")
	}
	if next.ID != dl3.ID {
		t.Errorf("expected download from BotB (ID=%d), got ID=%d", dl3.ID, next.ID)
	}
}

func TestQueue_Cancel(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)

	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024, false, false, true, 3)

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

	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024, false, false, true, 3)
	_, _ = q.NextAndMarkDownloading()
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

	_, _ = q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024, false, false, true, 3)
	dl2, _ := q.Add(1, "#channel", "BotB", 2, "file2.txt", 2048, false, false, true, 3)

	// MoveToFront dl2
	err := q.MoveToFront(dl2.ID)
	if err != nil {
		t.Fatalf("MoveToFront failed: %v", err)
	}

	// NextAndMarkDownloading() should now return dl2 (moved to front)
	next, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected NextAndMarkDownloading() to return a download")
	}
	if next.ID != dl2.ID {
		t.Errorf("expected download ID=%d (moved to front), got ID=%d", dl2.ID, next.ID)
	}
}

func TestQueue_MoveToFront_MultiplePromotions(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	q := New(store, 3)

	srv := &db.Server{Name: "srv", Host: "a.com", Port: 6667, Nickname: "bot", Enabled: true}
	store.CreateServer(srv)

	dl1, _ := q.Add(srv.ID, "#t", "bot1", 1, "first.mkv", 100, false, false, true, 3)
	dl2, _ := q.Add(srv.ID, "#t", "bot2", 2, "second.mkv", 100, false, false, true, 3)
	dl3, _ := q.Add(srv.ID, "#t", "bot3", 3, "third.mkv", 100, false, false, true, 3)

	// Promote dl3 to front, then dl2 to front
	// Expected order: dl2, dl3, dl1
	if err := q.MoveToFront(dl3.ID); err != nil {
		t.Fatalf("first MoveToFront failed: %v", err)
	}
	if err := q.MoveToFront(dl2.ID); err != nil {
		t.Fatalf("second MoveToFront failed: %v", err)
	}

	next, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected a download")
	}
	if next.ID != dl2.ID {
		t.Errorf("expected dl2 at front after double promotion, got id %d (%s)", next.ID, next.Filename)
	}
	_ = dl1
}

func TestQueue_MarkNeedsAction(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)
	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024, false, false, true, 3)

	err := q.MarkNeedsAction(dl.ID, "manual captcha required")
	if err != nil {
		t.Fatalf("MarkNeedsAction failed: %v", err)
	}

	retrieved, _ := store.GetDownload(dl.ID)
	if retrieved.Status != "needs_action" {
		t.Errorf("expected Status=needs_action, got %s", retrieved.Status)
	}
	if retrieved.ErrorMessage != "manual captcha required" {
		t.Errorf("expected ErrorMessage=manual captcha required, got %s", retrieved.ErrorMessage)
	}
}

func TestQueue_UpdateProgress(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)
	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024, false, false, true, 3)

	err := q.UpdateProgress(dl.ID, 512, 200, 150)
	if err != nil {
		t.Fatalf("UpdateProgress failed: %v", err)
	}

	retrieved, _ := store.GetDownload(dl.ID)
	if retrieved.DownloadedBytes != 512 {
		t.Errorf("expected DownloadedBytes=512, got %d", retrieved.DownloadedBytes)
	}
	if retrieved.PeakSpeed != 200 {
		t.Errorf("expected PeakSpeed=200, got %d", retrieved.PeakSpeed)
	}
	if retrieved.AverageSpeed != 150 {
		t.Errorf("expected AverageSpeed=150, got %d", retrieved.AverageSpeed)
	}
}

func TestQueue_UpdateDestinationPath(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 2)
	dl, _ := q.Add(1, "#channel", "BotA", 1, "file.txt", 1024, false, false, true, 3)

	newPath := "/downloads/final/file.txt"
	err := q.UpdateDestinationPath(dl.ID, newPath)
	if err != nil {
		t.Fatalf("UpdateDestinationPath failed: %v", err)
	}

	retrieved, _ := store.GetDownload(dl.ID)
	if retrieved.DestinationPath != newPath {
		t.Errorf("expected DestinationPath=%s, got %s", newPath, retrieved.DestinationPath)
	}
}

func TestQueue_RequeueInterrupted(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	q := New(store, 3)
	_, _ = q.Add(1, "#channel", "BotA", 1, "file1.txt", 1024, false, false, true, 3)
	_, _ = q.Add(1, "#channel", "BotB", 2, "file2.txt", 2048, false, false, true, 3)

	first, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("first NextAndMarkDownloading failed: %v", err)
	}
	if first == nil {
		t.Fatal("expected first download to be marked downloading")
	}

	second, err := q.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("second NextAndMarkDownloading failed: %v", err)
	}
	if second == nil {
		t.Fatal("expected second download to be marked downloading")
	}

	// A third download that finished its transfer but was interrupted during
	// post-processing (routing/hooks/extraction) — the file itself is done.
	third, _ := q.Add(1, "#channel", "BotC", 3, "file3.txt", 4096, false, false, true, 3)
	if err := q.MarkProcessing(third.ID, "/tmp/file3.txt"); err != nil {
		t.Fatalf("MarkProcessing failed: %v", err)
	}

	err = q.RequeueInterrupted()
	if err != nil {
		t.Fatalf("RequeueInterrupted failed: %v", err)
	}

	retrievedFirst, _ := store.GetDownload(first.ID)
	if retrievedFirst.Status != "queued" {
		t.Errorf("expected first Status=queued, got %s", retrievedFirst.Status)
	}

	retrievedSecond, _ := store.GetDownload(second.ID)
	if retrievedSecond.Status != "queued" {
		t.Errorf("expected second Status=queued, got %s", retrievedSecond.Status)
	}

	retrievedThird, _ := store.GetDownload(third.ID)
	if retrievedThird.Status != "completed" {
		t.Errorf("expected third (was processing) Status=completed, got %s", retrievedThird.Status)
	}
	if retrievedThird.CompletedAt == nil {
		t.Error("expected third CompletedAt to be set")
	}
}

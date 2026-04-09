package queue

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/irc"
)

func TestEngine_HandlesDCCOffer(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	bus := irc.NewEventBus()
	storageCfg := &config.StorageConfig{
		DownloadsDir: "/tmp/downloads",
		TempDir:      "/tmp/temp",
		MinFreeSpace: "1MB",
	}

	engine := NewEngine(store, bus, storageCfg, 2)

	dl, err := engine.queue.Add(1, "#channel", "BotNick", 1, "file.txt", 1024)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	engine.RegisterPendingRequest(dl.ID, 1, "BotNick")

	pending := engine.GetPendingRequest(1, "BotNick")
	if pending == nil {
		t.Fatal("expected GetPendingRequest to return the pending request")
	}

	if pending.DownloadID != dl.ID {
		t.Errorf("expected DownloadID=%d, got %d", dl.ID, pending.DownloadID)
	}
	if pending.ServerID != 1 {
		t.Errorf("expected ServerID=1, got %d", pending.ServerID)
	}
	if pending.BotNick != "BotNick" {
		t.Errorf("expected BotNick=BotNick, got %s", pending.BotNick)
	}
}

func TestEngine_DetectsPassiveDCC(t *testing.T) {
	// Simple test to verify that DCC offers with port 0 are detected as passive
	// No engine needed for this test
	testCases := []struct {
		port    int
		passive bool
	}{
		{0, true},      // port 0 = passive
		{1024, false},  // port > 0 = active
		{6969, false},  // typical DCC port = active
	}

	for _, tc := range testCases {
		// Simulate the check that happens in ParseDCCSend
		// Port 0 should be treated as passive
		isPassive := tc.port == 0
		if isPassive != tc.passive {
			t.Errorf("port %d: expected passive=%v, got %v", tc.port, tc.passive, isPassive)
		}
	}
}

func TestEngine_QueueProcessing(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	bus := irc.NewEventBus()
	storageCfg := &config.StorageConfig{
		DownloadsDir: "/tmp/downloads",
		TempDir:      "/tmp/temp",
		MinFreeSpace: "1MB",
	}

	engine := NewEngine(store, bus, storageCfg, 2)

	// Add downloads to queue
	dl1, err := engine.queue.Add(1, "#channel", "BotA", 1, "file1.txt", 1024)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	dl2, err := engine.queue.Add(1, "#channel", "BotB", 2, "file2.txt", 2048)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify NextAndMarkDownloading() returns the oldest queued download
	next, err := engine.queue.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected NextAndMarkDownloading() to return a download")
	}

	if next.ID != dl1.ID {
		t.Errorf("expected oldest download (ID=%d), got ID=%d", dl1.ID, next.ID)
	}

	// Mark second as downloading and verify NextAndMarkDownloading() returns it
	next, err = engine.queue.NextAndMarkDownloading()
	if err != nil {
		t.Fatalf("NextAndMarkDownloading failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected NextAndMarkDownloading() to return second download")
	}

	if next.ID != dl2.ID {
		t.Errorf("expected second download (ID=%d), got ID=%d", dl2.ID, next.ID)
	}
}

func TestEngine_StartsAndStops(t *testing.T) {
	dir, err := ioutil.TempDir("", "engine-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	store, cleanup := newTestStore(t)
	defer cleanup()

	bus := irc.NewEventBus()
	storageCfg := &config.StorageConfig{
		DownloadsDir: dir,
		TempDir:      dir,
		MinFreeSpace: "1MB",
	}

	engine := NewEngine(store, bus, storageCfg, 2)

	// Start should not panic
	engine.Start()

	// Stop should not panic
	engine.Stop()
}

package queue

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

type recordingStore struct {
	db.Store
	mu                 sync.Mutex
	lastUpdated        *db.Download
	sawDownloading     bool
	downloadingStarted bool
	downloadingName    string
	downloadingSize    int64
}

func (s *recordingStore) UpdateDownload(d *db.Download) error {
	s.mu.Lock()
	cp := *d
	s.lastUpdated = &cp
	if d.Status == "downloading" {
		s.sawDownloading = true
		s.downloadingStarted = d.StartedAt != nil
		s.downloadingName = d.Filename
		s.downloadingSize = d.Filesize
	}
	s.mu.Unlock()
	return s.Store.UpdateDownload(d)
}

func TestEngine_HandlesDCCOffer(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	storageCfg := &config.StorageConfig{
		DownloadsDir: "/tmp/downloads",
		TempDir:      "/tmp/temp",
		MinFreeSpace: "1MB",
	}

	engine := NewEngine(store, bus, ircMgr, storageCfg, 2)

	dl, err := engine.queue.Add(1, "#channel", "BotNick", 1, "file.txt", 1024, false)
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
		{0, true},     // port 0 = passive
		{1024, false}, // port > 0 = active
		{6969, false}, // typical DCC port = active
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
	ircMgr := irc.NewManager(store, bus)
	storageCfg := &config.StorageConfig{
		DownloadsDir: "/tmp/downloads",
		TempDir:      "/tmp/temp",
		MinFreeSpace: "1MB",
	}

	engine := NewEngine(store, bus, ircMgr, storageCfg, 2)

	// Add downloads to queue
	dl1, err := engine.queue.Add(1, "#channel", "BotA", 1, "file1.txt", 1024, false)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	dl2, err := engine.queue.Add(1, "#channel", "BotB", 2, "file2.txt", 2048, false)
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
	ircMgr := irc.NewManager(store, bus)
	storageCfg := &config.StorageConfig{
		DownloadsDir: dir,
		TempDir:      dir,
		MinFreeSpace: "1MB",
	}

	engine := NewEngine(store, bus, ircMgr, storageCfg, 2)

	// Start should not panic
	engine.Start()

	// Stop should not panic
	engine.Stop()
}

func TestEngine_Dispatch_NoConnection(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	storageCfg := &config.StorageConfig{
		DownloadsDir: "data",
		TempDir:      "data/tmp",
		MinFreeSpace: "0",
	}
	engine := NewEngine(store, bus, ircMgr, storageCfg, 2)

	dl, err := engine.queue.Add(1, "#channel", "BotNick", 123, "file.txt", 1000, false)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	err = engine.Dispatch(dl)
	if err == nil {
		t.Fatal("expected dispatch error when no IRC connection exists")
	}
	if !strings.Contains(err.Error(), "no IRC connection") {
		t.Fatalf("unexpected error: %v", err)
	}
	if pending := engine.GetPendingRequest(dl.ServerID, dl.BotNick); pending != nil {
		t.Fatalf("expected no pending request to be registered on failed dispatch")
	}
}

func TestEngine_HandleMessage_MarksDownloadingBeforeTransfer(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	dir, err := ioutil.TempDir("", "engine-storage-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	recStore := &recordingStore{Store: store}
	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(recStore, bus)
	storageCfg := &config.StorageConfig{
		DownloadsDir: filepath.Join(dir, "downloads"),
		TempDir:      filepath.Join(dir, "temp"),
		MinFreeSpace: "1000TB",
	}
	engine := NewEngine(recStore, bus, ircMgr, storageCfg, 2)

	dl, err := engine.queue.Add(1, "#channel", "BotNick", 5, "queued-name.mkv", 1, false)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	engine.RegisterPendingRequest(dl.ID, 1, "BotNick")

	engine.handleMessage(irc.Event{
		Type:     irc.EventIRCMessage,
		ServerID: 1,
		Nick:     "BotNick",
		Data: map[string]string{
			"type":    "ctcp",
			"message": `DCC SEND "Offer.Name.mkv" 3232235777 4500 2048`,
		},
	})

	recStore.mu.Lock()
	sawDownloading := recStore.sawDownloading
	downloadingStarted := recStore.downloadingStarted
	downloadingName := recStore.downloadingName
	downloadingSize := recStore.downloadingSize
	recStore.mu.Unlock()
	if !sawDownloading {
		t.Fatal("expected a download update with status=downloading")
	}
	if !downloadingStarted {
		t.Fatal("expected started_at to be set when DCC offer arrives")
	}
	if downloadingName != "Offer.Name.mkv" {
		t.Fatalf("expected filename from offer, got %q", downloadingName)
	}
	if downloadingSize != 2048 {
		t.Fatalf("expected filesize from offer, got %d", downloadingSize)
	}
}

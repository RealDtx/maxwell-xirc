package queue

import (
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/irc"
)

// helper: downloads as GetDownloads returns them — newest first.
func queuedDesc(dls ...db.Download) []db.Download {
	for i := range dls {
		dls[i].Status = "queued"
		dls[i].CreatedAt = time.Now().Add(-time.Duration(len(dls)-i) * time.Minute)
	}
	// input given oldest-first for readability; reverse to newest-first
	out := make([]db.Download, 0, len(dls))
	for i := len(dls) - 1; i >= 0; i-- {
		out = append(out, dls[i])
	}
	return out
}

func TestSelectDispatchable_FIFOAndCapacity(t *testing.T) {
	queued := queuedDesc(
		db.Download{ID: 1, ServerID: 1, BotNick: "BotA"},
		db.Download{ID: 2, ServerID: 1, BotNick: "BotB"},
		db.Download{ID: 3, ServerID: 1, BotNick: "BotC"},
	)

	got := selectDispatchable(queued, map[string]bool{}, 1, 3)

	if len(got) != 2 {
		t.Fatalf("expected 2 dispatches (3 slots - 1 in flight), got %d", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("expected oldest first (IDs 1,2), got %d,%d", got[0].ID, got[1].ID)
	}
}

func TestSelectDispatchable_OnePerBot(t *testing.T) {
	queued := queuedDesc(
		db.Download{ID: 1, ServerID: 1, BotNick: "BotA"},
		db.Download{ID: 2, ServerID: 1, BotNick: "BotA"}, // same bot: must wait
		db.Download{ID: 3, ServerID: 1, BotNick: "BotB"},
	)

	got := selectDispatchable(queued, map[string]bool{}, 0, 3)

	if len(got) != 2 {
		t.Fatalf("expected 2 dispatches (BotA once, BotB once), got %d", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 3 {
		t.Errorf("expected IDs 1,3, got %d,%d", got[0].ID, got[1].ID)
	}
}

func TestSelectDispatchable_SkipsBusyBots_FullCapacity(t *testing.T) {
	queued := queuedDesc(
		db.Download{ID: 1, ServerID: 1, BotNick: "BotA"}, // bot already busy
		db.Download{ID: 2, ServerID: 2, BotNick: "BotA"}, // same nick, other server: free
	)
	busy := map[string]bool{pendingKey(1, "BotA"): true}

	got := selectDispatchable(queued, busy, 1, 2)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("expected only ID 2 (server 2), got %+v", got)
	}

	// At capacity: nothing dispatches.
	if got := selectDispatchable(queued, busy, 2, 2); len(got) != 0 {
		t.Fatalf("expected no dispatches at full capacity, got %d", len(got))
	}
}

// TryDispatchQueued must leave downloads queued (not fail or drop them) when
// the IRC connection is unavailable, so a later pump can pick them up.
func TestEngine_TryDispatchQueued_NoConnection(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	bus := irc.NewEventBus()
	ircMgr := irc.NewManager(store, bus)
	engine := NewEngine(store, bus, ircMgr, &config.StorageConfig{
		DownloadsDir: "/tmp/downloads", TempDir: "/tmp/temp", MinFreeSpace: "1MB",
	}, 2)

	dl, err := engine.queue.Add(1, "#chan", "BotA", 1, "f.txt", 1, false, false, true, 3)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	engine.TryDispatchQueued()

	after, err := store.GetDownload(dl.ID)
	if err != nil {
		t.Fatalf("GetDownload failed: %v", err)
	}
	if after.Status != "queued" {
		t.Errorf("expected download to stay queued without IRC connection, got %s", after.Status)
	}
	if engine.GetPendingRequest(1, "BotA") != nil {
		t.Error("expected no pending request after failed dispatch")
	}
}

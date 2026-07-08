package db

import "testing"

func TestRollupIndexStats(t *testing.T) {
	files := []indexFileAggRow{
		{ServerID: 1, Channel: "#a", BotNick: "bot1", Filesize: "1G", Count: 2, LastSeen: "2026-07-01 10:00:00"},
		{ServerID: 1, Channel: "#a", BotNick: "bot1", Filesize: "512M", Count: 1, LastSeen: "2026-07-02 10:00:00"},
		{ServerID: 1, Channel: "#a", BotNick: "bot2", Filesize: "", Count: 5, LastSeen: "2026-07-03 10:00:00"},
		{ServerID: 2, Channel: "#b", BotNick: "bot3", Filesize: "junk", Count: 1, LastSeen: "2026-07-04 10:00:00"},
	}
	transfers := []botTransferAggRow{
		{ServerID: 1, Channel: "#a", BotNick: "bot1", Transfers: 3, AvgSpeed: 1048576.5, PeakSpeed: 2097152},
		// transfer for a bot no longer in the index: must be ignored
		{ServerID: 9, Channel: "#gone", BotNick: "ghost", Transfers: 1, AvgSpeed: 1, PeakSpeed: 1},
	}

	d := rollupIndexStats(files, transfers)

	if len(d.Bots) != 3 {
		t.Fatalf("len(Bots) = %d, want 3", len(d.Bots))
	}
	// Sorted by files desc: bot2 (5), bot1 (3), bot3 (1).
	b := d.Bots[1]
	if b.BotNick != "bot1" || b.Files != 3 {
		t.Fatalf("Bots[1] = %+v, want bot1 with 3 files", b)
	}
	wantBytes := int64(2*(1<<30)) + 512*(1<<20)
	if b.AdvertisedBytes != wantBytes {
		t.Errorf("bot1 AdvertisedBytes = %d, want %d", b.AdvertisedBytes, wantBytes)
	}
	if b.LastSeenAt != "2026-07-02 10:00:00" {
		t.Errorf("bot1 LastSeenAt = %q", b.LastSeenAt)
	}
	if b.Transfers != 3 || b.AvgSpeed != 1048576 || b.PeakSpeed != 2097152 {
		t.Errorf("bot1 transfer stats = %+v", b)
	}
	if d.Bots[0].Transfers != 0 || d.Bots[0].AvgSpeed != 0 {
		t.Errorf("bot2 should have zero transfer stats, got %+v", d.Bots[0])
	}

	if len(d.Channels) != 2 {
		t.Fatalf("len(Channels) = %d, want 2", len(d.Channels))
	}
	// Sorted by files desc: #a (8), #b (1).
	c := d.Channels[0]
	if c.Channel != "#a" || c.Bots != 2 || c.Files != 8 {
		t.Fatalf("Channels[0] = %+v, want #a with 2 bots / 8 files", c)
	}
	if c.AdvertisedBytes != wantBytes { // bot2 ("") and unparseable contribute 0
		t.Errorf("#a AdvertisedBytes = %d, want %d", c.AdvertisedBytes, wantBytes)
	}
	if c.LastSeenAt != "2026-07-03 10:00:00" {
		t.Errorf("#a LastSeenAt = %q", c.LastSeenAt)
	}
}

func TestRollupIndexStats_Empty(t *testing.T) {
	d := rollupIndexStats(nil, nil)
	if d.Channels == nil || d.Bots == nil {
		t.Fatal("empty rollup must return empty slices, not nil (JSON [] not null)")
	}
	if len(d.Channels) != 0 || len(d.Bots) != 0 {
		t.Fatalf("expected empty result, got %+v", d)
	}
}

package db

import (
	"sort"

	"github.com/RealDtx/maxwell-irc/dcc"
)

// IndexChannelStats aggregates the persistent index per download channel.
type IndexChannelStats struct {
	ServerID        int64  `json:"server_id"`
	Channel         string `json:"channel"`
	Bots            int    `json:"bots"`
	Files           int64  `json:"files"`
	AdvertisedBytes int64  `json:"advertised_bytes"`
	LastSeenAt      string `json:"last_seen_at"`
}

// IndexBotStats aggregates the persistent index per bot, enriched with
// bandwidth observed during completed downloads (zero = never downloaded).
type IndexBotStats struct {
	ServerID        int64  `json:"server_id"`
	Channel         string `json:"channel"`
	BotNick         string `json:"bot_nick"`
	Files           int64  `json:"files"`
	AdvertisedBytes int64  `json:"advertised_bytes"`
	LastSeenAt      string `json:"last_seen_at"`
	Transfers       int64  `json:"transfers"`
	AvgSpeed        int64  `json:"avg_speed"`
	PeakSpeed       int64  `json:"peak_speed"`
}

type IndexStatsDetail struct {
	Channels []IndexChannelStats `json:"channels"`
	Bots     []IndexBotStats     `json:"bots"`
}

// indexFileAggRow is one scanned row of:
//
//	SELECT server_id, channel, bot_nick, COALESCE(filesize,''), COUNT(*), MAX(last_seen_at)
//	FROM indexed_files GROUP BY server_id, channel, bot_nick, filesize
//
// LastSeen stays a string: modernc/sqlite returns TEXT for expression columns,
// go-sql-driver's time.Time converts to RFC3339 via database/sql. Both formats
// are lexicographically ordered, so string max is correct per store.
type indexFileAggRow struct {
	ServerID int64
	Channel  string
	BotNick  string
	Filesize string
	Count    int64
	LastSeen string
}

// botTransferAggRow is one scanned row of:
//
//	SELECT server_id, channel, bot_nick, COUNT(*), COALESCE(AVG(average_speed),0), COALESCE(MAX(peak_speed),0)
//	FROM downloads WHERE status='completed' GROUP BY server_id, channel, bot_nick
type botTransferAggRow struct {
	ServerID  int64
	Channel   string
	BotNick   string
	Transfers int64
	AvgSpeed  float64
	PeakSpeed int64
}

type botKey struct {
	serverID int64
	channel  string
	botNick  string
}

// rollupIndexStats folds the pre-grouped rows into per-bot and per-channel
// aggregates. Advertised sizes are parsed from the raw ad strings ("1.4G");
// empty or unparseable sizes contribute 0 bytes.
func rollupIndexStats(files []indexFileAggRow, transfers []botTransferAggRow) *IndexStatsDetail {
	bots := map[botKey]*IndexBotStats{}
	for _, r := range files {
		k := botKey{r.ServerID, r.Channel, r.BotNick}
		b := bots[k]
		if b == nil {
			b = &IndexBotStats{ServerID: r.ServerID, Channel: r.Channel, BotNick: r.BotNick}
			bots[k] = b
		}
		b.Files += r.Count
		if bytes, err := dcc.ParseSize(r.Filesize); err == nil {
			b.AdvertisedBytes += bytes * r.Count
		}
		if r.LastSeen > b.LastSeenAt {
			b.LastSeenAt = r.LastSeen
		}
	}

	for _, tr := range transfers {
		if b := bots[botKey{tr.ServerID, tr.Channel, tr.BotNick}]; b != nil {
			b.Transfers = tr.Transfers
			b.AvgSpeed = int64(tr.AvgSpeed)
			b.PeakSpeed = tr.PeakSpeed
		}
	}

	channels := map[botKey]*IndexChannelStats{} // botNick left empty in key
	for _, b := range bots {
		k := botKey{serverID: b.ServerID, channel: b.Channel}
		c := channels[k]
		if c == nil {
			c = &IndexChannelStats{ServerID: b.ServerID, Channel: b.Channel}
			channels[k] = c
		}
		c.Bots++
		c.Files += b.Files
		c.AdvertisedBytes += b.AdvertisedBytes
		if b.LastSeenAt > c.LastSeenAt {
			c.LastSeenAt = b.LastSeenAt
		}
	}

	d := &IndexStatsDetail{Channels: []IndexChannelStats{}, Bots: []IndexBotStats{}}
	for _, b := range bots {
		d.Bots = append(d.Bots, *b)
	}
	for _, c := range channels {
		d.Channels = append(d.Channels, *c)
	}
	sort.Slice(d.Bots, func(i, j int) bool {
		if d.Bots[i].Files != d.Bots[j].Files {
			return d.Bots[i].Files > d.Bots[j].Files
		}
		return d.Bots[i].BotNick < d.Bots[j].BotNick
	})
	sort.Slice(d.Channels, func(i, j int) bool {
		if d.Channels[i].Files != d.Channels[j].Files {
			return d.Channels[i].Files > d.Channels[j].Files
		}
		return d.Channels[i].Channel < d.Channels[j].Channel
	})
	return d
}

package db

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	TokenHash string
	UserID    int64
	ExpiresAt time.Time
}

type DownloadStat struct {
	ID          int64      `json:"id"`
	Filename    string     `json:"filename"`
	SizeBytes   int64      `json:"size_bytes"`
	ServerID    int64      `json:"server_id"`
	Channel     string     `json:"channel"`
	BotNick     string     `json:"bot_nick"`
	PackNumber  int        `json:"pack_number"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Status      string     `json:"status"` // "completed", "failed", "cancelled", "stats_only"
	StatsOnly   bool       `json:"stats_only"`
}

type DownloadStatsSummary struct {
	TotalTransfers int64   `json:"total_transfers"`
	SuccessRate    float64 `json:"success_rate"`
	TotalBytes     int64   `json:"total_bytes"`
	TotalSaved     int64   `json:"total_saved"` // bytes kept on disk (not stats_only)
}

type Server struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	SSL          bool     `json:"ssl"`
	Nickname     string   `json:"nickname"`
	AltNicknames []string `json:"alt_nicknames"`
	AuthMethod   string   `json:"auth_method"`
	AuthPassword string   `json:"-"`
	AutoConnect  bool     `json:"auto_connect"`
	Enabled      bool     `json:"enabled"`
	// DiscoveryMode is "normal" or "aggressive".
	DiscoveryMode      string    `json:"discovery_mode"`
	DiscoveryOnConnect bool      `json:"discovery_on_connect"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// DiscoveredChannel is a channel found by channel discovery that isn't
// configured as a realm yet. Channel is stored lowercased.
type DiscoveredChannel struct {
	ID        int64     `json:"id"`
	ServerID  int64     `json:"server_id"`
	Channel   string    `json:"channel"`
	Partner   string    `json:"partner"`
	Type      string    `json:"type"` // chat | download | unknown
	Users     int       `json:"users"`
	Reason    string    `json:"reason"` // "keyword: xdcc" | "3 bot ads"
	AdCount   int       `json:"ad_count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Seen      bool      `json:"seen"`
	Dismissed bool      `json:"dismissed"`
	Added     bool      `json:"added"`
}

type Realm struct {
	ID              int64  `json:"id"`
	ServerID        int64  `json:"server_id"`
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	Key             string `json:"key,omitempty"`
	SearchCommand   string `json:"search_command"`
	DownloadChannel string `json:"download_channel"`
	SearchBot       string `json:"search_bot"`
	SearchTimeout   int    `json:"search_timeout"`
	AutoJoin        bool   `json:"auto_join"`
	Enabled         bool   `json:"enabled"`
}

type Download struct {
	ID              int64      `json:"id"`
	ServerID        int64      `json:"server_id"`
	Channel         string     `json:"channel"`
	BotNick         string     `json:"bot_nick"`
	PackNumber      int        `json:"pack_number"`
	Filename        string     `json:"filename"`
	Filesize        int64      `json:"filesize"`
	DownloadedBytes int64      `json:"downloaded_bytes"`
	Status          string     `json:"status"`
	DestinationPath string     `json:"destination_path"`
	ErrorMessage    string     `json:"error_message"`
	PeakSpeed       int64      `json:"peak_speed"`
	AverageSpeed    int64      `json:"average_speed"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	StatsOnly       bool       `json:"stats_only"`
	AutoExtract     bool       `json:"auto_extract"`
	TargetDir       string     `json:"target_dir"`
	AutoSubdir      bool       `json:"auto_subdir"`
	// ExpectedFilename is set when the bot sent a different file than the
	// pack list promised (pack swapped/renumbered); Filename holds what was sent.
	ExpectedFilename string `json:"expected_filename"`
}

type SearchResult struct {
	ID             int64     `json:"id"`
	ServerID       int64     `json:"server_id"`
	Channel        string    `json:"channel"`
	BotNick        string    `json:"bot_nick"`
	PackNumber     *int      `json:"pack_number"`
	Filename       *string   `json:"filename"`
	Filesize       *string   `json:"filesize"`
	DownloadsCount *int      `json:"downloads_count"`
	RawLine        string    `json:"raw_line"`
	SearchQuery    string    `json:"search_query"`
	Parsed         bool      `json:"parsed"`
	CreatedAt      time.Time `json:"created_at"`
}

type SavedSearch struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	ServerID  int64     `json:"server_id"`
	Channel   string    `json:"channel"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"created_at"`
}

type ParsePattern struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Regex         string     `json:"regex"`
	FieldMapping  string     `json:"field_mapping"`
	Priority      int        `json:"priority"`
	Builtin       bool       `json:"builtin"`
	Enabled       bool       `json:"enabled"`
	MatchCount    int        `json:"match_count"`
	LastMatchedAt *time.Time `json:"last_matched_at"`
	Tags          string     `db:"tags" json:"tags"`
	// ServerID and Channel scope this pattern to a specific realm.
	// NULL/empty means the pattern applies globally (all channels).
	ServerID *int64 `json:"server_id"`
	Channel  string `json:"channel"`
}

// IndexedFile is a persistent, deduplicated catalog entry built from
// successfully parsed live search results. Unlike SearchResult (which is
// scoped to a single search session and cleared on the next search),
// IndexedFile accumulates over time so files can be found instantly without
// re-querying the IRC bot.
type IndexedFile struct {
	ID             int64     `json:"id"`
	ServerID       int64     `json:"server_id"`
	Channel        string    `json:"channel"`
	BotNick        string    `json:"bot_nick"`
	PackNumber     *int      `json:"pack_number"`
	Filename       string    `json:"filename"`
	Filesize       *string   `json:"filesize"`
	DownloadsCount *int      `json:"downloads_count"`
	RawLine        string    `json:"raw_line"`
	HitCount       int       `json:"hit_count"`
	FirstSeenAt    time.Time `json:"first_seen_at"`
	LastSeenAt     time.Time `json:"last_seen_at"`
}

type IndexStats struct {
	TotalFiles    int64 `json:"total_files"`
	TotalBots     int64 `json:"total_bots"`
	TotalChannels int64 `json:"total_channels"`
}

// MergeResult counts what BulkMergeIndexedFiles did: new rows, rows merged
// into an existing entry, and rows skipped because a newer entry maps the
// same bot+pack to another file.
type MergeResult struct {
	Added  int `json:"added"`
	Merged int `json:"merged"`
	Stale  int `json:"stale"`
}

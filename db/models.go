package db

import "time"

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
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	SSL          bool      `json:"ssl"`
	Nickname     string    `json:"nickname"`
	AltNicknames []string  `json:"alt_nicknames"`
	AuthMethod   string    `json:"auth_method"`
	AuthPassword string    `json:"-"`
	AutoConnect  bool      `json:"auto_connect"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
	FailCount     int        `json:"fail_count"`
	LastMatchedAt *time.Time `json:"last_matched_at"`
	AutoDisabled  bool       `json:"auto_disabled"`
	Tags          string     `db:"tags" json:"tags"`
	// ServerID and Channel scope this pattern to a specific realm.
	// NULL/empty means the pattern applies globally (all channels).
	ServerID *int64 `json:"server_id"`
	Channel  string `json:"channel"`
}

type PostHook struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	ScopeID  *int64 `json:"scope_id"`
	HookType string `json:"hook_type"`
	Config   string `json:"config"`
	Enabled  bool   `json:"enabled"`
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

type FileRoutingRule struct {
	ID             int64  `json:"id"`
	Pattern        string `json:"pattern"`
	DestinationDir string `json:"destination_dir"`
	Priority       int    `json:"priority"`
	Builtin        bool   `json:"builtin"`
	Enabled        bool   `json:"enabled"`
}

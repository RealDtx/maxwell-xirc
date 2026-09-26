package db

import "time"

type Store interface {
	// Lifecycle
	Close() error
	Migrate() error

	// Servers
	GetServers() ([]Server, error)
	GetServer(id int64) (*Server, error)
	CreateServer(s *Server) error
	UpdateServer(s *Server) error
	DeleteServer(id int64) error

	// Realms
	GetRealms(serverID int64) ([]Realm, error)
	GetRealm(id int64) (*Realm, error)
	CreateRealm(r *Realm) error
	UpdateRealm(r *Realm) error
	DeleteRealm(id int64) error
	UpdateRealmSearchBot(id int64, botNick string) error

	// Downloads
	// GetDownloads returns downloads filtered by status, ordered by created_at DESC.
	// Pass an empty string to get all downloads regardless of status.
	// The queue package relies on DESC ordering and reverses for FIFO iteration.
	GetDownloads(status string) ([]Download, error)
	GetDownload(id int64) (*Download, error)
	CreateDownload(d *Download) error
	// UpdateDownload persists all fields of d, including created_at.
	// Writing created_at is intentional and required for MoveToFront queue reordering.
	UpdateDownload(d *Download) error
	// DeleteDownloads removes downloads by IDs. Only deletes terminal statuses
	// (completed, failed, cancelled). Returns the number of rows deleted.
	DeleteDownloads(ids []int64) (int64, error)
	// DeleteDownloadsByStatus removes all downloads matching the given terminal
	// status (completed, failed, cancelled). Returns the number of rows deleted.
	DeleteDownloadsByStatus(status string) (int64, error)

	// Search Results
	GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error)
	GetAllSearchResults(query string, since *time.Time) ([]SearchResult, error)
	CreateSearchResult(r *SearchResult) error
	DeleteSearchResults(serverID int64, channel string) error
	// GetUnparsedSearchSamples returns distinct raw_line samples for unmatched results.
	// Only returns results with RFC3339 timestamps (new-format entries).
	// Results are ordered by created_at DESC, limited to limit rows.
	GetUnparsedSearchSamples(serverID int64, query string, since time.Time, limit int) ([]SearchResult, error)
	// GetAllUnparsedSince returns all unmatched (parsed=0) results created after since.
	// Only returns results with RFC3339 timestamps (new-format entries).
	GetAllUnparsedSince(since time.Time) ([]SearchResult, error)
	// MarkSearchResultParsed updates a search result to parsed=1 with the extracted fields.
	MarkSearchResultParsed(id int64, botNick string, packNumber *int, filename *string, filesize *string, downloadsCount *int) error

	// Saved Searches
	GetSavedSearches() ([]SavedSearch, error)
	CreateSavedSearch(s *SavedSearch) error
	DeleteSavedSearch(id int64) error

	// Indexed Files (self-collected search index)
	// UpsertIndexedFile inserts or refreshes a persistent catalog entry.
	// Existing entries (matched by server_id+channel+bot_nick+filename) have
	// hit_count incremented and pack/size/downloads/last_seen_at refreshed;
	// otherwise a new row is created with hit_count=1. Entries for the same
	// server+bot+pack that carry a different filename are evicted first —
	// pack numbers rotate, so the latest advertisement wins.
	UpsertIndexedFile(f *IndexedFile) error
	// EvictStaleIndexedFiles deletes index entries for the given server+bot+pack
	// whose filename differs from keepFilename. keepFilename="" removes the
	// bot+pack entirely (e.g. after the bot reported an invalid pack number).
	EvictStaleIndexedFiles(serverID int64, botNick string, packNumber int, keepFilename string) error
	// SearchIndexedFiles performs an offline, case-insensitive AND-of-words
	// substring search over the persistent file index, ordered by
	// last_seen_at DESC. serverID=0 searches all servers; channel=""
	// searches all channels for the given server(s).
	SearchIndexedFiles(query string, serverID int64, channel string, limit int) ([]IndexedFile, error)
	// GetIndexStats returns the indexed file count, optionally scoped to a
	// single server (serverID=0 means all servers).
	GetIndexStats(serverID int64) (*IndexStats, error)
	// ClearIndex deletes indexed files, optionally scoped to a single server
	// (serverID=0 means all servers).
	ClearIndex(serverID int64) error
	// GetIndexStatsDetail returns per-channel and per-bot aggregates over
	// the persistent index, enriched with bandwidth observed during
	// completed downloads. Global (all servers); rows carry server_id.
	GetIndexStatsDetail() (*IndexStatsDetail, error)
	// PruneSearchResults deletes search_results rows older than olderThan.
	// This is a time-based safety net: results are normally cleared per
	// channel when a new search starts, but channels that stop being
	// searched would otherwise accumulate rows forever. Returns the number
	// of rows deleted.
	PruneSearchResults(olderThan time.Time) (int64, error)
	// EnforceIndexCap caps the persistent file index at maxFiles rows,
	// evicting the least-recently-seen entries first (ORDER BY last_seen_at
	// ASC) until the total is at or below the cap. maxFiles<=0 disables
	// enforcement. Returns the number of rows evicted.
	EnforceIndexCap(maxFiles int64) (int64, error)

	// Parse Patterns
	GetParsePatterns() ([]ParsePattern, error)
	GetAllParsePatterns() ([]ParsePattern, error)
	// GetParsePatternsForChannel returns global patterns plus any scoped to the given server+channel.
	GetParsePatternsForChannel(serverID int64, channel string) ([]ParsePattern, error)
	// RecordPatternMatch adds n to a pattern's match counter and stamps
	// last_matched_at. The parser batches calls (searches + passive indexing).
	RecordPatternMatch(patternID int64, n int, at time.Time) error
	UpdateParsePattern(p *ParsePattern) error
	CreateParsePattern(p *ParsePattern) error

	// Download Stats
	CreateDownloadStat(s *DownloadStat) error
	GetDownloadStatsSummary() (*DownloadStatsSummary, error)
	GetDownloadHistory(offset, limit int) ([]DownloadStat, error)
}

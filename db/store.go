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

	// Parse Patterns
	GetParsePatterns() ([]ParsePattern, error)
	GetAllParsePatterns() ([]ParsePattern, error)
	// GetParsePatternsForChannel returns global patterns plus any scoped to the given server+channel.
	GetParsePatternsForChannel(serverID int64, channel string) ([]ParsePattern, error)
	UpdateParsePattern(p *ParsePattern) error
	CreateParsePattern(p *ParsePattern) error

	// Post Hooks
	GetPostHooks(scope string, scopeID *int64) ([]PostHook, error)
	GetPostHookByID(id int64) (*PostHook, error)
	CreatePostHook(h *PostHook) error
	UpdatePostHook(h *PostHook) error
	DeletePostHook(id int64) error

	// File Routing Rules
	GetFileRoutingRules() ([]FileRoutingRule, error)
	GetAllFileRoutingRules() ([]FileRoutingRule, error)
	GetFileRoutingRuleByID(id int64) (*FileRoutingRule, error)
	CreateFileRoutingRule(r *FileRoutingRule) error
	UpdateFileRoutingRule(r *FileRoutingRule) error
	DeleteFileRoutingRule(id int64) error

	// Download Stats
	CreateDownloadStat(s *DownloadStat) error
	GetDownloadStatsSummary() (*DownloadStatsSummary, error)
	GetDownloadHistory(offset, limit int) ([]DownloadStat, error)
}

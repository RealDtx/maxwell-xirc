package db

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

	// Search Results
	GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error)
	GetAllSearchResults(query string) ([]SearchResult, error)
	CreateSearchResult(r *SearchResult) error

	// Saved Searches
	GetSavedSearches() ([]SavedSearch, error)
	CreateSavedSearch(s *SavedSearch) error
	DeleteSavedSearch(id int64) error

	// Parse Patterns
	GetParsePatterns() ([]ParsePattern, error)
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

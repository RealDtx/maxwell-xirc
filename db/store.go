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

	// Channels
	GetChannels(serverID int64) ([]Channel, error)
	GetChannel(id int64) (*Channel, error)
	CreateChannel(c *Channel) error
	UpdateChannel(c *Channel) error
	DeleteChannel(id int64) error

	// Downloads
	GetDownloads(status string) ([]Download, error)
	GetDownload(id int64) (*Download, error)
	CreateDownload(d *Download) error
	UpdateDownload(d *Download) error

	// Search Results
	GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error)
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
	CreatePostHook(h *PostHook) error
	UpdatePostHook(h *PostHook) error
	DeletePostHook(id int64) error

	// File Routing Rules
	GetFileRoutingRules() ([]FileRoutingRule, error)
	CreateFileRoutingRule(r *FileRoutingRule) error
	UpdateFileRoutingRule(r *FileRoutingRule) error
	DeleteFileRoutingRule(id int64) error
}

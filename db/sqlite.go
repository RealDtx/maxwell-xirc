package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Migrate() error {
	return runMigrations(s.db)
}

// --- Servers ---

func (s *SQLiteStore) GetServers() ([]Server, error) {
	rows, err := s.db.Query("SELECT id, name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := []Server{}
	for rows.Next() {
		var srv Server
		var altJSON string
		err := rows.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSL, &srv.Nickname,
			&altJSON, &srv.AuthMethod, &srv.AuthPassword, &srv.AutoConnect, &srv.Enabled,
			&srv.CreatedAt, &srv.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(altJSON), &srv.AltNicknames); err != nil {
			log.Printf("WARN: server %d has corrupt alt_nicknames JSON: %v", srv.ID, err)
			srv.AltNicknames = []string{}
		}
		servers = append(servers, srv)
	}
	return servers, rows.Err()
}

func (s *SQLiteStore) GetServer(id int64) (*Server, error) {
	var srv Server
	var altJSON string
	err := s.db.QueryRow(
		"SELECT id, name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers WHERE id=?", id,
	).Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSL, &srv.Nickname,
		&altJSON, &srv.AuthMethod, &srv.AuthPassword, &srv.AutoConnect, &srv.Enabled,
		&srv.CreatedAt, &srv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(altJSON), &srv.AltNicknames); err != nil {
		log.Printf("WARN: server %d has corrupt alt_nicknames JSON: %v", srv.ID, err)
		srv.AltNicknames = []string{}
	}
	return &srv, nil
}

func (s *SQLiteStore) CreateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO servers (name, host, port, ssl, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, now,
	)
	if err != nil {
		return err
	}
	srv.ID, _ = result.LastInsertId()
	srv.CreatedAt = now
	srv.UpdatedAt = now
	return nil
}

func (s *SQLiteStore) UpdateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	now := time.Now()
	_, err := s.db.Exec(
		`UPDATE servers SET name=?, host=?, port=?, ssl=?, nickname=?, alt_nicknames=?, auth_method=?, auth_password=?, auto_connect=?, enabled=?, updated_at=? WHERE id=?`,
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, srv.ID,
	)
	if err == nil {
		srv.UpdatedAt = now
	}
	return err
}

func (s *SQLiteStore) DeleteServer(id int64) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE id=?", id)
	return err
}

// --- Channels ---

func (s *SQLiteStore) GetChannels(serverID int64) ([]Channel, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, name, key, search_command, download_channel, auto_join, enabled FROM channels WHERE server_id=? ORDER BY name", serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := []Channel{}
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *SQLiteStore) GetChannel(id int64) (*Channel, error) {
	var ch Channel
	err := s.db.QueryRow(
		"SELECT id, server_id, name, key, search_command, download_channel, auto_join, enabled FROM channels WHERE id=?", id,
	).Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *SQLiteStore) CreateChannel(ch *Channel) error {
	result, err := s.db.Exec(
		`INSERT INTO channels (server_id, name, key, search_command, download_channel, auto_join, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ch.ServerID, ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled,
	)
	if err != nil {
		return err
	}
	ch.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateChannel(ch *Channel) error {
	_, err := s.db.Exec(
		`UPDATE channels SET name=?, key=?, search_command=?, download_channel=?, auto_join=?, enabled=? WHERE id=?`,
		ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled, ch.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteChannel(id int64) error {
	_, err := s.db.Exec("DELETE FROM channels WHERE id=?", id)
	return err
}

// --- Downloads ---

func (s *SQLiteStore) GetDownloads(status string) ([]Download, error) {
	var rows *sql.Rows
	var err error
	query := "SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads"
	if status != "" {
		rows, err = s.db.Query(query+" WHERE status=? ORDER BY created_at DESC", status)
	} else {
		rows, err = s.db.Query(query + " ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	downloads := []Download{}
	for rows.Next() {
		var dl Download
		if err := rows.Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
			&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
			&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
			&dl.CreatedAt); err != nil {
			return nil, err
		}
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

func (s *SQLiteStore) GetDownload(id int64) (*Download, error) {
	var dl Download
	err := s.db.QueryRow(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads WHERE id=?", id,
	).Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
		&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
		&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
		&dl.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &dl, nil
}

func (s *SQLiteStore) CreateDownload(dl *Download) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO downloads (server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dl.ServerID, dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, now,
	)
	if err != nil {
		return err
	}
	dl.ID, _ = result.LastInsertId()
	dl.CreatedAt = now
	return nil
}

func (s *SQLiteStore) UpdateDownload(dl *Download) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET channel=?, bot_nick=?, pack_number=?, filename=?, filesize=?, downloaded_bytes=?, status=?, destination_path=?, error_message=?, peak_speed=?, average_speed=?, started_at=?, completed_at=?, created_at=? WHERE id=?`,
		dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, dl.CreatedAt, dl.ID,
	)
	return err
}

// --- Search Results ---

func (s *SQLiteStore) GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND server_id=? AND channel=? ORDER BY created_at DESC",
		query, serverID, channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.ServerID, &r.Channel, &r.BotNick, &r.PackNumber,
			&r.Filename, &r.Filesize, &r.DownloadsCount, &r.RawLine, &r.SearchQuery,
			&r.Parsed, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) CreateSearchResult(r *SearchResult) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO search_results (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ServerID, r.Channel, r.BotNick, r.PackNumber, r.Filename, r.Filesize,
		r.DownloadsCount, r.RawLine, r.SearchQuery, r.Parsed, now,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	r.CreatedAt = now
	return nil
}

// --- Saved Searches ---

func (s *SQLiteStore) GetSavedSearches() ([]SavedSearch, error) {
	rows, err := s.db.Query("SELECT id, name, server_id, channel, query, created_at FROM saved_searches ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	searches := []SavedSearch{}
	for rows.Next() {
		var ss SavedSearch
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.ServerID, &ss.Channel, &ss.Query, &ss.CreatedAt); err != nil {
			return nil, err
		}
		searches = append(searches, ss)
	}
	return searches, rows.Err()
}

func (s *SQLiteStore) CreateSavedSearch(ss *SavedSearch) error {
	now := time.Now()
	result, err := s.db.Exec(
		"INSERT INTO saved_searches (name, server_id, channel, query, created_at) VALUES (?, ?, ?, ?, ?)",
		ss.Name, ss.ServerID, ss.Channel, ss.Query, now,
	)
	if err != nil {
		return err
	}
	ss.ID, _ = result.LastInsertId()
	ss.CreatedAt = now
	return nil
}

func (s *SQLiteStore) DeleteSavedSearch(id int64) error {
	_, err := s.db.Exec("DELETE FROM saved_searches WHERE id=?", id)
	return err
}

// --- Parse Patterns ---

func (s *SQLiteStore) GetParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT id, name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, last_matched_at, auto_disabled FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		var p ParsePattern
		if err := rows.Scan(&p.ID, &p.Name, &p.Regex, &p.FieldMapping, &p.Priority,
			&p.Builtin, &p.Enabled, &p.MatchCount, &p.FailCount, &p.LastMatchedAt,
			&p.AutoDisabled); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteStore) CreateParsePattern(p *ParsePattern) error {
	result, err := s.db.Exec(
		`INSERT INTO parse_patterns (name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, auto_disabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Builtin, p.Enabled,
		p.MatchCount, p.FailCount, p.AutoDisabled,
	)
	if err != nil {
		return err
	}
	p.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateParsePattern(p *ParsePattern) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET name=?, regex=?, field_mapping=?, priority=?, enabled=?, match_count=?, fail_count=?, last_matched_at=?, auto_disabled=? WHERE id=?`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Enabled,
		p.MatchCount, p.FailCount, p.LastMatchedAt, p.AutoDisabled, p.ID,
	)
	return err
}

// --- Post Hooks ---

func (s *SQLiteStore) GetPostHooks(scope string, scopeID *int64) ([]PostHook, error) {
	var rows *sql.Rows
	var err error
	if scope == "" {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 ORDER BY id")
	} else if scopeID != nil {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 AND scope=? AND scope_id=? ORDER BY id", scope, *scopeID)
	} else {
		rows, err = s.db.Query("SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE enabled=1 AND scope=? ORDER BY id", scope)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hooks := []PostHook{}
	for rows.Next() {
		var h PostHook
		if err := rows.Scan(&h.ID, &h.Name, &h.Scope, &h.ScopeID, &h.HookType, &h.Config, &h.Enabled); err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}
	return hooks, rows.Err()
}

func (s *SQLiteStore) GetPostHookByID(id int64) (*PostHook, error) {
	var h PostHook
	var scopeID sql.NullInt64
	err := s.db.QueryRow(
		"SELECT id, name, scope, scope_id, hook_type, config, enabled FROM post_hooks WHERE id=?", id,
	).Scan(&h.ID, &h.Name, &h.Scope, &scopeID, &h.HookType, &h.Config, &h.Enabled)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if scopeID.Valid {
		v := scopeID.Int64
		h.ScopeID = &v
	}
	return &h, nil
}

func (s *SQLiteStore) CreatePostHook(h *PostHook) error {
	result, err := s.db.Exec(
		"INSERT INTO post_hooks (name, scope, scope_id, hook_type, config, enabled) VALUES (?, ?, ?, ?, ?, ?)",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled,
	)
	if err != nil {
		return err
	}
	h.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdatePostHook(h *PostHook) error {
	_, err := s.db.Exec(
		"UPDATE post_hooks SET name=?, scope=?, scope_id=?, hook_type=?, config=?, enabled=? WHERE id=?",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled, h.ID,
	)
	return err
}

func (s *SQLiteStore) DeletePostHook(id int64) error {
	_, err := s.db.Exec("DELETE FROM post_hooks WHERE id=?", id)
	return err
}

// --- File Routing Rules ---

func (s *SQLiteStore) GetFileRoutingRules() ([]FileRoutingRule, error) {
	rows, err := s.db.Query("SELECT id, pattern, destination_dir, priority, builtin, enabled FROM file_routing_rules WHERE enabled=1 ORDER BY priority DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []FileRoutingRule{}
	for rows.Next() {
		var r FileRoutingRule
		if err := rows.Scan(&r.ID, &r.Pattern, &r.DestinationDir, &r.Priority, &r.Builtin, &r.Enabled); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *SQLiteStore) GetAllFileRoutingRules() ([]FileRoutingRule, error) {
	rows, err := s.db.Query("SELECT id, pattern, destination_dir, priority, builtin, enabled FROM file_routing_rules ORDER BY priority DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []FileRoutingRule{}
	for rows.Next() {
		var r FileRoutingRule
		if err := rows.Scan(&r.ID, &r.Pattern, &r.DestinationDir, &r.Priority, &r.Builtin, &r.Enabled); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *SQLiteStore) GetFileRoutingRuleByID(id int64) (*FileRoutingRule, error) {
	var r FileRoutingRule
	err := s.db.QueryRow(
		"SELECT id, pattern, destination_dir, priority, builtin, enabled FROM file_routing_rules WHERE id=?", id,
	).Scan(&r.ID, &r.Pattern, &r.DestinationDir, &r.Priority, &r.Builtin, &r.Enabled)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

func (s *SQLiteStore) CreateFileRoutingRule(r *FileRoutingRule) error {
	result, err := s.db.Exec(
		"INSERT INTO file_routing_rules (pattern, destination_dir, priority, builtin, enabled) VALUES (?, ?, ?, ?, ?)",
		r.Pattern, r.DestinationDir, r.Priority, r.Builtin, r.Enabled,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateFileRoutingRule(r *FileRoutingRule) error {
	_, err := s.db.Exec(
		"UPDATE file_routing_rules SET pattern=?, destination_dir=?, priority=?, enabled=? WHERE id=?",
		r.Pattern, r.DestinationDir, r.Priority, r.Enabled, r.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteFileRoutingRule(id int64) error {
	_, err := s.db.Exec("DELETE FROM file_routing_rules WHERE id=?", id)
	return err
}

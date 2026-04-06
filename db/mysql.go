package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// mysqlMigrationStatements returns MySQL-compatible CREATE TABLE statements.
// The only differences from SQLite: AUTO_INCREMENT instead of AUTOINCREMENT,
// TEXT types for JSON, DATETIME for timestamps, and explicit ENGINE=InnoDB.
func mysqlMigrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS servers (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			host VARCHAR(255) NOT NULL,
			port INT NOT NULL DEFAULT 6667,
			ssl BOOLEAN NOT NULL DEFAULT FALSE,
			nickname VARCHAR(255) NOT NULL DEFAULT 'xirc_user',
			alt_nicknames TEXT NOT NULL,
			auth_method VARCHAR(50) NOT NULL DEFAULT 'none',
			auth_password VARCHAR(255) NOT NULL DEFAULT '',
			auto_connect BOOLEAN NOT NULL DEFAULT TRUE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS channels (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			name VARCHAR(255) NOT NULL,
			` + "`key`" + ` VARCHAR(255) NOT NULL DEFAULT '',
			search_command VARCHAR(50) NOT NULL DEFAULT '!s',
			download_channel VARCHAR(255) NOT NULL DEFAULT '',
			auto_join BOOLEAN NOT NULL DEFAULT TRUE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS downloads (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			bot_nick VARCHAR(255) NOT NULL,
			pack_number INT NOT NULL,
			filename VARCHAR(500) NOT NULL DEFAULT '',
			filesize BIGINT NOT NULL DEFAULT 0,
			downloaded_bytes BIGINT NOT NULL DEFAULT 0,
			status VARCHAR(50) NOT NULL DEFAULT 'queued',
			destination_path VARCHAR(500) NOT NULL DEFAULT '',
			error_message TEXT NOT NULL,
			peak_speed BIGINT NOT NULL DEFAULT 0,
			average_speed BIGINT NOT NULL DEFAULT 0,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS search_results (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			bot_nick VARCHAR(255) NOT NULL,
			pack_number INT,
			filename VARCHAR(500),
			filesize VARCHAR(50),
			downloads_count INT,
			raw_line TEXT NOT NULL,
			search_query VARCHAR(255) NOT NULL,
			parsed BOOLEAN NOT NULL DEFAULT FALSE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS saved_searches (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			query VARCHAR(255) NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS parse_patterns (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			regex TEXT NOT NULL,
			field_mapping TEXT NOT NULL,
			priority INT NOT NULL DEFAULT 0,
			builtin BOOLEAN NOT NULL DEFAULT FALSE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			match_count INT NOT NULL DEFAULT 0,
			fail_count INT NOT NULL DEFAULT 0,
			last_matched_at DATETIME,
			auto_disabled BOOLEAN NOT NULL DEFAULT FALSE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS post_hooks (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			scope VARCHAR(50) NOT NULL DEFAULT 'global',
			scope_id BIGINT,
			hook_type VARCHAR(50) NOT NULL,
			config TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS file_routing_rules (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			pattern VARCHAR(255) NOT NULL,
			destination_dir VARCHAR(500) NOT NULL,
			priority INT NOT NULL DEFAULT 0,
			builtin BOOLEAN NOT NULL DEFAULT FALSE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
}

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(dsn string) (*MySQLStore, error) {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open("mysql", dsn+sep+"parseTime=true")
	if err != nil {
		return nil, fmt.Errorf("opening mysql: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging mysql: %w", err)
	}

	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) Close() error {
	return s.db.Close()
}

func (s *MySQLStore) Migrate() error {
	for _, stmt := range mysqlMigrationStatements() {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// --- Servers ---

func (s *MySQLStore) GetServers() ([]Server, error) {
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

func (s *MySQLStore) GetServer(id int64) (*Server, error) {
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

func (s *MySQLStore) CreateServer(srv *Server) error {
	altJSON, _ := json.Marshal(srv.AltNicknames)
	if srv.AltNicknames == nil {
		altJSON = []byte("[]")
	}
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

func (s *MySQLStore) UpdateServer(srv *Server) error {
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

func (s *MySQLStore) DeleteServer(id int64) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE id=?", id)
	return err
}

// --- Channels ---

func (s *MySQLStore) GetChannels(serverID int64) ([]Channel, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, name, `key`, search_command, download_channel, auto_join, enabled FROM channels WHERE server_id=? ORDER BY name", serverID,
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

func (s *MySQLStore) GetChannel(id int64) (*Channel, error) {
	var ch Channel
	err := s.db.QueryRow(
		"SELECT id, server_id, name, `key`, search_command, download_channel, auto_join, enabled FROM channels WHERE id=?", id,
	).Scan(&ch.ID, &ch.ServerID, &ch.Name, &ch.Key, &ch.SearchCommand, &ch.DownloadChannel, &ch.AutoJoin, &ch.Enabled)
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *MySQLStore) CreateChannel(ch *Channel) error {
	result, err := s.db.Exec(
		"INSERT INTO channels (server_id, name, `key`, search_command, download_channel, auto_join, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)",
		ch.ServerID, ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled,
	)
	if err != nil {
		return err
	}
	ch.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) UpdateChannel(ch *Channel) error {
	_, err := s.db.Exec(
		"UPDATE channels SET name=?, `key`=?, search_command=?, download_channel=?, auto_join=?, enabled=? WHERE id=?",
		ch.Name, ch.Key, ch.SearchCommand, ch.DownloadChannel, ch.AutoJoin, ch.Enabled, ch.ID,
	)
	return err
}

func (s *MySQLStore) DeleteChannel(id int64) error {
	_, err := s.db.Exec("DELETE FROM channels WHERE id=?", id)
	return err
}

// --- Downloads ---

func (s *MySQLStore) GetDownloads(status string) ([]Download, error) {
	query := "SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at FROM downloads"
	var rows *sql.Rows
	var err error
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

func (s *MySQLStore) GetDownload(id int64) (*Download, error) {
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

func (s *MySQLStore) CreateDownload(dl *Download) error {
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

func (s *MySQLStore) UpdateDownload(dl *Download) error {
	_, err := s.db.Exec(
		`UPDATE downloads SET channel=?, bot_nick=?, pack_number=?, filename=?, filesize=?, downloaded_bytes=?, status=?, destination_path=?, error_message=?, peak_speed=?, average_speed=?, started_at=?, completed_at=? WHERE id=?`,
		dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, dl.ID,
	)
	return err
}

// --- Search Results ---

func (s *MySQLStore) GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error) {
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

func (s *MySQLStore) CreateSearchResult(r *SearchResult) error {
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

func (s *MySQLStore) GetSavedSearches() ([]SavedSearch, error) {
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

func (s *MySQLStore) CreateSavedSearch(ss *SavedSearch) error {
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

func (s *MySQLStore) DeleteSavedSearch(id int64) error {
	_, err := s.db.Exec("DELETE FROM saved_searches WHERE id=?", id)
	return err
}

// --- Parse Patterns ---

func (s *MySQLStore) GetParsePatterns() ([]ParsePattern, error) {
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

func (s *MySQLStore) CreateParsePattern(p *ParsePattern) error {
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

func (s *MySQLStore) UpdateParsePattern(p *ParsePattern) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET name=?, regex=?, field_mapping=?, priority=?, enabled=?, match_count=?, fail_count=?, last_matched_at=?, auto_disabled=? WHERE id=?`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Enabled,
		p.MatchCount, p.FailCount, p.LastMatchedAt, p.AutoDisabled, p.ID,
	)
	return err
}

// --- Post Hooks ---

func (s *MySQLStore) GetPostHooks(scope string, scopeID *int64) ([]PostHook, error) {
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

func (s *MySQLStore) CreatePostHook(h *PostHook) error {
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

func (s *MySQLStore) UpdatePostHook(h *PostHook) error {
	_, err := s.db.Exec(
		"UPDATE post_hooks SET name=?, scope=?, scope_id=?, hook_type=?, config=?, enabled=? WHERE id=?",
		h.Name, h.Scope, h.ScopeID, h.HookType, h.Config, h.Enabled, h.ID,
	)
	return err
}

func (s *MySQLStore) DeletePostHook(id int64) error {
	_, err := s.db.Exec("DELETE FROM post_hooks WHERE id=?", id)
	return err
}

// --- File Routing Rules ---

func (s *MySQLStore) GetFileRoutingRules() ([]FileRoutingRule, error) {
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

func (s *MySQLStore) CreateFileRoutingRule(r *FileRoutingRule) error {
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

func (s *MySQLStore) UpdateFileRoutingRule(r *FileRoutingRule) error {
	_, err := s.db.Exec(
		"UPDATE file_routing_rules SET pattern=?, destination_dir=?, priority=?, enabled=? WHERE id=?",
		r.Pattern, r.DestinationDir, r.Priority, r.Enabled, r.ID,
	)
	return err
}

func (s *MySQLStore) DeleteFileRoutingRule(id int64) error {
	_, err := s.db.Exec("DELETE FROM file_routing_rules WHERE id=?", id)
	return err
}

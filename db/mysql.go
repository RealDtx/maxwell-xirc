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
			` + "`ssl`" + ` BOOLEAN NOT NULL DEFAULT FALSE,
			nickname VARCHAR(255) NOT NULL DEFAULT 'maxwell_user',
			alt_nicknames TEXT NOT NULL,
			auth_method VARCHAR(50) NOT NULL DEFAULT 'none',
			auth_password VARCHAR(255) NOT NULL DEFAULT '',
			auto_connect BOOLEAN NOT NULL DEFAULT TRUE,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS realms (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			name VARCHAR(255) NOT NULL,
			display_name VARCHAR(255) NOT NULL DEFAULT '',
			` + "`key`" + ` VARCHAR(255) NOT NULL DEFAULT '',
			search_command VARCHAR(50) NOT NULL DEFAULT '!s',
			download_channel VARCHAR(255) NOT NULL DEFAULT '',
			search_bot VARCHAR(255) NOT NULL DEFAULT '',
			search_timeout INT NOT NULL DEFAULT 10,
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
			auto_extract BOOLEAN NOT NULL DEFAULT TRUE,
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

		`CREATE TABLE IF NOT EXISTS download_stats (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    filename     VARCHAR(500) NOT NULL,
    size_bytes   BIGINT NOT NULL DEFAULT 0,
    server_id    BIGINT NOT NULL DEFAULT 0,
    channel      VARCHAR(255) NOT NULL DEFAULT '',
    bot_nick     VARCHAR(255) NOT NULL DEFAULT '',
    pack_number  INT NOT NULL DEFAULT 0,
    started_at   DATETIME,
    completed_at DATETIME,
    status       VARCHAR(50) NOT NULL DEFAULT 'completed',
    stats_only   BOOLEAN NOT NULL DEFAULT FALSE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`ALTER TABLE downloads ADD COLUMN stats_only BOOLEAN NOT NULL DEFAULT FALSE`,

		// Realm rename
		`ALTER TABLE channels RENAME TO realms`,
		`ALTER TABLE realms ADD COLUMN display_name VARCHAR(255) NOT NULL DEFAULT ''`,

		`ALTER TABLE realms ADD COLUMN search_bot VARCHAR(255) NOT NULL DEFAULT ''`,
		`ALTER TABLE realms ADD COLUMN search_timeout INT NOT NULL DEFAULT 10`,

		`ALTER TABLE parse_patterns ADD COLUMN tags TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE parse_patterns ADD COLUMN server_id INTEGER`,
		`ALTER TABLE parse_patterns ADD COLUMN channel VARCHAR(255) NOT NULL DEFAULT ''`,

		`ALTER TABLE downloads ADD COLUMN auto_extract BOOLEAN NOT NULL DEFAULT TRUE`,

		`ALTER TABLE downloads ADD COLUMN target_dir VARCHAR(500) NOT NULL DEFAULT ''`,
		`ALTER TABLE downloads ADD COLUMN auto_subdir BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE downloads ADD COLUMN subdir_depth INTEGER NOT NULL DEFAULT 3`,

		`CREATE TABLE IF NOT EXISTS indexed_files (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			channel VARCHAR(255) NOT NULL,
			bot_nick VARCHAR(255) NOT NULL,
			pack_number INT,
			filename VARCHAR(500) NOT NULL,
			filesize VARCHAR(50),
			downloads_count INT,
			raw_line TEXT NOT NULL,
			hit_count INT NOT NULL DEFAULT 1,
			first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id),
			UNIQUE KEY idx_indexed_files_unique (server_id, channel(50), bot_nick(50), filename(191)),
			KEY idx_indexed_files_filename (filename(191))
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE INDEX idx_search_results_scope ON search_results(server_id, channel, parsed)`,
		`CREATE INDEX idx_search_results_created_at ON search_results(created_at)`,

		// Supports stale-entry eviction by bot+pack on every broadcast upsert.
		`CREATE INDEX idx_indexed_files_bot_pack ON indexed_files(server_id, bot_nick(50), pack_number)`,

		// Speeds up the index-stats aggregation; prefix lengths keep the key
		// within InnoDB limits, still avoids the full-table sort.
		`CREATE INDEX idx_indexed_files_agg ON indexed_files(server_id, channel(50), bot_nick(50), filesize(20), last_seen_at)`,

		// FULLTEXT index for offline catalog search (requires MySQL 5.6+ /
		// MariaDB 10.0.5+ InnoDB fulltext support). Note: MySQL's default
		// fulltext stopword list and ft_min_word_len/innodb_ft_min_token_size
		// settings mean very short or common words (e.g. "the") may not be
		// indexed — acceptable for searching distinctive filenames/titles.
		`ALTER TABLE indexed_files ADD FULLTEXT INDEX idx_indexed_files_filename_ft (filename)`,
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
			msg := err.Error()
			// Duplicate column — ALTER ADD COLUMN on an already-migrated DB.
			if strings.Contains(msg, "uplicate column") {
				continue
			}
			// Table doesn't exist — historical rename/alter on a fresh install
			// where the old table (e.g. channels) was never created.
			if strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "Does not exist") {
				continue
			}
			// Table already exists — CREATE TABLE without IF NOT EXISTS guard,
			// or a rename target that already exists.
			if strings.Contains(msg, "already exists") || strings.Contains(msg, "Already exists") {
				continue
			}
			// Duplicate key/index name — CREATE INDEX has no IF NOT EXISTS
			// guard in standard MySQL, so reruns on an already-migrated DB
			// hit this instead.
			if strings.Contains(msg, "Duplicate key name") {
				continue
			}
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// --- Servers ---

func (s *MySQLStore) GetServers() ([]Server, error) {
	rows, err := s.db.Query("SELECT id, name, host, port, `ssl`, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers ORDER BY name")
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
		"SELECT id, name, host, port, `ssl`, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at FROM servers WHERE id=?", id,
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
		"INSERT INTO servers (name, host, port, `ssl`, nickname, alt_nicknames, auth_method, auth_password, auto_connect, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
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
		"UPDATE servers SET name=?, host=?, port=?, `ssl`=?, nickname=?, alt_nicknames=?, auth_method=?, auth_password=?, auto_connect=?, enabled=?, updated_at=? WHERE id=?",
		srv.Name, srv.Host, srv.Port, srv.SSL, srv.Nickname, string(altJSON),
		srv.AuthMethod, srv.AuthPassword, srv.AutoConnect, srv.Enabled, now, srv.ID,
	)
	if err == nil {
		srv.UpdatedAt = now
	}
	return err
}

// DeleteServer removes a server and all data scoped to it. Only realms
// cascade at the database level (ON DELETE CASCADE); downloads,
// search_results, saved_searches, indexed_files, and server-scoped parse
// patterns have no DB-level cascade, so they're deleted explicitly in a
// transaction before the server row itself is removed.
func (s *MySQLStore) DeleteServer(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"DELETE FROM downloads WHERE server_id=?",
		"DELETE FROM search_results WHERE server_id=?",
		"DELETE FROM saved_searches WHERE server_id=?",
		"DELETE FROM indexed_files WHERE server_id=?",
		"DELETE FROM parse_patterns WHERE server_id=?",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM servers WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Realms ---

func (s *MySQLStore) GetRealms(serverID int64) ([]Realm, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, name, display_name, `key`, search_command, download_channel, search_bot, search_timeout, auto_join, enabled FROM realms WHERE server_id=? ORDER BY name", serverID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	realms := []Realm{}
	for rows.Next() {
		var r Realm
		if err := rows.Scan(&r.ID, &r.ServerID, &r.Name, &r.DisplayName, &r.Key, &r.SearchCommand, &r.DownloadChannel, &r.SearchBot, &r.SearchTimeout, &r.AutoJoin, &r.Enabled); err != nil {
			return nil, err
		}
		realms = append(realms, r)
	}
	return realms, rows.Err()
}

func (s *MySQLStore) GetRealm(id int64) (*Realm, error) {
	var r Realm
	err := s.db.QueryRow(
		"SELECT id, server_id, name, display_name, `key`, search_command, download_channel, search_bot, search_timeout, auto_join, enabled FROM realms WHERE id=?", id,
	).Scan(&r.ID, &r.ServerID, &r.Name, &r.DisplayName, &r.Key, &r.SearchCommand, &r.DownloadChannel, &r.SearchBot, &r.SearchTimeout, &r.AutoJoin, &r.Enabled)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *MySQLStore) CreateRealm(r *Realm) error {
	result, err := s.db.Exec(
		"INSERT INTO realms (server_id, name, display_name, `key`, search_command, download_channel, search_bot, search_timeout, auto_join, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		r.ServerID, r.Name, r.DisplayName, r.Key, r.SearchCommand, r.DownloadChannel, r.SearchBot, r.SearchTimeout, r.AutoJoin, r.Enabled,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) UpdateRealm(r *Realm) error {
	_, err := s.db.Exec(
		"UPDATE realms SET name=?, display_name=?, `key`=?, search_command=?, download_channel=?, search_bot=?, search_timeout=?, auto_join=?, enabled=? WHERE id=?",
		r.Name, r.DisplayName, r.Key, r.SearchCommand, r.DownloadChannel, r.SearchBot, r.SearchTimeout, r.AutoJoin, r.Enabled, r.ID,
	)
	return err
}

func (s *MySQLStore) DeleteRealm(id int64) error {
	_, err := s.db.Exec("DELETE FROM realms WHERE id=?", id)
	return err
}

func (s *MySQLStore) UpdateRealmSearchBot(id int64, botNick string) error {
	_, err := s.db.Exec("UPDATE realms SET search_bot=? WHERE id=?", botNick, id)
	return err
}

// --- Downloads ---

func (s *MySQLStore) GetDownloads(status string) ([]Download, error) {
	query := "SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at, stats_only, auto_extract, target_dir, auto_subdir FROM downloads"
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
		var statsOnly int
		if err := rows.Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
			&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
			&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
			&dl.CreatedAt, &statsOnly, &dl.AutoExtract, &dl.TargetDir, &dl.AutoSubdir); err != nil {
			return nil, err
		}
		dl.StatsOnly = statsOnly == 1
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

func (s *MySQLStore) GetDownload(id int64) (*Download, error) {
	var dl Download
	var statsOnly int
	err := s.db.QueryRow(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at, stats_only, auto_extract, target_dir, auto_subdir FROM downloads WHERE id=?", id,
	).Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
		&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
		&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
		&dl.CreatedAt, &statsOnly, &dl.AutoExtract, &dl.TargetDir, &dl.AutoSubdir)
	if err != nil {
		return nil, err
	}
	dl.StatsOnly = statsOnly == 1
	return &dl, nil
}

func (s *MySQLStore) CreateDownload(dl *Download) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO downloads (server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at, auto_extract, target_dir, auto_subdir)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dl.ServerID, dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, now, dl.AutoExtract,
		dl.TargetDir, dl.AutoSubdir,
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
		`UPDATE downloads SET channel=?, bot_nick=?, pack_number=?, filename=?, filesize=?, downloaded_bytes=?, status=?, destination_path=?, error_message=?, peak_speed=?, average_speed=?, started_at=?, completed_at=?, created_at=?, auto_extract=?, target_dir=?, auto_subdir=? WHERE id=?`,
		dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, dl.CreatedAt, dl.AutoExtract,
		dl.TargetDir, dl.AutoSubdir, dl.ID,
	)
	return err
}

func (s *MySQLStore) DeleteDownloads(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := ""
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	result, err := s.db.Exec(
		"DELETE FROM downloads WHERE id IN ("+placeholders+") AND status IN ('completed','failed','cancelled')",
		args...,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *MySQLStore) DeleteDownloadsByStatus(status string) (int64, error) {
	if !terminalStatuses[status] {
		return 0, fmt.Errorf("cannot delete downloads with active status %q", status)
	}
	result, err := s.db.Exec("DELETE FROM downloads WHERE status=?", status)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// --- Search Results ---

func (s *MySQLStore) GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND server_id=? AND channel=? AND parsed=1 ORDER BY created_at DESC",
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

func (s *MySQLStore) GetAllSearchResults(query string, since *time.Time) ([]SearchResult, error) {
	var rows *sql.Rows
	var err error
	if since != nil {
		rows, err = s.db.Query(
			"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND parsed=1 AND created_at > ? ORDER BY created_at DESC LIMIT 500",
			query, *since,
		)
	} else {
		rows, err = s.db.Query(
			"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND parsed=1 ORDER BY created_at DESC LIMIT 500",
			query,
		)
	}
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
	// Trim and normalize fields before storage
	r.SearchQuery = strings.TrimSpace(r.SearchQuery)
	r.RawLine = strings.TrimSpace(r.RawLine)
	r.BotNick = strings.TrimSpace(r.BotNick)
	r.Channel = strings.TrimSpace(r.Channel)
	if r.Filename != nil {
		trimmed := strings.TrimSpace(*r.Filename)
		r.Filename = &trimmed
	}
	if r.Filesize != nil {
		trimmed := strings.TrimSpace(*r.Filesize)
		r.Filesize = &trimmed
	}

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

func (s *MySQLStore) DeleteSearchResults(serverID int64, channel string) error {
	_, err := s.db.Exec(
		`DELETE FROM search_results WHERE server_id=? AND channel=?`,
		serverID, channel,
	)
	return err
}

func (s *MySQLStore) GetUnparsedSearchSamples(serverID int64, query string, since time.Time, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT raw_line, bot_nick, MAX(created_at) as created_at
		FROM search_results
		WHERE search_query=? AND server_id=? AND parsed=0 AND created_at > ?
		GROUP BY raw_line
		ORDER BY created_at DESC
		LIMIT ?`,
		query, serverID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.RawLine, &r.BotNick, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *MySQLStore) GetAllUnparsedSince(since time.Time) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize,
		       downloads_count, raw_line, search_query, parsed, created_at
		FROM search_results
		WHERE parsed=0 AND created_at > ?`,
		since,
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

func (s *MySQLStore) MarkSearchResultParsed(id int64, botNick string, packNumber *int, filename *string, filesize *string, downloadsCount *int) error {
	_, err := s.db.Exec(
		`UPDATE search_results
		SET parsed=1, bot_nick=?, pack_number=?, filename=?, filesize=?, downloads_count=?
		WHERE id=?`,
		botNick, packNumber, filename, filesize, downloadsCount, id,
	)
	return err
}

// --- Indexed Files (self-collected search index) ---

func (s *MySQLStore) EvictStaleIndexedFiles(serverID int64, botNick string, packNumber int, keepFilename string) error {
	_, err := s.db.Exec(
		`DELETE FROM indexed_files WHERE server_id=? AND bot_nick=? AND pack_number=? AND filename<>?`,
		serverID, botNick, packNumber, keepFilename,
	)
	return err
}

func (s *MySQLStore) UpsertIndexedFile(f *IndexedFile) error {
	// Pack numbers rotate: the same bot re-uses #N for new content. Drop any
	// entry still claiming this bot+pack under an older filename.
	if f.PackNumber != nil {
		if err := s.EvictStaleIndexedFiles(f.ServerID, f.BotNick, *f.PackNumber, f.Filename); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO indexed_files (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, hit_count, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON DUPLICATE KEY UPDATE
			pack_number=VALUES(pack_number),
			filesize=VALUES(filesize),
			downloads_count=VALUES(downloads_count),
			raw_line=VALUES(raw_line),
			hit_count=hit_count+1,
			last_seen_at=VALUES(last_seen_at)`,
		f.ServerID, f.Channel, f.BotNick, f.PackNumber, f.Filename, f.Filesize,
		f.DownloadsCount, f.RawLine, now, now,
	)
	return err
}

// buildMySQLBooleanQuery turns a user query into a MySQL boolean-mode
// fulltext expression that requires every word to match as a prefix
// (leading "+" = required, trailing "*" = prefix truncation). Boolean-mode
// operator characters are stripped from each word first so arbitrary input
// can never be interpreted as query syntax.
func buildMySQLBooleanQuery(query string) string {
	words := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(words))
	for _, w := range words {
		cleaned := strings.Map(func(r rune) rune {
			switch r {
			case '+', '-', '<', '>', '(', ')', '~', '*', '"', '@':
				return -1
			}
			return r
		}, w)
		if cleaned == "" {
			continue
		}
		terms = append(terms, "+"+cleaned+"*")
	}
	return strings.Join(terms, " ")
}

func (s *MySQLStore) SearchIndexedFiles(query string, serverID int64, channel string, limit int) ([]IndexedFile, error) {
	booleanQuery := buildMySQLBooleanQuery(query)
	if booleanQuery == "" {
		return []IndexedFile{}, nil
	}

	conds := make([]string, 0, 2)
	args := make([]interface{}, 0, 4)
	args = append(args, booleanQuery)
	if serverID != 0 {
		conds = append(conds, "server_id=?")
		args = append(args, serverID)
	}
	if channel != "" {
		conds = append(conds, "channel=?")
		args = append(args, channel)
	}
	extra := ""
	if len(conds) > 0 {
		extra = "AND " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)

	rows, err := s.db.Query(
		`SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, hit_count, first_seen_at, last_seen_at
		FROM indexed_files
		WHERE MATCH(filename) AGAINST(? IN BOOLEAN MODE) `+extra+`
		ORDER BY last_seen_at DESC LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []IndexedFile{}
	for rows.Next() {
		var f IndexedFile
		if err := rows.Scan(&f.ID, &f.ServerID, &f.Channel, &f.BotNick, &f.PackNumber,
			&f.Filename, &f.Filesize, &f.DownloadsCount, &f.RawLine, &f.HitCount,
			&f.FirstSeenAt, &f.LastSeenAt); err != nil {
			return nil, err
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

func (s *MySQLStore) GetIndexStats(serverID int64) (*IndexStats, error) {
	var stats IndexStats
	var err error
	const q = "SELECT COUNT(*), COUNT(DISTINCT bot_nick), COUNT(DISTINCT channel) FROM indexed_files"
	if serverID != 0 {
		err = s.db.QueryRow(q+" WHERE server_id=?", serverID).Scan(&stats.TotalFiles, &stats.TotalBots, &stats.TotalChannels)
	} else {
		err = s.db.QueryRow(q).Scan(&stats.TotalFiles, &stats.TotalBots, &stats.TotalChannels)
	}
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

func (s *MySQLStore) ClearIndex(serverID int64) error {
	var err error
	if serverID != 0 {
		_, err = s.db.Exec("DELETE FROM indexed_files WHERE server_id=?", serverID)
	} else {
		_, err = s.db.Exec("DELETE FROM indexed_files")
	}
	return err
}

func (s *MySQLStore) GetIndexStatsDetail() (*IndexStatsDetail, error) {
	return queryIndexStatsDetail(s.db)
}

// PruneSearchResults is a time-based safety net for the ephemeral
// search_results cache (which is normally cleared per-channel on the next
// search, but channels that stop being searched would otherwise accumulate
// rows forever).
func (s *MySQLStore) PruneSearchResults(olderThan time.Time) (int64, error) {
	result, err := s.db.Exec("DELETE FROM search_results WHERE created_at < ?", olderThan)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// EnforceIndexCap evicts the least-recently-seen indexed_files rows until
// the total is at or below maxFiles. The subquery is wrapped in a derived
// table (`AS t`) because MySQL forbids selecting from the same table a
// DELETE targets directly in a subquery.
func (s *MySQLStore) EnforceIndexCap(maxFiles int64) (int64, error) {
	if maxFiles <= 0 {
		return 0, nil
	}
	var count int64
	if err := s.db.QueryRow("SELECT COUNT(*) FROM indexed_files").Scan(&count); err != nil {
		return 0, err
	}
	excess := count - maxFiles
	if excess <= 0 {
		return 0, nil
	}
	result, err := s.db.Exec(
		`DELETE FROM indexed_files WHERE id IN (
			SELECT id FROM (SELECT id FROM indexed_files ORDER BY last_seen_at ASC LIMIT ?) AS t
		)`,
		excess,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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

const mysqlPatternSelectCols = `id, name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, last_matched_at, auto_disabled, tags, server_id, channel`

func scanMySQLPattern(rows *sql.Rows) (ParsePattern, error) {
	var p ParsePattern
	err := rows.Scan(&p.ID, &p.Name, &p.Regex, &p.FieldMapping, &p.Priority,
		&p.Builtin, &p.Enabled, &p.MatchCount, &p.FailCount, &p.LastMatchedAt,
		&p.AutoDisabled, &p.Tags, &p.ServerID, &p.Channel)
	return p, err
}

func (s *MySQLStore) GetParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT " + mysqlPatternSelectCols + " FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		p, err := scanMySQLPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *MySQLStore) GetAllParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT " + mysqlPatternSelectCols + " FROM parse_patterns ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		p, err := scanMySQLPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *MySQLStore) GetParsePatternsForChannel(serverID int64, channel string) ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT "+mysqlPatternSelectCols+" FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 AND (server_id IS NULL OR channel='' OR (server_id=? AND channel=?)) ORDER BY priority DESC",
		serverID, channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		p, err := scanMySQLPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *MySQLStore) CreateParsePattern(p *ParsePattern) error {
	result, err := s.db.Exec(
		`INSERT INTO parse_patterns (name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, auto_disabled, tags, server_id, channel)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Builtin, p.Enabled,
		p.MatchCount, p.FailCount, p.AutoDisabled, p.Tags, p.ServerID, p.Channel,
	)
	if err != nil {
		return err
	}
	p.ID, _ = result.LastInsertId()
	return nil
}

func (s *MySQLStore) RecordPatternMatch(patternID int64) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET match_count=match_count+1, fail_count=0, last_matched_at=? WHERE id=?`,
		time.Now().UTC(), patternID,
	)
	return err
}

func (s *MySQLStore) UpdateParsePattern(p *ParsePattern) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET name=?, regex=?, field_mapping=?, priority=?, enabled=?, match_count=?, fail_count=?, last_matched_at=?, auto_disabled=?, tags=?, server_id=?, channel=? WHERE id=?`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Enabled,
		p.MatchCount, p.FailCount, p.LastMatchedAt, p.AutoDisabled, p.Tags, p.ServerID, p.Channel, p.ID,
	)
	return err
}

// --- Download Stats ---

func (s *MySQLStore) CreateDownloadStat(stat *DownloadStat) error {
	_, err := s.db.Exec(
		`INSERT INTO download_stats (filename,size_bytes,server_id,channel,bot_nick,pack_number,started_at,completed_at,status,stats_only)
         VALUES (?,?,?,?,?,?,?,?,?,?)`,
		stat.Filename, stat.SizeBytes, stat.ServerID, stat.Channel, stat.BotNick,
		stat.PackNumber, stat.StartedAt, stat.CompletedAt, stat.Status, boolToInt(stat.StatsOnly),
	)
	return err
}

func (s *MySQLStore) GetDownloadStatsSummary() (*DownloadStatsSummary, error) {
	row := s.db.QueryRow(`
        SELECT
            COUNT(*),
            COALESCE(SUM(CASE WHEN status='completed' OR status='stats_only' THEN 1 ELSE 0 END), 0),
            COALESCE(SUM(size_bytes),0),
            COALESCE(SUM(CASE WHEN stats_only=0 AND status='completed' THEN size_bytes ELSE 0 END),0)
        FROM download_stats`)
	var total, success int64
	var totalBytes, totalSaved int64
	if err := row.Scan(&total, &success, &totalBytes, &totalSaved); err != nil {
		return nil, err
	}
	var rate float64
	if total > 0 {
		rate = float64(success) / float64(total) * 100
	}
	return &DownloadStatsSummary{
		TotalTransfers: total,
		SuccessRate:    rate,
		TotalBytes:     totalBytes,
		TotalSaved:     totalSaved,
	}, nil
}

func (s *MySQLStore) GetDownloadHistory(offset, limit int) ([]DownloadStat, error) {
	rows, err := s.db.Query(
		`SELECT id,filename,size_bytes,server_id,channel,bot_nick,pack_number,started_at,completed_at,status,stats_only
         FROM download_stats ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DownloadStat
	for rows.Next() {
		var d DownloadStat
		var statsOnly int
		if err := rows.Scan(&d.ID, &d.Filename, &d.SizeBytes, &d.ServerID, &d.Channel,
			&d.BotNick, &d.PackNumber, &d.StartedAt, &d.CompletedAt, &d.Status, &statsOnly); err != nil {
			return nil, err
		}
		d.StatsOnly = statsOnly == 1
		out = append(out, d)
	}
	return out, rows.Err()
}

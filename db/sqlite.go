package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// rfc3339Fixed formats timestamps with exactly 9 fractional-second digits.
// Go's time.RFC3339Nano trims trailing zeros, producing variable-length
// fractions (.5Z vs .567011129Z). SQLite text comparison then breaks:
// ".567Z" < ".5Z" because 'Z'(90) > '6'(54) at the first differing position.
// Fixed-width fractions make the lexicographic comparison numerically correct.
const rfc3339Fixed = "2006-01-02T15:04:05.000000000Z"

var terminalStatuses = map[string]bool{"completed": true, "failed": true, "cancelled": true}

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
	if err := runMigrations(s.db); err != nil {
		return err
	}
	return s.backfillIndexedFilesFTS()
}

// backfillIndexedFilesFTS populates the indexed_files_fts index for rows
// that existed before the FTS5 table was introduced (or after a triggerless
// bulk insert). It uses the docsize shadow table to detect a truly empty
// index — querying the FTS5 table itself without MATCH transparently reads
// through to the content table via content_rowid, so it always looks
// "non-empty" even when the actual search index has never been built.
func (s *SQLiteStore) backfillIndexedFilesFTS() error {
	var docCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM indexed_files_fts_docsize").Scan(&docCount); err != nil {
		// Shadow table missing is not fatal — just skip backfill.
		return nil
	}
	if docCount > 0 {
		return nil
	}
	var contentCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM indexed_files").Scan(&contentCount); err != nil {
		return err
	}
	if contentCount == 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO indexed_files_fts(indexed_files_fts) VALUES('rebuild')`)
	return err
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

// DeleteServer removes a server and all data scoped to it. Only realms
// cascade at the database level (ON DELETE CASCADE); downloads,
// search_results, saved_searches, indexed_files, and server-scoped parse
// patterns have no DB-level cascade (to avoid a risky SQLite table rebuild
// on existing installs), so they're deleted explicitly in a transaction
// before the server row itself is removed.
func (s *SQLiteStore) DeleteServer(id int64) error {
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

func (s *SQLiteStore) GetRealms(serverID int64) ([]Realm, error) {
	rows, err := s.db.Query(
		"SELECT id, server_id, name, display_name, key, search_command, download_channel, search_bot, search_timeout, auto_join, enabled FROM realms WHERE server_id=? ORDER BY name", serverID,
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

func (s *SQLiteStore) GetRealm(id int64) (*Realm, error) {
	var r Realm
	err := s.db.QueryRow(
		"SELECT id, server_id, name, display_name, key, search_command, download_channel, search_bot, search_timeout, auto_join, enabled FROM realms WHERE id=?", id,
	).Scan(&r.ID, &r.ServerID, &r.Name, &r.DisplayName, &r.Key, &r.SearchCommand, &r.DownloadChannel, &r.SearchBot, &r.SearchTimeout, &r.AutoJoin, &r.Enabled)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *SQLiteStore) CreateRealm(r *Realm) error {
	result, err := s.db.Exec(
		`INSERT INTO realms (server_id, name, display_name, key, search_command, download_channel, search_bot, search_timeout, auto_join, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ServerID, r.Name, r.DisplayName, r.Key, r.SearchCommand, r.DownloadChannel, r.SearchBot, r.SearchTimeout, r.AutoJoin, r.Enabled,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	return nil
}

func (s *SQLiteStore) UpdateRealm(r *Realm) error {
	_, err := s.db.Exec(
		`UPDATE realms SET name=?, display_name=?, key=?, search_command=?, download_channel=?, search_bot=?, search_timeout=?, auto_join=?, enabled=? WHERE id=?`,
		r.Name, r.DisplayName, r.Key, r.SearchCommand, r.DownloadChannel, r.SearchBot, r.SearchTimeout, r.AutoJoin, r.Enabled, r.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteRealm(id int64) error {
	_, err := s.db.Exec("DELETE FROM realms WHERE id=?", id)
	return err
}

func (s *SQLiteStore) UpdateRealmSearchBot(id int64, botNick string) error {
	_, err := s.db.Exec("UPDATE realms SET search_bot=? WHERE id=?", botNick, id)
	return err
}

// --- Downloads ---

func (s *SQLiteStore) GetDownloads(status string) ([]Download, error) {
	var rows *sql.Rows
	var err error
	query := "SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at, stats_only, auto_extract FROM downloads"
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
		var autoExtract int
		if err := rows.Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
			&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
			&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
			&dl.CreatedAt, &statsOnly, &autoExtract); err != nil {
			return nil, err
		}
		dl.StatsOnly = statsOnly == 1
		dl.AutoExtract = autoExtract == 1
		downloads = append(downloads, dl)
	}
	return downloads, rows.Err()
}

func (s *SQLiteStore) GetDownload(id int64) (*Download, error) {
	var dl Download
	var statsOnly int
	var autoExtract int
	err := s.db.QueryRow(
		"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at, stats_only, auto_extract FROM downloads WHERE id=?", id,
	).Scan(&dl.ID, &dl.ServerID, &dl.Channel, &dl.BotNick, &dl.PackNumber,
		&dl.Filename, &dl.Filesize, &dl.DownloadedBytes, &dl.Status, &dl.DestinationPath,
		&dl.ErrorMessage, &dl.PeakSpeed, &dl.AverageSpeed, &dl.StartedAt, &dl.CompletedAt,
		&dl.CreatedAt, &statsOnly, &autoExtract)
	if err != nil {
		return nil, err
	}
	dl.StatsOnly = statsOnly == 1
	dl.AutoExtract = autoExtract == 1
	return &dl, nil
}

func (s *SQLiteStore) CreateDownload(dl *Download) error {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO downloads (server_id, channel, bot_nick, pack_number, filename, filesize, downloaded_bytes, status, destination_path, error_message, peak_speed, average_speed, started_at, completed_at, created_at, auto_extract)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dl.ServerID, dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, now, boolToInt(dl.AutoExtract),
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
		`UPDATE downloads SET channel=?, bot_nick=?, pack_number=?, filename=?, filesize=?, downloaded_bytes=?, status=?, destination_path=?, error_message=?, peak_speed=?, average_speed=?, started_at=?, completed_at=?, created_at=?, auto_extract=? WHERE id=?`,
		dl.Channel, dl.BotNick, dl.PackNumber, dl.Filename, dl.Filesize,
		dl.DownloadedBytes, dl.Status, dl.DestinationPath, dl.ErrorMessage,
		dl.PeakSpeed, dl.AverageSpeed, dl.StartedAt, dl.CompletedAt, dl.CreatedAt, boolToInt(dl.AutoExtract), dl.ID,
	)
	return err
}

func (s *SQLiteStore) DeleteDownloads(ids []int64) (int64, error) {
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

func (s *SQLiteStore) DeleteDownloadsByStatus(status string) (int64, error) {
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

func (s *SQLiteStore) GetSearchResults(query string, serverID int64, channel string) ([]SearchResult, error) {
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

func (s *SQLiteStore) GetAllSearchResults(query string, since *time.Time) ([]SearchResult, error) {
	var rows *sql.Rows
	var err error
	if since != nil {
		rows, err = s.db.Query(
			"SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at FROM search_results WHERE search_query=? AND parsed=1 AND created_at > ? ORDER BY created_at DESC LIMIT 500",
			query, since.UTC().Format(rfc3339Fixed),
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

func (s *SQLiteStore) CreateSearchResult(r *SearchResult) error {
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

	now := time.Now().UTC()
	nowStr := now.Format(rfc3339Fixed)
	result, err := s.db.Exec(
		`INSERT INTO search_results (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, search_query, parsed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ServerID, r.Channel, r.BotNick, r.PackNumber, r.Filename, r.Filesize,
		r.DownloadsCount, r.RawLine, r.SearchQuery, r.Parsed, nowStr,
	)
	if err != nil {
		return err
	}
	r.ID, _ = result.LastInsertId()
	r.CreatedAt = now
	return nil
}

func (s *SQLiteStore) DeleteSearchResults(serverID int64, channel string) error {
	_, err := s.db.Exec(
		`DELETE FROM search_results WHERE server_id=? AND channel=?`,
		serverID, channel,
	)
	return err
}

func (s *SQLiteStore) GetUnparsedSearchSamples(serverID int64, query string, since time.Time, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT raw_line, bot_nick, MAX(created_at) as created_at
		FROM search_results
		WHERE search_query=? AND server_id=? AND parsed=0
		  AND substr(created_at,11,1)='T'
		  AND created_at > ?
		GROUP BY raw_line
		ORDER BY created_at DESC
		LIMIT ?`,
		query, serverID, since.UTC().Format(rfc3339Fixed), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		var createdAt string
		if err := rows.Scan(&r.RawLine, &r.BotNick, &createdAt); err != nil {
			return nil, err
		}
		parsedTime, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			parsedTime, err = time.Parse(time.RFC3339, createdAt)
			if err != nil {
				return nil, err
			}
		}
		r.CreatedAt = parsedTime
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) GetAllUnparsedSince(since time.Time) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize,
		       downloads_count, raw_line, search_query, parsed, created_at
		FROM search_results
		WHERE parsed=0
		  AND substr(created_at,11,1)='T'
		  AND created_at > ?`,
		since.UTC().Format(rfc3339Fixed),
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

func (s *SQLiteStore) MarkSearchResultParsed(id int64, botNick string, packNumber *int, filename *string, filesize *string, downloadsCount *int) error {
	_, err := s.db.Exec(
		`UPDATE search_results
		SET parsed=1, bot_nick=?, pack_number=?, filename=?, filesize=?, downloads_count=?
		WHERE id=?`,
		botNick, packNumber, filename, filesize, downloadsCount, id,
	)
	return err
}

// --- Indexed Files (self-collected search index) ---

func (s *SQLiteStore) EvictStaleIndexedFiles(serverID int64, botNick string, packNumber int, keepFilename string) error {
	_, err := s.db.Exec(
		`DELETE FROM indexed_files WHERE server_id=? AND bot_nick=? AND pack_number=? AND filename<>?`,
		serverID, botNick, packNumber, keepFilename,
	)
	return err
}

func (s *SQLiteStore) UpsertIndexedFile(f *IndexedFile) error {
	// Pack numbers rotate: the same bot re-uses #N for new content. Drop any
	// entry still claiming this bot+pack under an older filename.
	if f.PackNumber != nil {
		if err := s.EvictStaleIndexedFiles(f.ServerID, f.BotNick, *f.PackNumber, f.Filename); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(rfc3339Fixed)
	_, err := s.db.Exec(
		`INSERT INTO indexed_files (server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count, raw_line, hit_count, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(server_id, channel, bot_nick, filename) DO UPDATE SET
			pack_number=excluded.pack_number,
			filesize=excluded.filesize,
			downloads_count=excluded.downloads_count,
			raw_line=excluded.raw_line,
			hit_count=hit_count+1,
			last_seen_at=excluded.last_seen_at`,
		f.ServerID, f.Channel, f.BotNick, f.PackNumber, f.Filename, f.Filesize,
		f.DownloadsCount, f.RawLine, now, now,
	)
	return err
}

// buildFTS5MatchQuery turns a user query into an FTS5 MATCH expression that
// requires every word to match as a prefix (implicit AND between terms).
// Each word is quoted as an FTS5 string literal (embedded quotes doubled) so
// arbitrary input can never be interpreted as FTS5 query syntax.
func buildFTS5MatchQuery(query string) string {
	words := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(words))
	for _, w := range words {
		escaped := strings.ReplaceAll(w, `"`, `""`)
		terms = append(terms, `"`+escaped+`"*`)
	}
	return strings.Join(terms, " ")
}

func (s *SQLiteStore) SearchIndexedFiles(query string, serverID int64, channel string, limit int) ([]IndexedFile, error) {
	matchQuery := buildFTS5MatchQuery(query)
	if matchQuery == "" {
		return []IndexedFile{}, nil
	}

	conds := make([]string, 0, 2)
	args := make([]interface{}, 0, 4)
	args = append(args, matchQuery)
	if serverID != 0 {
		conds = append(conds, "f.server_id=?")
		args = append(args, serverID)
	}
	if channel != "" {
		conds = append(conds, "f.channel=?")
		args = append(args, channel)
	}
	extra := ""
	if len(conds) > 0 {
		extra = "AND " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)

	rows, err := s.db.Query(
		`SELECT f.id, f.server_id, f.channel, f.bot_nick, f.pack_number, f.filename, f.filesize, f.downloads_count, f.raw_line, f.hit_count, f.first_seen_at, f.last_seen_at
		FROM indexed_files_fts fts
		JOIN indexed_files f ON f.id = fts.rowid
		WHERE indexed_files_fts MATCH ? `+extra+`
		ORDER BY f.last_seen_at DESC LIMIT ?`,
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

func (s *SQLiteStore) GetIndexStats(serverID int64) (*IndexStats, error) {
	var stats IndexStats
	var err error
	if serverID != 0 {
		err = s.db.QueryRow("SELECT COUNT(*) FROM indexed_files WHERE server_id=?", serverID).Scan(&stats.TotalFiles)
	} else {
		err = s.db.QueryRow("SELECT COUNT(*) FROM indexed_files").Scan(&stats.TotalFiles)
	}
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

func (s *SQLiteStore) ClearIndex(serverID int64) error {
	var err error
	if serverID != 0 {
		_, err = s.db.Exec("DELETE FROM indexed_files WHERE server_id=?", serverID)
	} else {
		_, err = s.db.Exec("DELETE FROM indexed_files")
	}
	return err
}

// PruneSearchResults is a time-based safety net for the ephemeral
// search_results cache (which is normally cleared per-channel on the next
// search, but channels that stop being searched would otherwise accumulate
// rows forever).
func (s *SQLiteStore) PruneSearchResults(olderThan time.Time) (int64, error) {
	result, err := s.db.Exec(
		"DELETE FROM search_results WHERE created_at < ?",
		olderThan.UTC().Format(rfc3339Fixed),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// EnforceIndexCap evicts the least-recently-seen indexed_files rows until
// the total is at or below maxFiles.
func (s *SQLiteStore) EnforceIndexCap(maxFiles int64) (int64, error) {
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
			SELECT id FROM indexed_files ORDER BY last_seen_at ASC LIMIT ?
		)`,
		excess,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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

const patternSelectCols = `id, name, regex, field_mapping, priority, builtin, enabled, match_count, fail_count, last_matched_at, auto_disabled, tags, server_id, channel`

func scanPattern(rows *sql.Rows) (ParsePattern, error) {
	var p ParsePattern
	err := rows.Scan(&p.ID, &p.Name, &p.Regex, &p.FieldMapping, &p.Priority,
		&p.Builtin, &p.Enabled, &p.MatchCount, &p.FailCount, &p.LastMatchedAt,
		&p.AutoDisabled, &p.Tags, &p.ServerID, &p.Channel)
	return p, err
}

func (s *SQLiteStore) GetParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT " + patternSelectCols + " FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		p, err := scanPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteStore) GetAllParsePatterns() ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT " + patternSelectCols + " FROM parse_patterns ORDER BY priority DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		p, err := scanPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteStore) GetParsePatternsForChannel(serverID int64, channel string) ([]ParsePattern, error) {
	rows, err := s.db.Query(
		"SELECT "+patternSelectCols+" FROM parse_patterns WHERE enabled=1 AND auto_disabled=0 AND (server_id IS NULL OR channel='' OR (server_id=? AND channel=?)) ORDER BY priority DESC",
		serverID, channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := []ParsePattern{}
	for rows.Next() {
		p, err := scanPattern(rows)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

func (s *SQLiteStore) CreateParsePattern(p *ParsePattern) error {
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

func (s *SQLiteStore) RecordPatternMatch(patternID int64) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET match_count=match_count+1, fail_count=0, last_matched_at=? WHERE id=?`,
		time.Now().UTC(), patternID,
	)
	return err
}

func (s *SQLiteStore) UpdateParsePattern(p *ParsePattern) error {
	_, err := s.db.Exec(
		`UPDATE parse_patterns SET name=?, regex=?, field_mapping=?, priority=?, enabled=?, match_count=?, fail_count=?, last_matched_at=?, auto_disabled=?, tags=?, server_id=?, channel=? WHERE id=?`,
		p.Name, p.Regex, p.FieldMapping, p.Priority, p.Enabled,
		p.MatchCount, p.FailCount, p.LastMatchedAt, p.AutoDisabled, p.Tags, p.ServerID, p.Channel, p.ID,
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

// --- Download Stats ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *SQLiteStore) CreateDownloadStat(stat *DownloadStat) error {
	_, err := s.db.Exec(
		`INSERT INTO download_stats (filename,size_bytes,server_id,channel,bot_nick,pack_number,started_at,completed_at,status,stats_only)
         VALUES (?,?,?,?,?,?,?,?,?,?)`,
		stat.Filename, stat.SizeBytes, stat.ServerID, stat.Channel, stat.BotNick,
		stat.PackNumber, stat.StartedAt, stat.CompletedAt, stat.Status, boolToInt(stat.StatsOnly),
	)
	return err
}

func (s *SQLiteStore) GetDownloadStatsSummary() (*DownloadStatsSummary, error) {
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

func (s *SQLiteStore) GetDownloadHistory(offset, limit int) ([]DownloadStat, error) {
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

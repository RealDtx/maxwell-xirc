package db

import (
	"database/sql"
	"strings"
	"time"
)

const indexPageSize = 5000

// forEachIndexedFile pages through indexed_files by id (keyset pagination);
// the SQL is identical for SQLite and MySQL.
func forEachIndexedFile(conn *sql.DB, serverIDs []int64, fn func(*IndexedFile) error) error {
	q := `WHERE id > ?`
	if len(serverIDs) > 0 {
		q += " AND server_id IN (?" + strings.Repeat(",?", len(serverIDs)-1) + ")"
	}
	q += " ORDER BY id LIMIT ?"
	var after int64
	for {
		args := []interface{}{after}
		for _, id := range serverIDs {
			args = append(args, id)
		}
		args = append(args, indexPageSize)
		page, err := queryIndexedFiles(conn, q, args...)
		if err != nil {
			return err
		}
		// Rows are released before fn runs, so a slow consumer (an HTTP
		// download) never holds a read transaction open.
		for i := range page {
			if err := fn(&page[i]); err != nil {
				return err
			}
		}
		if len(page) < indexPageSize {
			return nil
		}
		after = page[len(page)-1].ID
	}
}

// queryIndexedFiles returns the indexed_files rows matching where (a WHERE
// clause plus anything after it).
func queryIndexedFiles(conn *sql.DB, where string, args ...interface{}) ([]IndexedFile, error) {
	rows, err := conn.Query(`SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count,
		raw_line, hit_count, first_seen_at, last_seen_at FROM indexed_files `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IndexedFile
	for rows.Next() {
		var f IndexedFile
		if err := rows.Scan(&f.ID, &f.ServerID, &f.Channel, &f.BotNick, &f.PackNumber, &f.Filename,
			&f.Filesize, &f.DownloadsCount, &f.RawLine, &f.HitCount, &f.FirstSeenAt, &f.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// pruneIndexedFiles is shared by both stores; seenBefore is the dialect's
// "last_seen_at < ?" test (SQLite stores two text formats, see below).
func pruneIndexedFiles(conn *sql.DB, seenBefore string, serverID int64, channels []string, cutoff interface{}) (int64, error) {
	var lower []interface{}
	for _, c := range channels {
		if c != "" {
			lower = append(lower, strings.ToLower(c))
		}
	}
	if len(lower) == 0 {
		return 0, nil
	}
	args := append([]interface{}{serverID}, lower...)
	args = append(args, cutoff)
	res, err := conn.Exec(`DELETE FROM indexed_files WHERE server_id=? AND lower(channel) IN (?`+
		strings.Repeat(",?", len(lower)-1)+`) AND `+seenBefore, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SQLite rows carry either rfc3339Fixed text (upserts) or the column
// default "YYYY-MM-DD HH:MM:SS"; julianday() reads both.
func (s *SQLiteStore) PruneIndexedFiles(serverID int64, channels []string, olderThan time.Time) (int64, error) {
	return pruneIndexedFiles(s.db, "julianday(last_seen_at) < julianday(?)", serverID, channels, olderThan.UTC().Format(rfc3339Fixed))
}

func (s *MySQLStore) PruneIndexedFiles(serverID int64, channels []string, olderThan time.Time) (int64, error) {
	return pruneIndexedFiles(s.db, "last_seen_at < ?", serverID, channels, olderThan.UTC())
}

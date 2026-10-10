package db

import (
	"database/sql"
	"strings"
)

const indexPageSize = 5000

// forEachIndexedFile pages through indexed_files by id (keyset pagination);
// the SQL is identical for SQLite and MySQL.
func forEachIndexedFile(conn *sql.DB, serverIDs []int64, fn func(*IndexedFile) error) error {
	q := `SELECT id, server_id, channel, bot_nick, pack_number, filename, filesize, downloads_count,
		raw_line, hit_count, first_seen_at, last_seen_at FROM indexed_files WHERE id > ?`
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
		rows, err := conn.Query(q, args...)
		if err != nil {
			return err
		}
		var page []IndexedFile
		for rows.Next() {
			var f IndexedFile
			if err := rows.Scan(&f.ID, &f.ServerID, &f.Channel, &f.BotNick, &f.PackNumber, &f.Filename,
				&f.Filesize, &f.DownloadsCount, &f.RawLine, &f.HitCount, &f.FirstSeenAt, &f.LastSeenAt); err != nil {
				rows.Close()
				return err
			}
			page = append(page, f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
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

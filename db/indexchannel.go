package db

import (
	"database/sql"
	"log"
)

type queryRower interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// indexChannel files a realm's chat channel under its download channel.
// Search-bot replies arrive where the search was sent (the chat channel),
// announcements in the download channel; without this the same pack would
// be two index rows. Other channels are returned unchanged.
func indexChannel(q queryRower, serverID int64, channel string) (string, error) {
	var dl string
	err := q.QueryRow(`SELECT download_channel FROM realms
		WHERE server_id=? AND LOWER(name)=LOWER(?) AND download_channel<>'' LIMIT 1`, serverID, channel).Scan(&dl)
	if err == sql.ErrNoRows {
		return channel, nil
	}
	return dl, err
}

// mergeChatChannelRows moves index rows filed under a realm's chat channel
// (from before indexChannel existed) into its download channel, merging
// duplicates via merge (which applies indexChannel). Idempotent: once
// merged there is nothing left to move, so it's cheap to run every start.
func mergeChatChannelRows(conn *sql.DB, merge func([]IndexedFile) (MergeResult, error)) {
	rows, err := conn.Query(`SELECT server_id, name FROM realms
		WHERE download_channel<>'' AND LOWER(name)<>LOWER(download_channel)`)
	if err != nil {
		log.Printf("index: listing realms for chat-channel merge: %v", err)
		return
	}
	type chat struct {
		serverID int64
		name     string
	}
	var chats []chat
	for rows.Next() {
		var c chat
		if err := rows.Scan(&c.serverID, &c.name); err == nil {
			chats = append(chats, c)
		}
	}
	rows.Close()

	for _, c := range chats {
		files, err := queryIndexedFiles(conn, `WHERE server_id=? AND LOWER(channel)=LOWER(?)`, c.serverID, c.name)
		if err != nil || len(files) == 0 {
			continue
		}
		if _, err := merge(files); err != nil {
			log.Printf("index: merging %d row(s) of %s: %v", len(files), c.name, err)
			continue
		}
		if _, err := conn.Exec(`DELETE FROM indexed_files WHERE server_id=? AND LOWER(channel)=LOWER(?)`, c.serverID, c.name); err != nil {
			log.Printf("index: removing merged rows of %s: %v", c.name, err)
			continue
		}
		log.Printf("index: merged %d row(s) from chat channel %s into its download channel", len(files), c.name)
	}
}

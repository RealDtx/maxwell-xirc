package db

import (
	"database/sql"
	"strings"
)

func migrationStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 6667,
			ssl INTEGER NOT NULL DEFAULT 0,
			nickname TEXT NOT NULL DEFAULT 'xirc_user',
			alt_nicknames TEXT NOT NULL DEFAULT '[]',
			auth_method TEXT NOT NULL DEFAULT 'none',
			auth_password TEXT NOT NULL DEFAULT '',
			auto_connect INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS realms (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			key TEXT NOT NULL DEFAULT '',
			search_command TEXT NOT NULL DEFAULT '!s',
			download_channel TEXT NOT NULL DEFAULT '',
			search_bot TEXT NOT NULL DEFAULT '',
			search_timeout INTEGER NOT NULL DEFAULT 10,
			auto_join INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS downloads (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			bot_nick TEXT NOT NULL,
			pack_number INTEGER NOT NULL,
			filename TEXT NOT NULL DEFAULT '',
			filesize INTEGER NOT NULL DEFAULT 0,
			downloaded_bytes INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'queued',
			destination_path TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			peak_speed INTEGER NOT NULL DEFAULT 0,
			average_speed INTEGER NOT NULL DEFAULT 0,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		)`,

		`CREATE TABLE IF NOT EXISTS search_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			bot_nick TEXT NOT NULL,
			pack_number INTEGER,
			filename TEXT,
			filesize TEXT,
			downloads_count INTEGER,
			raw_line TEXT NOT NULL,
			search_query TEXT NOT NULL,
			parsed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		)`,

		`CREATE TABLE IF NOT EXISTS saved_searches (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			query TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id)
		)`,

		`CREATE TABLE IF NOT EXISTS parse_patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			regex TEXT NOT NULL,
			field_mapping TEXT NOT NULL DEFAULT '{}',
			priority INTEGER NOT NULL DEFAULT 0,
			builtin INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			match_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			last_matched_at DATETIME,
			auto_disabled INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS post_hooks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'global',
			scope_id INTEGER,
			hook_type TEXT NOT NULL,
			config TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1
		)`,

		`CREATE TABLE IF NOT EXISTS file_routing_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			destination_dir TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			builtin INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1
		)`,

		`CREATE TABLE IF NOT EXISTS download_stats (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    filename     TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    server_id    INTEGER NOT NULL DEFAULT 0,
    channel      TEXT NOT NULL DEFAULT '',
    bot_nick     TEXT NOT NULL DEFAULT '',
    pack_number  INTEGER NOT NULL DEFAULT 0,
    started_at   DATETIME,
    completed_at DATETIME,
    status       TEXT NOT NULL DEFAULT 'completed',
    stats_only   INTEGER NOT NULL DEFAULT 0
)`,

		`ALTER TABLE downloads ADD COLUMN stats_only INTEGER NOT NULL DEFAULT 0`,

		`UPDATE realms SET enabled=1 WHERE enabled=0`,

		// Realm rename: channels → realms
		`ALTER TABLE channels RENAME TO realms`,
		`ALTER TABLE realms ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,

		`ALTER TABLE realms ADD COLUMN search_bot TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE realms ADD COLUMN search_timeout INTEGER NOT NULL DEFAULT 10`,

		`ALTER TABLE parse_patterns ADD COLUMN tags TEXT NOT NULL DEFAULT ''`,
	}
}

func runMigrations(db *sql.DB) error {
	for _, stmt := range migrationStatements() {
		if _, err := db.Exec(stmt); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "Duplicate column") {
				continue
			}
			// On fresh installs the old `channels` table never existed; skip.
			// On already-migrated DBs, renaming channels→realms fails because
			// realms already exists; skip that too.
			if strings.Contains(msg, "no such table") ||
				strings.Contains(msg, "already another table") ||
				strings.Contains(msg, "already exists") {
				continue
			}
			return err
		}
	}
	return nil
}

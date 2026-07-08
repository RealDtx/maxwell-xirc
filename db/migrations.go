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
			nickname TEXT NOT NULL DEFAULT 'maxwell_user',
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
		`ALTER TABLE parse_patterns ADD COLUMN server_id INTEGER`,
		`ALTER TABLE parse_patterns ADD COLUMN channel TEXT NOT NULL DEFAULT ''`,

		`ALTER TABLE downloads ADD COLUMN auto_extract INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE downloads ADD COLUMN auto_extract BOOLEAN NOT NULL DEFAULT TRUE`,

		`CREATE TABLE IF NOT EXISTS indexed_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			channel TEXT NOT NULL,
			bot_nick TEXT NOT NULL,
			pack_number INTEGER,
			filename TEXT NOT NULL,
			filesize TEXT,
			downloads_count INTEGER,
			raw_line TEXT NOT NULL,
			hit_count INTEGER NOT NULL DEFAULT 1,
			first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (server_id) REFERENCES servers(id),
			UNIQUE (server_id, channel, bot_nick, filename)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_indexed_files_filename ON indexed_files(filename)`,
		// Supports stale-entry eviction by bot+pack on every broadcast upsert.
		`CREATE INDEX IF NOT EXISTS idx_indexed_files_bot_pack ON indexed_files(server_id, bot_nick, pack_number)`,
		// Covering index for the index-stats aggregation (GROUP BY server,
		// channel, bot, filesize + MAX(last_seen_at)) — index-only scan
		// instead of full scan + temp sort (~10s -> ~1s at 200K rows on a Pi).
		`CREATE INDEX IF NOT EXISTS idx_indexed_files_agg ON indexed_files(server_id, channel, bot_nick, filesize, last_seen_at)`,

		`CREATE INDEX IF NOT EXISTS idx_search_results_scope ON search_results(server_id, channel, parsed)`,
		`CREATE INDEX IF NOT EXISTS idx_search_results_created_at ON search_results(created_at)`,

		// FTS5 full-text index over indexed_files.filename, kept in sync via
		// triggers so application code never has to remember to dual-write.
		// external content ("content='indexed_files'") avoids duplicating the
		// filename text on disk.
		`CREATE VIRTUAL TABLE IF NOT EXISTS indexed_files_fts USING fts5(filename, content='indexed_files', content_rowid='id')`,
		`CREATE TRIGGER IF NOT EXISTS indexed_files_ai AFTER INSERT ON indexed_files BEGIN
			INSERT INTO indexed_files_fts(rowid, filename) VALUES (new.id, new.filename);
		END`,
		`CREATE TRIGGER IF NOT EXISTS indexed_files_ad AFTER DELETE ON indexed_files BEGIN
			INSERT INTO indexed_files_fts(indexed_files_fts, rowid, filename) VALUES('delete', old.id, old.filename);
		END`,
		`CREATE TRIGGER IF NOT EXISTS indexed_files_au AFTER UPDATE ON indexed_files BEGIN
			INSERT INTO indexed_files_fts(indexed_files_fts, rowid, filename) VALUES('delete', old.id, old.filename);
			INSERT INTO indexed_files_fts(rowid, filename) VALUES (new.id, new.filename);
		END`,
		// One-time backfill for rows created before the FTS table existed
		// is handled in SQLiteStore.Migrate() via the FTS5 'rebuild' command
		// (see backfillIndexedFilesFTS) — a plain INSERT...SELECT guarded by
		// "does the FTS table have rows" doesn't work here because querying
		// an external-content FTS5 table without MATCH transparently passes
		// through to the content table, so it always looks non-empty.
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

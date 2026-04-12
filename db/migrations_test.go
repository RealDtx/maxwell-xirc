package db

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// isMigrationSkippable returns true for errors that runMigrations silently ignores,
// such as "duplicate column" (ALTER ADD COLUMN on a fresh schema) and
// "no such table" (ALTER TABLE channels RENAME TO realms on a fresh install
// where the table was already created as realms).
func isMigrationSkippable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "Duplicate column") ||
		strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "already another table") ||
		strings.Contains(msg, "already exists")
}

func TestMigrationSQL_IsValid(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	defer db.Close()

	for i, stmt := range migrationStatements() {
		if _, err := db.Exec(stmt); err != nil {
			if isMigrationSkippable(err) {
				continue
			}
			t.Fatalf("migration statement %d failed: %v\nSQL: %s", i, err, stmt)
		}
	}
}

func TestMigrationSQL_CreatesAllTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	defer db.Close()

	for _, stmt := range migrationStatements() {
		if _, err := db.Exec(stmt); err != nil && !isMigrationSkippable(err) {
			t.Fatalf("migration failed: %v", err)
		}
	}

	expectedTables := []string{
		"servers", "realms", "downloads", "search_results",
		"saved_searches", "parse_patterns", "post_hooks", "file_routing_rules",
	}

	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist, but it doesn't", table)
		}
	}
}

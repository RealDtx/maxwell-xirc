package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// Shared discovered_channels SQL — identical for SQLite and MySQL except the
// upsert conflict clause (like users.go).

const (
	sqliteDiscoveredConflict = `ON CONFLICT(server_id, channel) DO UPDATE SET
		partner=CASE WHEN excluded.partner<>'' THEN excluded.partner ELSE partner END,
		type=excluded.type, users=excluded.users, reason=excluded.reason,
		ad_count=excluded.ad_count, last_seen=excluded.last_seen`
	mysqlDiscoveredConflict = `ON DUPLICATE KEY UPDATE
		partner=IF(VALUES(partner)<>'', VALUES(partner), partner),
		type=VALUES(type), users=VALUES(users), reason=VALUES(reason),
		ad_count=VALUES(ad_count), last_seen=VALUES(last_seen)`
)

func upsertDiscovered(db *sql.DB, conflict string, d *DiscoveredChannel) error {
	d.Channel = strings.ToLower(d.Channel)
	_, err := db.Exec(`INSERT INTO discovered_channels
		(server_id, channel, partner, type, users, reason, ad_count, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) `+conflict,
		d.ServerID, d.Channel, d.Partner, d.Type, d.Users, d.Reason, d.AdCount, d.FirstSeen, d.LastSeen)
	return err
}

func getDiscovered(db *sql.DB, serverID int64) ([]DiscoveredChannel, error) {
	rows, err := db.Query(`SELECT id, server_id, channel, partner, type, users, reason, ad_count,
		first_seen, last_seen, seen, dismissed, added FROM discovered_channels
		WHERE server_id=? AND dismissed=FALSE AND added=FALSE ORDER BY last_seen DESC, id DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscoveredChannel{}
	for rows.Next() {
		var d DiscoveredChannel
		if err := rows.Scan(&d.ID, &d.ServerID, &d.Channel, &d.Partner, &d.Type, &d.Users, &d.Reason,
			&d.AdCount, &d.FirstSeen, &d.LastSeen, &d.Seen, &d.Dismissed, &d.Added); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func setDiscoveredFlag(db *sql.DB, id int64, flag string) error {
	if flag != "dismissed" && flag != "added" {
		return fmt.Errorf("invalid discovered flag %q", flag)
	}
	_, err := db.Exec("UPDATE discovered_channels SET "+flag+"=TRUE WHERE id=?", id)
	return err
}

func markDiscoveredSeen(db *sql.DB, serverID int64) error {
	_, err := db.Exec("UPDATE discovered_channels SET seen=TRUE WHERE server_id=?", serverID)
	return err
}

func countUnseenDiscovered(db *sql.DB) (map[int64]int, error) {
	rows, err := db.Query(`SELECT server_id, COUNT(*) FROM discovered_channels
		WHERE seen=FALSE AND dismissed=FALSE AND added=FALSE GROUP BY server_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		m[id] = n
	}
	return m, rows.Err()
}

// --- Channel discovery (SQL shared in discovery.go) ---

func (s *SQLiteStore) UpsertDiscoveredChannel(d *DiscoveredChannel) error {
	return upsertDiscovered(s.db, sqliteDiscoveredConflict, d)
}
func (s *SQLiteStore) GetDiscoveredChannels(id int64) ([]DiscoveredChannel, error) {
	return getDiscovered(s.db, id)
}
func (s *SQLiteStore) SetDiscoveredFlag(id int64, flag string) error {
	return setDiscoveredFlag(s.db, id, flag)
}
func (s *SQLiteStore) MarkDiscoveredSeen(id int64) error { return markDiscoveredSeen(s.db, id) }
func (s *SQLiteStore) CountUnseenDiscovered() (map[int64]int, error) {
	return countUnseenDiscovered(s.db)
}

func (s *MySQLStore) UpsertDiscoveredChannel(d *DiscoveredChannel) error {
	return upsertDiscovered(s.db, mysqlDiscoveredConflict, d)
}
func (s *MySQLStore) GetDiscoveredChannels(id int64) ([]DiscoveredChannel, error) {
	return getDiscovered(s.db, id)
}
func (s *MySQLStore) SetDiscoveredFlag(id int64, flag string) error {
	return setDiscoveredFlag(s.db, id, flag)
}
func (s *MySQLStore) MarkDiscoveredSeen(id int64) error { return markDiscoveredSeen(s.db, id) }
func (s *MySQLStore) CountUnseenDiscovered() (map[int64]int, error) {
	return countUnseenDiscovered(s.db)
}

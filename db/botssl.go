package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

func getBotSSL(db *sql.DB, serverID int64, nick string) (bool, bool, error) {
	var ssl bool
	err := db.QueryRow(`SELECT supports_ssl FROM bot_ssl WHERE server_id=? AND bot_nick=?`, serverID, strings.ToLower(nick)).Scan(&ssl)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return ssl, err == nil, err
}

func setBotSSL(db *sql.DB, conflict string, serverID int64, nick string, ssl bool) error {
	_, err := db.Exec(`INSERT INTO bot_ssl (server_id, bot_nick, supports_ssl, updated_at) VALUES (?, ?, ?, ?) `+conflict,
		serverID, strings.ToLower(nick), ssl, time.Now())
	return err
}

func (s *SQLiteStore) GetBotSSL(serverID int64, nick string) (bool, bool, error) {
	return getBotSSL(s.db, serverID, nick)
}
func (s *SQLiteStore) SetBotSSL(serverID int64, nick string, ssl bool) error {
	return setBotSSL(s.db, `ON CONFLICT(server_id, bot_nick) DO UPDATE SET supports_ssl=excluded.supports_ssl, updated_at=excluded.updated_at`, serverID, nick, ssl)
}
func (s *MySQLStore) GetBotSSL(serverID int64, nick string) (bool, bool, error) {
	return getBotSSL(s.db, serverID, nick)
}
func (s *MySQLStore) SetBotSSL(serverID int64, nick string, ssl bool) error {
	return setBotSSL(s.db, `ON DUPLICATE KEY UPDATE supports_ssl=VALUES(supports_ssl), updated_at=VALUES(updated_at)`, serverID, nick, ssl)
}

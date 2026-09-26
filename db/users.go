package db

import (
	"database/sql"
	"errors"
	"time"
)

// Shared users/sessions SQL — identical for SQLite and MySQL (like indexstats.go).
// Times are unix seconds.

const userCols = "id, username, password_hash, role, created_at"

func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	var created int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

func createUser(db *sql.DB, u *User) error {
	res, err := db.Exec("INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		u.Username, u.PasswordHash, u.Role, u.CreatedAt.Unix())
	if err != nil {
		return err
	}
	u.ID, err = res.LastInsertId()
	return err
}

func getUser(db *sql.DB, id int64) (*User, error) {
	return scanUser(db.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func getUserByName(db *sql.DB, name string) (*User, error) {
	return scanUser(db.QueryRow("SELECT "+userCols+" FROM users WHERE username = ?", name))
}

func listUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query("SELECT " + userCols + " FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func updateUser(db *sql.DB, u *User) error {
	_, err := db.Exec("UPDATE users SET username = ?, password_hash = ?, role = ? WHERE id = ?",
		u.Username, u.PasswordHash, u.Role, u.ID)
	return err
}

func deleteUser(db *sql.DB, id int64) error {
	// Explicit session delete: don't rely on FK enforcement being on.
	if _, err := db.Exec("DELETE FROM sessions WHERE user_id = ?", id); err != nil {
		return err
	}
	_, err := db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func countUsers(db *sql.DB, where string, args ...interface{}) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM users"+where, args...).Scan(&n)
	return n, err
}

func createSession(db *sql.DB, s *Session) error {
	_, err := db.Exec("INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)",
		s.TokenHash, s.UserID, s.ExpiresAt.Unix())
	return err
}

func getSession(db *sql.DB, tokenHash string) (*Session, error) {
	var s Session
	var exp int64
	err := db.QueryRow("SELECT token_hash, user_id, expires_at FROM sessions WHERE token_hash = ?", tokenHash).
		Scan(&s.TokenHash, &s.UserID, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.ExpiresAt = time.Unix(exp, 0)
	return &s, nil
}

func touchSession(db *sql.DB, tokenHash string, expiresAt time.Time) error {
	_, err := db.Exec("UPDATE sessions SET expires_at = ? WHERE token_hash = ?", expiresAt.Unix(), tokenHash)
	return err
}

func deleteSession(db *sql.DB, tokenHash string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func deleteUserSessions(db *sql.DB, userID int64) error {
	_, err := db.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

func deleteExpiredSessions(db *sql.DB, now time.Time) (int64, error) {
	res, err := db.Exec("DELETE FROM sessions WHERE expires_at < ?", now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

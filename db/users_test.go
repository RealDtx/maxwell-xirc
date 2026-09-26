package db

import (
	"testing"
	"time"
)

func TestUsersAndSessions_SQLite(t *testing.T) {
	store, cleanup := newTestSQLiteStore(t)
	defer cleanup()
	testUsersAndSessions(t, store)
}

func TestUsersAndSessions_MySQL(t *testing.T) {
	store := newTestMySQLStore(t) // skips without XIRC_TEST_MYSQL_DSN
	store.db.Exec("DELETE FROM sessions")
	store.db.Exec("DELETE FROM users")
	testUsersAndSessions(t, store)
}

func testUsersAndSessions(t *testing.T, s Store) {
	t.Helper()
	if n, _ := s.CountUsers(); n != 0 {
		t.Fatalf("fresh db users = %d", n)
	}
	u := &User{Username: "alice", PasswordHash: "h1", Role: "admin", CreatedAt: time.Now()}
	if err := s.CreateUser(u); err != nil || u.ID == 0 {
		t.Fatalf("create: %v id=%d", err, u.ID)
	}
	if err := s.CreateUser(&User{Username: "alice", PasswordHash: "x", Role: "user", CreatedAt: time.Now()}); err == nil {
		t.Fatal("duplicate username accepted")
	}
	s.CreateUser(&User{Username: "bob", PasswordHash: "h2", Role: "user", CreatedAt: time.Now()})

	got, err := s.GetUserByName("alice")
	if err != nil || got == nil || got.PasswordHash != "h1" || got.Role != "admin" {
		t.Fatalf("get by name: %+v %v", got, err)
	}
	if missing, err := s.GetUserByName("nobody"); missing != nil || err != nil {
		t.Fatalf("missing user: %+v %v", missing, err)
	}
	if n, _ := s.CountAdmins(); n != 1 {
		t.Errorf("admins = %d", n)
	}
	list, _ := s.ListUsers()
	if len(list) != 2 || list[0].Username != "alice" {
		t.Errorf("list: %+v", list)
	}

	got.Role = "user"
	got.PasswordHash = "h3"
	if err := s.UpdateUser(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetUser(got.ID); again.Role != "user" || again.PasswordHash != "h3" {
		t.Errorf("update not persisted: %+v", again)
	}

	now := time.Now()
	s.CreateSession(&Session{TokenHash: "live", UserID: u.ID, ExpiresAt: now.Add(time.Hour)})
	s.CreateSession(&Session{TokenHash: "dead", UserID: u.ID, ExpiresAt: now.Add(-time.Hour)})
	if sess, _ := s.GetSession("live"); sess == nil || sess.UserID != u.ID {
		t.Fatalf("get session: %+v", sess)
	}
	if n, _ := s.DeleteExpiredSessions(now); n != 1 {
		t.Errorf("expired deleted = %d", n)
	}
	later := now.Add(48 * time.Hour).Truncate(time.Second)
	s.TouchSession("live", later)
	if sess, _ := s.GetSession("live"); !sess.ExpiresAt.Equal(later) {
		t.Errorf("touch: %v want %v", sess.ExpiresAt, later)
	}
	if err := s.DeleteUserSessions(u.ID); err != nil {
		t.Fatal(err)
	}
	if sess, _ := s.GetSession("live"); sess != nil {
		t.Error("user sessions not deleted")
	}

	s.CreateSession(&Session{TokenHash: "x", UserID: u.ID, ExpiresAt: now.Add(time.Hour)})
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if sess, _ := s.GetSession("x"); sess != nil {
		t.Error("DeleteUser left sessions behind")
	}
	if n, _ := s.CountUsers(); n != 1 {
		t.Errorf("users after delete = %d", n)
	}
}

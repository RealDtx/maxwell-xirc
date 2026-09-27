package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
	"golang.org/x/crypto/bcrypt"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9._-]{1,32}$`)

// dummyHash keeps unknown-user logins as slow as wrong-password ones.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("xirc-dummy-password"), bcrypt.DefaultCost)

func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !usernameRe.MatchString(s) {
		return "", errors.New("username must be 1-32 characters of a-z 0-9 . _ -")
	}
	return s, nil
}

func HashPassword(pw string) (string, error) {
	if len(pw) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CreateOrResetAdmin creates name as admin, or resets its password and
// promotes it. Existing sessions of that user are revoked.
func CreateOrResetAdmin(store db.Store, name, password string) error {
	name, err := NormalizeUsername(name)
	if err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	u, err := store.GetUserByName(name)
	if err != nil {
		return err
	}
	if u == nil {
		return store.CreateUser(&db.User{Username: name, PasswordHash: hash, Role: "admin", CreatedAt: time.Now()})
	}
	u.PasswordHash, u.Role = hash, "admin"
	if err := store.UpdateUser(u); err != nil {
		return err
	}
	return store.DeleteUserSessions(u.ID)
}

func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, u *db.User) error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	if err := a.store.CreateSession(&db.Session{TokenHash: hashToken(tok), UserID: u.ID, ExpiresAt: time.Now().Add(sessionTTL)}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: a.cookiePath,
		MaxAge: int(sessionTTL / time.Second), HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: a.isHTTPS(r),
	})
	return nil
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "auth disabled")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := s.auth.clientIP(r).String()
	if !s.auth.limiter.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many failed logins, wait a minute")
		return
	}
	var c credentials
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, _ := NormalizeUsername(c.Username)
	u, err := s.store.GetUserByName(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hash := dummyHash
	if u != nil {
		hash = []byte(u.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(c.Password)) != nil || u == nil {
		writeError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	s.auth.limiter.succeed(ip)
	if err := s.auth.startSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Principal{Username: u.Username, Role: u.Role, Via: "session"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "auth disabled")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.store.DeleteSession(hashToken(c.Value)); err != nil {
			log.Printf("logout: delete session failed: %v", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: s.auth.cookiePath, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if p := principalFrom(r.Context()); p != nil {
		// Version only for authenticated callers: don't advertise it to anonymous probes.
		writeJSON(w, http.StatusOK, struct {
			*Principal
			Version string `json:"version"`
		}{p, Version})
		return
	}
	if n, err := s.store.CountUsers(); err == nil && n == 0 {
		writeJSON(w, http.StatusOK, map[string]bool{"setup_required": true})
		return
	}
	writeError(w, http.StatusUnauthorized, "login required")
}

// handleAuthSetup creates the first admin. Open to anyone, but only while no
// user exists.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "auth disabled")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var c credentials
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if n, err := s.store.CountUsers(); err != nil || n > 0 {
		writeError(w, http.StatusConflict, "setup already done")
		return
	}
	name, err := NormalizeUsername(c.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := HashPassword(c.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := &db.User{Username: name, PasswordHash: hash, Role: "admin", CreatedAt: time.Now()}
	if err := s.store.CreateUser(u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.auth.startSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Principal{Username: u.Username, Role: u.Role, Via: "session"})
}

type userRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// /api/users (admin; enforced by middleware)
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "auth disabled")
		return
	}
	switch r.Method {
	case http.MethodGet:
		users, err := s.store.ListUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, users)
	case http.MethodPost:
		var req userRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		name, err := NormalizeUsername(req.Username)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Role != "admin" && req.Role != "user" {
			writeError(w, http.StatusBadRequest, "role must be admin or user")
			return
		}
		hash, err := HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if existing, _ := s.store.GetUserByName(name); existing != nil {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		u := &db.User{Username: name, PasswordHash: hash, Role: req.Role, CreatedAt: time.Now()}
		if err := s.store.CreateUser(u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, u)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /api/users/{id}: PUT {role?, password?}, DELETE
func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeError(w, http.StatusNotFound, "auth disabled")
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/users/"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	s.setupMu.Lock() // serialises last-admin checks
	defer s.setupMu.Unlock()
	u, err := s.store.GetUser(id)
	if err != nil || u == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	lastAdmin := func() bool {
		n, _ := s.store.CountAdmins()
		return u.Role == "admin" && n <= 1
	}
	switch r.Method {
	case http.MethodPut:
		var req userRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Role != "" {
			if req.Role != "admin" && req.Role != "user" {
				writeError(w, http.StatusBadRequest, "role must be admin or user")
				return
			}
			if req.Role == "user" && lastAdmin() {
				writeError(w, http.StatusConflict, "cannot demote the last admin")
				return
			}
			u.Role = req.Role
		}
		if req.Password != "" {
			if u.PasswordHash, err = HashPassword(req.Password); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if err := s.store.UpdateUser(u); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Role or password change: revoke sessions so it applies immediately.
		if err := s.store.DeleteUserSessions(u.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, u)
	case http.MethodDelete:
		if lastAdmin() {
			writeError(w, http.StatusConflict, "cannot delete the last admin")
			return
		}
		if err := s.store.DeleteUser(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
)

var jsonHdr = map[string]string{"Content-Type": "application/json"}

func cookieOf(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	return ""
}

func withCookie(tok string) map[string]string {
	return map[string]string{"Content-Type": "application/json", "Cookie": sessionCookie + "=" + tok}
}

func TestSetupThenLogin(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	const ip = "203.0.113.9:1"

	w := do(srv, "GET", "/api/auth/me", ip, nil, "")
	if !strings.Contains(w.Body.String(), `"setup_required":true`) {
		t.Fatalf("fresh me: %d %s", w.Code, w.Body)
	}
	if w := do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"Admin","password":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("short password: %d", w.Code)
	}
	w = do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"Admin","password":"correct horse"}`)
	if w.Code != http.StatusOK || cookieOf(w) == "" {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	if w := do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"x","password":"another pw"}`); w.Code != http.StatusConflict {
		t.Errorf("second setup: got %d want 409", w.Code)
	}

	if w := do(srv, "POST", "/api/auth/login", ip, jsonHdr, `{"username":"admin","password":"wrong pass"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("bad pw: %d", w.Code)
	}
	w = do(srv, "POST", "/api/auth/login", ip, jsonHdr, `{"username":" ADMIN ","password":"correct horse"}`)
	tok := cookieOf(w)
	if w.Code != http.StatusOK || tok == "" {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	var me Principal
	json.NewDecoder(do(srv, "GET", "/api/auth/me", ip, withCookie(tok), "").Body).Decode(&me)
	if me.Username != "admin" || me.Role != "admin" || me.Via != "session" {
		t.Errorf("me: %+v", me)
	}

	do(srv, "POST", "/api/auth/logout", ip, withCookie(tok), `{}`)
	if w := do(srv, "GET", "/api/downloads", ip, withCookie(tok), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("after logout: %d", w.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	do(srv, "POST", "/api/auth/setup", "203.0.113.1:1", jsonHdr, `{"username":"a","password":"password1"}`)
	for i := 0; i < 5; i++ {
		do(srv, "POST", "/api/auth/login", "203.0.113.9:1", jsonHdr, `{"username":"a","password":"nope-nope"}`)
	}
	w := do(srv, "POST", "/api/auth/login", "203.0.113.9:1", jsonHdr, `{"username":"a","password":"password1"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th attempt: got %d want 429", w.Code)
	}
	if w := do(srv, "POST", "/api/auth/login", "203.0.113.10:1", jsonHdr, `{"username":"a","password":"password1"}`); w.Code != http.StatusOK {
		t.Errorf("other IP must not be limited: %d", w.Code)
	}
}

func TestCookiePathUsesPrefix(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()
	a, _ := NewAuth(config.AuthConfig{TrustedRole: "admin"}, store, "/xirc")
	srv.SetAuth(a)
	w := do(srv, "POST", "/api/auth/setup", "203.0.113.1:1", jsonHdr, `{"username":"a","password":"password1"}`)
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			if c.Path != "/xirc" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie attrs: path=%q httponly=%v samesite=%v", c.Path, c.HttpOnly, c.SameSite)
			}
			if c.Secure {
				t.Error("Secure on plain http")
			}
			return
		}
	}
	t.Fatal("no cookie")
}

func TestSecureCookieBehindHTTPSProxy(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	hdr := map[string]string{"Content-Type": "application/json", "X-Forwarded-Proto": "https"}
	w := do(srv, "POST", "/api/auth/setup", "127.0.0.1:1", hdr, `{"username":"a","password":"password1"}`)
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && !c.Secure {
			t.Error("Secure missing behind https proxy")
		}
	}
}

func TestUsersCRUDAndLastAdminGuard(t *testing.T) {
	srv, store := newAuthTestServer(t, config.AuthConfig{})
	const ip = "203.0.113.9:1"
	admin := cookieOf(do(srv, "POST", "/api/auth/setup", ip, jsonHdr, `{"username":"root","password":"password1"}`))

	w := do(srv, "POST", "/api/users", ip, withCookie(admin), `{"username":"bob","password":"password2","role":"user"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create user: %d %s", w.Code, w.Body)
	}
	bob, _ := store.GetUserByName("bob")
	bobTok := cookieOf(do(srv, "POST", "/api/auth/login", ip, jsonHdr, `{"username":"bob","password":"password2"}`))

	if w := do(srv, "GET", "/api/users", ip, withCookie(bobTok), ""); w.Code != http.StatusForbidden {
		t.Errorf("user listing users: %d", w.Code)
	}
	if strings.Contains(do(srv, "GET", "/api/users", ip, withCookie(admin), "").Body.String(), "password") {
		t.Error("password hash leaked in list")
	}

	// Password reset logs bob out everywhere.
	path := "/api/users/" + itoa(bob.ID)
	if w := do(srv, "PUT", path, ip, withCookie(admin), `{"password":"newpassword"}`); w.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	if w := do(srv, "GET", "/api/downloads", ip, withCookie(bobTok), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("bob still logged in after reset: %d", w.Code)
	}

	root, _ := store.GetUserByName("root")
	rootPath := "/api/users/" + itoa(root.ID)
	if w := do(srv, "PUT", rootPath, ip, withCookie(admin), `{"role":"user"}`); w.Code != http.StatusConflict {
		t.Errorf("demote last admin: got %d want 409", w.Code)
	}
	if w := do(srv, "DELETE", rootPath, ip, withCookie(admin), ""); w.Code != http.StatusConflict {
		t.Errorf("delete last admin: got %d want 409", w.Code)
	}
	if w := do(srv, "DELETE", path, ip, withCookie(admin), ""); w.Code != http.StatusOK {
		t.Errorf("delete bob: %d", w.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

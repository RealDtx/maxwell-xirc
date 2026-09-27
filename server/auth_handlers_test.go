package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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
	var me struct {
		Principal
		Version string `json:"version"`
	}
	json.NewDecoder(do(srv, "GET", "/api/auth/me", ip, withCookie(tok), "").Body).Decode(&me)
	if me.Username != "admin" || me.Role != "admin" || me.Via != "session" || me.Version != Version {
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
		if c.Name == sessionCookie {
			if !c.Secure {
				t.Error("Secure missing behind https proxy")
			}
			return
		}
	}
	t.Fatal("no cookie")
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

// TestConcurrentLoginRateLimitIsAtomic fires many concurrent wrong-password
// logins from one IP and checks the limiter can't be raced: the check and
// the recording of an attempt must happen under a single lock, otherwise
// concurrent requests could all observe "not yet blocked" before any of
// them records a failure, letting more than loginMaxFails through.
func TestConcurrentLoginRateLimitIsAtomic(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	do(srv, "POST", "/api/auth/setup", "203.0.113.1:1", jsonHdr, `{"username":"a","password":"password1"}`)

	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := do(srv, "POST", "/api/auth/login", "203.0.113.20:1", jsonHdr, `{"username":"a","password":"wrong-pass"}`)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if codes[http.StatusUnauthorized] > loginMaxFails {
		t.Errorf("got %d 401s, want at most %d", codes[http.StatusUnauthorized], loginMaxFails)
	}
	if codes[http.StatusUnauthorized]+codes[http.StatusTooManyRequests] != n {
		t.Errorf("unexpected response codes: %+v", codes)
	}
}

// TestLogoutClearsAnonymousCookie: an expired/bogus session cookie shouldn't
// prevent a client from clearing it, even from an untrusted IP with no
// authenticated principal.
func TestLogoutClearsAnonymousCookie(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	w := do(srv, "POST", "/api/auth/logout", "203.0.113.9:1", withCookie("bogus-token"), `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("logout: got %d want 200", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			if c.MaxAge >= 0 {
				t.Errorf("expected MaxAge<0 to clear cookie, got %d", c.MaxAge)
			}
			return
		}
	}
	t.Fatal("no Set-Cookie in logout response")
}

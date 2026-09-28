package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

func newAuthTestServer(t *testing.T, cfg config.AuthConfig) (*Server, db.Store) {
	t.Helper()
	srv, store, cleanup := newTestServerWithStore(t)
	t.Cleanup(cleanup)
	if cfg.TrustedRole == "" {
		cfg.TrustedRole = "admin"
	}
	if cfg.TrustedProxies == nil {
		cfg.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	}
	a, err := NewAuth(cfg, store, "")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAuth(a)
	return srv, store
}

func do(srv *Server, method, path, remote string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestNewAuth_RejectsBadConfig(t *testing.T) {
	if _, err := NewAuth(config.AuthConfig{TrustedRole: "root"}, nil, ""); err == nil {
		t.Error("bad role accepted")
	}
	if _, err := NewAuth(config.AuthConfig{TrustedRole: "user", TrustedNetworks: []string{"10.0.0.0/33"}}, nil, ""); err == nil {
		t.Error("bad CIDR accepted")
	}
}

func TestClientIP(t *testing.T) {
	a, _ := NewAuth(config.AuthConfig{TrustedRole: "admin", TrustedProxies: []string{"127.0.0.1/32", "::1/128"}}, nil, "")
	cases := []struct {
		remote, xff, want string
	}{
		{"203.0.113.9:5000", "", "203.0.113.9"},
		{"203.0.113.9:5000", "192.168.1.5", "203.0.113.9"},          // untrusted peer: XFF ignored
		{"127.0.0.1:5000", "192.168.1.5", "192.168.1.5"},            // trusted proxy
		{"127.0.0.1:5000", "6.6.6.6, 192.168.1.5", "192.168.1.5"},   // right-most untrusted wins
		{"127.0.0.1:5000", "192.168.1.5, 127.0.0.1", "192.168.1.5"}, // skip trusted hops
		{"[::1]:5000", "2001:db8::7", "2001:db8::7"},
		{"127.0.0.1:5000", "garbage", "127.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := a.clientIP(r).String(); got != c.want {
			t.Errorf("remote=%s xff=%q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}

// TestClientIP_MultipleHeaderLines covers proxies (e.g. HAProxy) that append
// their own X-Forwarded-For as a second header line rather than extending
// the first. Header.Get only sees the first line, which is client-controlled
// — clientIP must consider every line via Header.Values.
func TestClientIP_MultipleHeaderLines(t *testing.T) {
	a, _ := NewAuth(config.AuthConfig{TrustedRole: "admin", TrustedProxies: []string{"127.0.0.1/32", "::1/128"}}, nil, "")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Add("X-Forwarded-For", "192.168.1.5") // client-forged
	r.Header.Add("X-Forwarded-For", "203.0.113.9") // appended by the trusted proxy
	if got := a.clientIP(r).String(); got != "203.0.113.9" {
		t.Errorf("multi-line XFF: got %s want 203.0.113.9", got)
	}
}

func TestAnonymousBlockedFromAPI(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{})
	for _, p := range []string{"/api/downloads", "/api/servers", "/ws"} {
		if w := do(srv, "GET", p, "203.0.113.9:1", nil, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: got %d want 401", p, w.Code)
		}
	}
	for _, p := range []string{"/api/health", "/api/auth/me"} {
		if w := do(srv, "GET", p, "203.0.113.9:1", nil, ""); w.Code == http.StatusUnauthorized {
			t.Errorf("%s must be reachable anonymously", p)
		}
	}
}

func TestSpoofedXFFDoesNotGrantTrustedNetwork(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}})
	w := do(srv, "GET", "/api/downloads", "203.0.113.9:1", map[string]string{"X-Forwarded-For": "192.168.1.5"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed XFF: got %d want 401", w.Code)
	}
	w = do(srv, "GET", "/api/downloads", "127.0.0.1:1", map[string]string{"X-Forwarded-For": "192.168.1.5"}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("real LAN client via proxy: got %d want 200", w.Code)
	}
}

func TestTrustedNetworkRole(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"})
	if w := do(srv, "GET", "/api/servers", "192.168.1.5:1", nil, ""); w.Code != http.StatusOK {
		t.Errorf("user GET servers: %d", w.Code)
	}
	w := do(srv, "POST", "/api/servers", "192.168.1.5:1", map[string]string{"Content-Type": "application/json"}, `{}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("user POST servers: got %d want 403", w.Code)
	}
}

func TestAdminOnlyTable(t *testing.T) {
	admin := []struct{ m, p string }{
		{"POST", "/api/irc/raw"}, {"POST", "/api/irc/connect"}, {"POST", "/api/irc/disconnect"}, {"POST", "/api/irc/join"},
		{"POST", "/api/servers"}, {"PUT", "/api/servers/3"}, {"DELETE", "/api/realms/2"}, {"PUT", "/api/library"},
		{"GET", "/api/search/patterns"}, {"GET", "/api/search/unmatched"},
		{"POST", "/api/search/patterns/learn"}, {"POST", "/api/index/clear"}, {"GET", "/api/errors"},
		{"GET", "/api/users"}, {"DELETE", "/api/users/4"}, {"GET", "/api/setup/status"}, {"POST", "/api/capabilities/recheck"},
		{"POST", "/api/files"}, {"GET", "/api/browse"}, {"POST", "/api/downloads/delete"}, {"POST", "/api/downloads/clear"},
		{"POST", "/api/downloads/set-target"}, {"POST", "/api/downloads/move"},
	}
	for _, c := range admin {
		if !adminOnly(c.m, c.p) {
			t.Errorf("%s %s should be admin-only", c.m, c.p)
		}
	}
	user := []struct{ m, p string }{
		{"GET", "/api/servers"}, {"GET", "/api/realms"}, {"GET", "/api/library"}, {"POST", "/api/search/start"},
		{"GET", "/api/index/search"}, {"POST", "/api/downloads/request"}, {"POST", "/api/downloads/cancel"},
		{"POST", "/api/downloads/retry"}, {"POST", "/api/downloads/set-auto-extract"}, {"GET", "/api/files"},
		{"GET", "/api/files/raw"},
		{"POST", "/api/irc/message"}, {"GET", "/api/irc/status"}, {"GET", "/api/stats/downloads"},
		{"GET", "/api/storage"}, {"GET", "/api/capabilities"}, {"POST", "/api/search/saved"}, {"GET", "/ws"},
		{"POST", "/api/library/preview"}, // read-only: downloads table "Auto → …" label
	}
	for _, c := range user {
		if adminOnly(c.m, c.p) {
			t.Errorf("%s %s should be allowed for users", c.m, c.p)
		}
	}
}

// TestUserRoleForbiddenOnAdminRules drives every adminRules entry through the
// real middleware as a user-role principal.
func TestUserRoleForbiddenOnAdminRules(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"})
	json := map[string]string{"Content-Type": "application/json"}
	for _, r := range adminRules {
		for _, p := range []string{r.prefix, r.prefix + "/x"} {
			if w := do(srv, "POST", p, "192.168.1.5:1", json, `{}`); w.Code != http.StatusForbidden {
				t.Errorf("user POST %s: got %d want 403", p, w.Code)
			}
			if !r.nonGetOnly {
				if w := do(srv, "GET", p, "192.168.1.5:1", nil, ""); w.Code != http.StatusForbidden {
					t.Errorf("user GET %s: got %d want 403", p, w.Code)
				}
			}
		}
	}
}

func TestCrossSiteRequestsRejected(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}})
	// A form/fetch "simple request" from a foreign page on a LAN browser.
	w := do(srv, "POST", "/api/downloads/clear", "192.168.1.5:1", map[string]string{"Content-Type": "text/plain"}, `{"status":"completed"}`)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST: got %d want 415", w.Code)
	}
}

func TestAuthUpdateChangesTrustedNetworks(t *testing.T) {
	a, err := NewAuth(config.AuthConfig{TrustedRole: "admin"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/downloads", nil)
	req.RemoteAddr = "192.168.1.5:1234"
	if p := a.networkPrincipal(req); p != nil {
		t.Fatalf("before update: %+v", p)
	}
	if err := a.Update(config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"}); err != nil {
		t.Fatal(err)
	}
	if p := a.networkPrincipal(req); p == nil || p.Role != "user" {
		t.Fatalf("after update: %+v", p)
	}
	if err := a.Update(config.AuthConfig{TrustedNetworks: []string{"nope"}, TrustedRole: "user"}); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
	if p := a.networkPrincipal(req); p == nil || p.Role != "user" {
		t.Fatalf("failed update changed policy: %+v", p)
	}
}

func TestParseAuthPolicy(t *testing.T) {
	if err := ParseAuthPolicy(config.AuthConfig{TrustedRole: "admin"}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	if err := ParseAuthPolicy(config.AuthConfig{TrustedRole: "root"}); err == nil {
		t.Error("bad role accepted")
	}
	if err := ParseAuthPolicy(config.AuthConfig{TrustedRole: "user", TrustedNetworks: []string{"nope"}}); err == nil {
		t.Error("bad CIDR accepted")
	}
}

// TestAuthUpdateRace exercises Update concurrently with networkPrincipal
// resolution under -race, to catch any read of the trusted-network policy
// that isn't a single atomic snapshot.
func TestAuthUpdateRace(t *testing.T) {
	a, err := NewAuth(config.AuthConfig{TrustedRole: "admin"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/downloads", nil)
	req.RemoteAddr = "192.168.1.5:1234"
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			a.Update(config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"})
		}
	}()
	for i := 0; i < 200; i++ {
		a.networkPrincipal(req)
	}
	<-done
}

func TestSessionCookieAuthenticates(t *testing.T) {
	srv, store := newAuthTestServer(t, config.AuthConfig{})
	u := &db.User{Username: "bob", PasswordHash: "x", Role: "user", CreatedAt: time.Now()}
	store.CreateUser(u)
	store.CreateSession(&db.Session{TokenHash: hashToken("tok"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)})
	hdr := map[string]string{"Cookie": sessionCookie + "=tok"}
	if w := do(srv, "GET", "/api/downloads", "203.0.113.9:1", hdr, ""); w.Code != http.StatusOK {
		t.Errorf("valid session: %d", w.Code)
	}
	store.CreateSession(&db.Session{TokenHash: hashToken("old"), UserID: u.ID, ExpiresAt: time.Now().Add(-time.Hour)})
	hdr = map[string]string{"Cookie": sessionCookie + "=old"}
	if w := do(srv, "GET", "/api/downloads", "203.0.113.9:1", hdr, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("expired session: got %d want 401", w.Code)
	}
}

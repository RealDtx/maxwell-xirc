package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

const (
	sessionCookie = "xirc_session"
	sessionTTL    = 30 * 24 * time.Hour
	touchEvery    = time.Hour
)

type Principal struct {
	UserID   int64  `json:"-"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Via      string `json:"via"` // "session" | "network"
}

func (p *Principal) IsAdmin() bool { return p != nil && p.Role == "admin" }

type ctxKey struct{}

func principalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

type Auth struct {
	store       db.Store
	trustedNets []*net.IPNet
	proxies     []*net.IPNet
	trustedRole string
	cookiePath  string
	limiter     *loginLimiter // Task 6
}

func parseCIDRs(list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range list {
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
				s += "/32"
			} else {
				s += "/128"
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func NewAuth(cfg config.AuthConfig, store db.Store, prefix string) (*Auth, error) {
	if cfg.TrustedRole != "admin" && cfg.TrustedRole != "user" {
		return nil, fmt.Errorf("auth.trusted_role must be admin or user, got %q", cfg.TrustedRole)
	}
	nets, err := parseCIDRs(cfg.TrustedNetworks)
	if err != nil {
		return nil, fmt.Errorf("auth.trusted_networks: %w", err)
	}
	proxies, err := parseCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("auth.trusted_proxies: %w", err)
	}
	path := strings.TrimRight(prefix, "/")
	if path == "" {
		path = "/"
	}
	return &Auth{store: store, trustedNets: nets, proxies: proxies, trustedRole: cfg.TrustedRole,
		cookiePath: path, limiter: newLoginLimiter()}, nil
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// clientIP believes X-Forwarded-For only from trusted proxies, and then takes
// the right-most hop that is not itself a trusted proxy (left entries are
// client-controlled).
func (a *Auth) clientIP(r *http.Request) net.IP {
	ip := remoteIP(r)
	if ip == nil || !inNets(ip, a.proxies) {
		return ip
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := net.ParseIP(strings.TrimSpace(hops[i]))
		if h == nil {
			break
		}
		if !inNets(h, a.proxies) {
			return h
		}
	}
	return ip
}

func (a *Auth) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	ip := remoteIP(r)
	if ip == nil || !inNets(ip, a.proxies) {
		return false
	}
	proto := r.Header.Values("X-Forwarded-Proto")
	if len(proto) == 0 {
		return false
	}
	// A client-sent first line can't set this: only the proxy-appended last
	// value (the hop closest to us) is believed.
	return strings.EqualFold(proto[len(proto)-1], "https")
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// sessionPrincipal resolves the cookie to a user, sliding the expiry.
func (a *Auth) sessionPrincipal(r *http.Request) *Principal {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	h := hashToken(c.Value)
	sess, err := a.store.GetSession(h)
	if err != nil || sess == nil {
		return nil
	}
	now := time.Now()
	if now.After(sess.ExpiresAt) {
		a.store.DeleteSession(h)
		return nil
	}
	u, err := a.store.GetUser(sess.UserID)
	if err != nil || u == nil {
		return nil
	}
	if sess.ExpiresAt.Sub(now) < sessionTTL-touchEvery {
		a.store.TouchSession(h, now.Add(sessionTTL))
	}
	return &Principal{UserID: u.ID, Username: u.Username, Role: u.Role, Via: "session"}
}

func (a *Auth) principal(r *http.Request) *Principal {
	if p := a.sessionPrincipal(r); p != nil {
		return p
	}
	if ip := a.clientIP(r); ip != nil && inNets(ip, a.trustedNets) {
		return &Principal{Username: "lan", Role: a.trustedRole, Via: "network"}
	}
	return nil
}

// anonymousOK lists what a not-logged-in client may reach. Static assets are
// everything outside /api/ and /ws (the SPA draws the login screen itself).
func anonymousOK(path string) bool {
	switch path {
	case "/api/health", "/api/auth/login", "/api/auth/me", "/api/auth/setup":
		return true
	}
	return !strings.HasPrefix(path, "/api/") && path != "/ws"
}

type rule struct {
	prefix     string
	nonGetOnly bool
}

// adminRules mirrors the permission table in the spec. Prefix match on
// path segments: "/api/servers" covers "/api/servers" and "/api/servers/…".
var adminRules = []rule{
	{"/api/irc/raw", false}, {"/api/irc/connect", false}, {"/api/irc/disconnect", false}, {"/api/irc/join", false},
	{"/api/servers", true}, {"/api/realms", true}, {"/api/library", true},
	{"/api/search/patterns", false}, {"/api/search/unmatched", false},
	{"/api/index/clear", false}, {"/api/errors", false}, {"/api/users", false}, {"/api/setup", false},
	{"/api/capabilities/recheck", false}, {"/api/browse", false},
	{"/api/files", true},
	{"/api/downloads/delete", false}, {"/api/downloads/clear", false},
	{"/api/downloads/set-target", false}, {"/api/downloads/move", false},
}

func adminOnly(method, path string) bool {
	for _, r := range adminRules {
		if path != r.prefix && !strings.HasPrefix(path, r.prefix+"/") {
			continue
		}
		if r.nonGetOnly && (method == http.MethodGet || method == http.MethodHead) {
			return false
		}
		return true
	}
	return false
}

func (a *Auth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CSRF: browsers can only send JSON cross-origin after a CORS
		// preflight, which we never answer — so requiring JSON on writes
		// blocks form/text-plain attacks, including against LAN principals
		// that have no cookie to protect them.
		if strings.HasPrefix(r.URL.Path, "/api/") && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
				return
			}
		}
		p := a.principal(r)
		if p == nil && !anonymousOK(r.URL.Path) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		if adminOnly(r.Method, r.URL.Path) && !p.IsAdmin() {
			if p == nil {
				writeError(w, http.StatusUnauthorized, "login required")
			} else {
				writeError(w, http.StatusForbidden, "admin only")
			}
			return
		}
		if p != nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, p))
		}
		next.ServeHTTP(w, r)
	})
}

func logAuthConfig(a *Auth) {
	if len(a.trustedNets) == 0 {
		log.Printf("auth: login required for all clients")
	} else {
		log.Printf("auth: %d trusted network(s) skip login as role %q", len(a.trustedNets), a.trustedRole)
	}
}

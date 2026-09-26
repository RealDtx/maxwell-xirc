package server

import (
	"sync"
	"time"
)

const (
	loginMaxFails = 5
	loginWindow   = time.Minute
)

// loginLimiter counts failed logins per client IP in a fixed window.
// ponytail: in-memory, per process; fine for a single instance.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{fails: map[string][]time.Time{}} }

func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if now.Sub(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.fails, ip)
		return nil
	}
	l.fails[ip] = kept
	return kept
}

// allow reports whether ip may attempt a login right now. If so, it records
// this attempt immediately (before the caller checks the password) so
// concurrent requests can't all observe "not yet blocked" and slip through
// together; call succeed on a successful login to undo that recording.
func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.recent(ip, now)) >= loginMaxFails {
		return false
	}
	l.fails[ip] = append(l.fails[ip], now)
	return true
}

// succeed removes one recorded attempt for ip (the one allow just added for
// this request) so a correct password doesn't count against the limit.
func (l *loginLimiter) succeed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fails := l.fails[ip]
	if len(fails) == 0 {
		return
	}
	if len(fails) == 1 {
		delete(l.fails, ip)
		return
	}
	l.fails[ip] = fails[:len(fails)-1]
}

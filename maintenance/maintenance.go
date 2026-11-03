// Package maintenance runs a periodic background job that keeps the
// database bounded: it prunes stale search_results rows (a time-based
// safety net — results are normally cleared per-channel on the next search,
// but channels that stop being searched would otherwise accumulate rows
// forever), removes index entries a realm's bots haven't re-advertised
// within the realm's index_ttl_days, and caps the self-collected file index
// at a configured size, evicting the least-recently-seen entries first.
package maintenance

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

// Maintenance runs the periodic prune/eviction job.
type Maintenance struct {
	store   db.Store
	mu      sync.Mutex
	cfg     config.MaintenanceConfig
	unit    time.Duration // one IntervalHours step; tests shorten it
	onRun   func()        // test hook, called after each pass
	now     func() time.Time
	resetCh chan struct{}
	dailyCh chan struct{} // IndexCleanupTime changed
	once    sync.Once
	stopCh  chan struct{}
}

func New(store db.Store, cfg config.MaintenanceConfig) *Maintenance {
	return &Maintenance{store: store, cfg: cfg, unit: time.Hour, now: time.Now,
		resetCh: make(chan struct{}, 1), dailyCh: make(chan struct{}, 1), stopCh: make(chan struct{})}
}

func (m *Maintenance) config() config.MaintenanceConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SetConfig swaps the settings. If IntervalHours changes, the periodic
// timer restarts on the new interval (<= 0 stops periodic runs); if it's
// unchanged, the running ticker is left alone so a settings save unrelated
// to the schedule can't keep postponing the next run. No immediate pass.
func (m *Maintenance) SetConfig(cfg config.MaintenanceConfig) {
	m.mu.Lock()
	intervalChanged := cfg.IntervalHours != m.cfg.IntervalHours
	dailyChanged := cfg.IndexCleanupTime != m.cfg.IndexCleanupTime
	m.cfg = cfg
	m.mu.Unlock()
	if dailyChanged {
		select {
		case m.dailyCh <- struct{}{}:
		default:
		}
	}
	if !intervalChanged {
		return
	}
	select {
	case m.resetCh <- struct{}{}:
	default:
	}
}

// Start runs an immediate pass, then repeats at the configured interval.
func (m *Maintenance) Start() {
	m.runOnce()
	go m.loop()
	go m.dailyLoop()
}

// dailyLoop removes index entries past their realm's TTL once a day at
// IndexCleanupTime (local time). The TTL is in days, so daily is enough.
func (m *Maintenance) dailyLoop() {
	for {
		var fire <-chan time.Time
		var t *time.Timer
		if h, min, err := ParseClock(m.config().IndexCleanupTime); err == nil && h >= 0 {
			t = time.NewTimer(time.Until(nextClock(m.now(), h, min)))
			fire = t.C
		}
		select {
		case <-fire:
			m.pruneIndexByRealm()
		case <-m.dailyCh:
		case <-m.stopCh:
			if t != nil {
				t.Stop()
			}
			return
		}
		if t != nil {
			t.Stop()
		}
	}
}

// ParseClock parses "HH:MM" (24 h). Empty returns -1, -1: the job is off.
func ParseClock(s string) (hour, minute int, err error) {
	if s == "" {
		return -1, -1, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("expected a time like 04:00, or empty for never")
	}
	return t.Hour(), t.Minute(), nil
}

// nextClock is the next local hour:minute strictly after now.
func nextClock(now time.Time, hour, minute int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (m *Maintenance) loop() {
	for {
		var tick <-chan time.Time
		var t *time.Ticker
		if iv := time.Duration(m.config().IntervalHours) * m.unit; iv > 0 {
			t = time.NewTicker(iv)
			tick = t.C
		}
		select {
		case <-tick:
			m.runOnce()
		case <-m.resetCh:
		case <-m.stopCh:
			if t != nil {
				t.Stop()
			}
			return
		}
		if t != nil {
			t.Stop()
		}
	}
}

func (m *Maintenance) Stop() {
	m.once.Do(func() { close(m.stopCh) })
}

func (m *Maintenance) runOnce() {
	cfg := m.config()
	if cfg.SearchResultRetentionDays > 0 {
		cutoff := time.Now().Add(-time.Duration(cfg.SearchResultRetentionDays) * 24 * time.Hour)
		deleted, err := m.store.PruneSearchResults(cutoff)
		if err != nil {
			log.Printf("maintenance: prune search_results failed: %v", err)
		} else if deleted > 0 {
			log.Printf("maintenance: pruned %d search_results row(s) older than %d day(s)", deleted, cfg.SearchResultRetentionDays)
		}
	}

	if cfg.IndexMaxFiles > 0 {
		evicted, err := m.store.EnforceIndexCap(int64(cfg.IndexMaxFiles))
		if err != nil {
			log.Printf("maintenance: enforce index cap failed: %v", err)
		} else if evicted > 0 {
			log.Printf("maintenance: evicted %d indexed_files row(s) to stay within cap of %d", evicted, cfg.IndexMaxFiles)
		}
	}

	if n, err := m.store.DeleteExpiredSessions(time.Now()); err != nil {
		log.Printf("maintenance: prune sessions failed: %v", err)
	} else if n > 0 {
		log.Printf("maintenance: pruned %d expired session(s)", n)
	}

	if m.onRun != nil {
		m.onRun()
	}
}

// pruneIndexByRealm removes each realm's index entries not re-advertised
// within its IndexTTLDays (0 = keep). Channels outside any realm are only
// bounded by the index cap.
func (m *Maintenance) pruneIndexByRealm() {
	servers, err := m.store.GetServers()
	if err != nil {
		log.Printf("maintenance: listing servers failed: %v", err)
		return
	}
	for _, srv := range servers {
		realms, err := m.store.GetRealms(srv.ID)
		if err != nil {
			log.Printf("maintenance: listing realms of server %d failed: %v", srv.ID, err)
			continue
		}
		for _, r := range realms {
			if r.IndexTTLDays <= 0 {
				continue
			}
			cutoff := m.now().Add(-time.Duration(r.IndexTTLDays) * 24 * time.Hour)
			n, err := m.store.PruneIndexedFiles(srv.ID, []string{r.Name, r.DownloadChannel}, cutoff)
			if err != nil {
				log.Printf("maintenance: pruning index of realm %s failed: %v", r.Name, err)
			} else if n > 0 {
				log.Printf("maintenance: removed %d index entr(ies) of realm %s not seen for %d day(s)", n, r.Name, r.IndexTTLDays)
			}
		}
	}
}

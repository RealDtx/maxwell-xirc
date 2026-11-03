// Package maintenance runs two scheduled background jobs that keep the
// database bounded:
//
//   - cleanup: prunes stale search_results rows (a time-based safety net —
//     results are normally cleared per-channel on the next search), caps the
//     self-collected file index at a configured size (least-recently-seen
//     first) and drops expired sessions;
//   - index expiry: removes index entries a realm's bots haven't
//     re-advertised within the realm's index_expiry_days.
//
// Both use the same schedule: a start time (local "HH:MM", or empty =
// counted from server start) and "every N hours" (0 = no repeats).
package maintenance

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
)

// Maintenance runs the scheduled cleanup and index-expiry jobs.
type Maintenance struct {
	store   db.Store
	mu      sync.Mutex
	cfg     config.MaintenanceConfig
	unit    time.Duration // one "hours" step; tests shorten it
	onRun   func()        // test hook, called after each cleanup pass
	now     func() time.Time
	resetCh chan struct{} // cleanup schedule changed
	indexCh chan struct{} // index-expiry schedule changed
	once    sync.Once
	stopCh  chan struct{}
}

func New(store db.Store, cfg config.MaintenanceConfig) *Maintenance {
	return &Maintenance{store: store, cfg: cfg, unit: time.Hour, now: time.Now,
		resetCh: make(chan struct{}, 1), indexCh: make(chan struct{}, 1), stopCh: make(chan struct{})}
}

func (m *Maintenance) config() config.MaintenanceConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// SetConfig swaps the settings. A job whose schedule (start time or
// interval) changed restarts on the new one; an unchanged schedule is left
// alone, so saving unrelated settings can't keep postponing the next run.
// No immediate pass.
func (m *Maintenance) SetConfig(cfg config.MaintenanceConfig) {
	m.mu.Lock()
	cleanupChanged := cfg.IntervalHours != m.cfg.IntervalHours || cfg.StartTime != m.cfg.StartTime
	indexChanged := cfg.IndexExpiryIntervalHours != m.cfg.IndexExpiryIntervalHours || cfg.IndexExpiryStartTime != m.cfg.IndexExpiryStartTime
	m.cfg = cfg
	m.mu.Unlock()
	for ch, changed := range map[chan struct{}]bool{m.resetCh: cleanupChanged, m.indexCh: indexChanged} {
		if changed {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// Start runs an immediate cleanup pass, then both jobs on their schedules.
func (m *Maintenance) Start() {
	m.runOnce()
	go m.scheduleLoop(func(c config.MaintenanceConfig) (string, int) { return c.StartTime, c.IntervalHours }, m.resetCh, m.runOnce)
	go m.scheduleLoop(func(c config.MaintenanceConfig) (string, int) {
		return c.IndexExpiryStartTime, c.IndexExpiryIntervalHours
	}, m.indexCh, m.pruneIndexByRealm)
}

// scheduleLoop runs job on the schedule sched returns; a signal on reset
// re-reads it (and re-anchors "from server start" schedules to now).
func (m *Maintenance) scheduleLoop(sched func(config.MaintenanceConfig) (string, int), reset <-chan struct{}, job func()) {
	anchor := m.now()
	for {
		start, hours := sched(m.config())
		var fire <-chan time.Time
		var t *time.Timer
		if next, ok := nextRun(m.now(), anchor, start, time.Duration(hours)*m.unit); ok {
			t = time.NewTimer(next.Sub(m.now()))
			fire = t.C
		}
		select {
		case <-fire:
			job()
		case <-reset:
			anchor = m.now()
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

// ParseClock parses "HH:MM" (24 h). Empty returns -1, -1: no start time.
func ParseClock(s string) (hour, minute int, err error) {
	if s == "" {
		return -1, -1, nil
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("expected a time like 04:00, or empty")
	}
	return t.Hour(), t.Minute(), nil
}

// nextRun is the next run strictly after now. Without a start time the
// runs are anchor + k·every. With one, the job runs daily at the start
// time and then every `every` until the next day's start time (04:00 every
// 6 h → 04, 10, 16, 22; anything ≥ 24 h means once a day). every <= 0:
// no scheduled runs (ok=false).
// ponytail: wall-clock arithmetic; a DST switch can shift one run by an hour.
func nextRun(now, anchor time.Time, start string, every time.Duration) (time.Time, bool) {
	if every <= 0 {
		return time.Time{}, false
	}
	h, min, err := ParseClock(start)
	if err != nil || h < 0 {
		k := now.Sub(anchor)/every + 1
		return anchor.Add(k * every), true
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, now.Location())
	if day.After(now) {
		day = day.AddDate(0, 0, -1) // the most recent start time
	}
	next := day.Add((now.Sub(day)/every + 1) * every)
	if tomorrow := day.AddDate(0, 0, 1); !next.Before(tomorrow) {
		next = tomorrow
	}
	return next, true
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
// within its IndexExpiryDays (0 = keep). Channels outside any realm are only
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
			if r.IndexExpiryDays <= 0 {
				continue
			}
			cutoff := m.now().Add(-time.Duration(r.IndexExpiryDays) * 24 * time.Hour)
			n, err := m.store.PruneIndexedFiles(srv.ID, []string{r.Name, r.DownloadChannel}, cutoff)
			if err != nil {
				log.Printf("maintenance: pruning index of realm %s failed: %v", r.Name, err)
			} else if n > 0 {
				log.Printf("maintenance: removed %d index entr(ies) of realm %s not seen for %d day(s)", n, r.Name, r.IndexExpiryDays)
			}
		}
	}
}

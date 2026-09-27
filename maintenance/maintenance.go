// Package maintenance runs a periodic background job that keeps the
// database bounded: it prunes stale search_results rows (a time-based
// safety net — results are normally cleared per-channel on the next search,
// but channels that stop being searched would otherwise accumulate rows
// forever) and caps the self-collected file index at a configured size,
// evicting the least-recently-seen entries first.
package maintenance

import (
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
	resetCh chan struct{}
	once    sync.Once
	stopCh  chan struct{}
}

func New(store db.Store, cfg config.MaintenanceConfig) *Maintenance {
	return &Maintenance{store: store, cfg: cfg, unit: time.Hour,
		resetCh: make(chan struct{}, 1), stopCh: make(chan struct{})}
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
	m.cfg = cfg
	m.mu.Unlock()
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

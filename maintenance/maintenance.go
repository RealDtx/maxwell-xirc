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
	store  db.Store
	cfg    config.MaintenanceConfig
	once   sync.Once
	stopCh chan struct{}
}

func New(store db.Store, cfg config.MaintenanceConfig) *Maintenance {
	return &Maintenance{
		store:  store,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Start runs an immediate pass, then repeats at the configured interval in
// a background goroutine. An interval <= 0 disables the periodic job (an
// immediate pass still runs once).
func (m *Maintenance) Start() {
	m.runOnce()

	interval := time.Duration(m.cfg.IntervalHours) * time.Hour
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.runOnce()
			case <-m.stopCh:
				return
			}
		}
	}()
}

func (m *Maintenance) Stop() {
	m.once.Do(func() { close(m.stopCh) })
}

func (m *Maintenance) runOnce() {
	if m.cfg.SearchResultRetentionDays > 0 {
		cutoff := time.Now().Add(-time.Duration(m.cfg.SearchResultRetentionDays) * 24 * time.Hour)
		deleted, err := m.store.PruneSearchResults(cutoff)
		if err != nil {
			log.Printf("maintenance: prune search_results failed: %v", err)
		} else if deleted > 0 {
			log.Printf("maintenance: pruned %d search_results row(s) older than %d day(s)", deleted, m.cfg.SearchResultRetentionDays)
		}
	}

	if m.cfg.IndexMaxFiles > 0 {
		evicted, err := m.store.EnforceIndexCap(int64(m.cfg.IndexMaxFiles))
		if err != nil {
			log.Printf("maintenance: enforce index cap failed: %v", err)
		} else if evicted > 0 {
			log.Printf("maintenance: evicted %d indexed_files row(s) to stay within cap of %d", evicted, m.cfg.IndexMaxFiles)
		}
	}
}

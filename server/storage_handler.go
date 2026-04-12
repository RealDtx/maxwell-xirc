package server

import (
	"net/http"
	"syscall"
)

// GET /api/storage — returns disk usage stats for each unique destination directory
// in the active routing rules.
func (s *Server) handleStorageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rules, _ := s.store.GetAllFileRoutingRules()

	type DirStats struct {
		Path    string  `json:"path"`
		TotalGB float64 `json:"total_gb"`
		FreeGB  float64 `json:"free_gb"`
		UsedPct float64 `json:"used_pct"`
	}

	seen := map[string]bool{}
	var stats []DirStats
	for _, rule := range rules {
		if !rule.Enabled || rule.DestinationDir == "" || seen[rule.DestinationDir] {
			continue
		}
		seen[rule.DestinationDir] = true

		var fs syscall.Statfs_t
		if err := syscall.Statfs(rule.DestinationDir, &fs); err != nil {
			continue
		}

		total := float64(fs.Blocks) * float64(fs.Bsize)
		free := float64(fs.Bavail) * float64(fs.Bsize)
		usedPct := 0.0
		if total > 0 {
			usedPct = (total - free) / total * 100
		}
		stats = append(stats, DirStats{
			Path:    rule.DestinationDir,
			TotalGB: total / 1e9,
			FreeGB:  free / 1e9,
			UsedPct: usedPct,
		})
	}

	if stats == nil {
		stats = []DirStats{}
	}
	writeJSON(w, http.StatusOK, stats)
}

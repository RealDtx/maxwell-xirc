package server

import (
	"net/http"
	"syscall"
)

// GET /api/storage — returns disk usage stats for each configured root
// (library media root, enabled category dirs, downloads dir).
func (s *Server) handleStorageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type DirStats struct {
		Path    string  `json:"path"`
		TotalGB float64 `json:"total_gb"`
		FreeGB  float64 `json:"free_gb"`
		UsedPct float64 `json:"used_pct"`
	}

	var stats []DirStats
	for _, dir := range s.configuredRoots() {
		var fs syscall.Statfs_t
		if err := syscall.Statfs(dir, &fs); err != nil {
			continue
		}

		total := float64(fs.Blocks) * float64(fs.Bsize)
		free := float64(fs.Bavail) * float64(fs.Bsize)
		usedPct := 0.0
		if total > 0 {
			usedPct = (total - free) / total * 100
		}
		stats = append(stats, DirStats{
			Path:    dir,
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

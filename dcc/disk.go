package dcc

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// GetAvailableSpace returns the available bytes on the filesystem containing dir.
func GetAvailableSpace(dir string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// ParseSize converts human-readable size strings (e.g., "1GB", "500MB") to bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	s = strings.ToUpper(s)

	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"TB", 1 << 40},
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSuffix(s, m.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size: %s", s)
			}
			return int64(num * float64(m.mult)), nil
		}
	}

	// Plain number = bytes
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", s)
	}
	return n, nil
}

// CheckDiskSpace verifies that dir has enough space for filesize bytes
// plus the minFreeSpace safety margin.
func CheckDiskSpace(dir string, filesize int64, minFreeSpace string) error {
	avail, err := GetAvailableSpace(dir)
	if err != nil {
		return err
	}

	var margin int64
	if minFreeSpace != "" && minFreeSpace != "0" {
		margin, err = ParseSize(minFreeSpace)
		if err != nil {
			return fmt.Errorf("invalid min_free_space: %w", err)
		}
	}

	needed := filesize + margin
	if avail < needed {
		return fmt.Errorf("insufficient disk space: need %d bytes (file %d + margin %d), have %d",
			needed, filesize, margin, avail)
	}

	return nil
}

package parser

import (
	"encoding/json"

	"github.com/maxwell-xirc/xirc/db"
)

// ConfigPattern is a plain-Go representation of a pattern defined in config.
// It mirrors config.PatternConfig but lives in the parser package to avoid an
// import cycle (config → parser would be circular if parser → config).
type ConfigPattern struct {
	Name         string
	Regex        string
	FieldMapping map[string]int
	Priority     int
	Tags         []string
}

// SyncPatterns inserts patterns from the supplied slice that do not already
// exist in the DB (matched by name). It returns the number of patterns
// inserted. Existing names are silently skipped so that user edits are
// preserved across restarts.
func SyncPatterns(store db.Store, patterns []ConfigPattern) (int, error) {
	if len(patterns) == 0 {
		return 0, nil
	}

	existing, err := store.GetAllParsePatterns()
	if err != nil {
		return 0, err
	}
	existingNames := make(map[string]bool, len(existing))
	for _, p := range existing {
		existingNames[p.Name] = true
	}

	inserted := 0
	for _, cp := range patterns {
		if existingNames[cp.Name] {
			continue
		}

		fmBytes, err := json.Marshal(cp.FieldMapping)
		if err != nil {
			return inserted, err
		}

		var tagsBytes []byte
		if len(cp.Tags) > 0 {
			tagsBytes, err = json.Marshal(cp.Tags)
			if err != nil {
				return inserted, err
			}
		} else {
			tagsBytes = []byte("[]")
		}

		p := db.ParsePattern{
			Name:         cp.Name,
			Regex:        cp.Regex,
			FieldMapping: string(fmBytes),
			Priority:     cp.Priority,
			Builtin:      false,
			Enabled:      true,
			Tags:         string(tagsBytes),
		}
		if err := store.CreateParsePattern(&p); err != nil {
			return inserted, err
		}
		inserted++
	}

	return inserted, nil
}

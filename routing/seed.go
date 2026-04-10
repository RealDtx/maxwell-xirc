package routing

import "github.com/maxwell-xirc/xirc/db"

// SeedRoutingRules inserts default file routing rules into the store if they don't already exist.
// Rules are keyed by pattern — existing patterns are never updated, only absent ones are inserted.
// Call this on startup after migration to ensure baseline routing rules are present.
func SeedRoutingRules(store db.Store, mediaDir, downloadsDir string) error {
	existing, err := store.GetFileRoutingRules()
	if err != nil {
		return err
	}

	existingPatterns := make(map[string]bool)
	for _, r := range existing {
		existingPatterns[r.Pattern] = true
	}

	defaults := []db.FileRoutingRule{
		{Pattern: "*.mkv", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.avi", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.mp4", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.mov", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.wmv", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.flv", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.webm", DestinationDir: mediaDir, Priority: 100, Builtin: true, Enabled: true},
		{Pattern: "*.mp3", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.flac", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.ogg", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.wav", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.aac", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.m4a", DestinationDir: mediaDir, Priority: 90, Builtin: true, Enabled: true},
		{Pattern: "*.srt", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*.sub", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*.ass", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*.ssa", DestinationDir: mediaDir, Priority: 80, Builtin: true, Enabled: true},
		{Pattern: "*", DestinationDir: downloadsDir, Priority: 0, Builtin: true, Enabled: true},
	}

	for _, rule := range defaults {
		if existingPatterns[rule.Pattern] {
			continue
		}
		if err := store.CreateFileRoutingRule(&rule); err != nil {
			return err
		}
	}

	return nil
}

package parser

import (
	"log"

	"github.com/RealDtx/maxwell-irc/db"
)

type BuiltinPattern struct {
	Name         string
	Regex        string
	FieldMapping string
	Priority     int
}

func BuiltinPatterns() []BuiltinPattern {
	return []BuiltinPattern{
		{
			Name:         "hash-x-bracket",
			Regex:        `#(\d+)\s+(\d+)x\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     100,
		},
		{
			Name:         "bracket-hash-dash",
			Regex:        `\[#0*(\d+)\]\s*-\s*(\d+)x\s*-\s*\[([^\]]+)\]\s*-\s*(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     95,
		},
		{
			Name:         "pipe-separated",
			Regex:        `Pack\s+#(\d+)\s*\|\s*(\d+)\s+downloads?\s*\|\s*([^\|]+?)\s*\|\s*(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     90,
		},
		{
			Name:         "hash-file-bracket-trailing",
			Regex:        `#(\d+)\s+(.+?)\s+\[([^\]]+)\]\s+\((\d+)\s+downloads?\)`,
			FieldMapping: `{"pack_number":1,"filename":2,"filesize":3,"downloads_count":4}`,
			Priority:     85,
		},
		{
			Name:         "triple-colon",
			Regex:        `#(\d+)\s+:::\s+(\d+)x\s+:::\s+\[([^\]]+)\]\s+:::\s+(.+)`,
			FieldMapping: `{"pack_number":1,"downloads_count":2,"filesize":3,"filename":4}`,
			Priority:     80,
		},
		{
			Name:         "hash-bracket-file",
			Regex:        `#(\d+)\s+\[([^\]]+)\]\s+(.+)`,
			FieldMapping: `{"pack_number":1,"filesize":2,"filename":3}`,
			Priority:     50,
		},
		{
			Name:         "hash-file-bracket",
			Regex:        `#(\d+)\s+(.+?)\s+\[([^\]]+)\]\s*$`,
			FieldMapping: `{"pack_number":1,"filename":2,"filesize":3}`,
			Priority:     45,
		},
		{
			Name:         "minimal-hash-file",
			Regex:        `#(\d+)\s+(.+\.\w{2,4})\s*$`,
			FieldMapping: `{"pack_number":1,"filename":2}`,
			Priority:     10,
		},
		// BotReign/search-bot format: NNN) Nx | SizeU | filename | /msg BotNick XDCC SEND PackNum
		{
			Name:         "botreign-pipe-xdcc",
			Regex:        `^\d+\)\s+(\d+)x\s*\|\s*([0-9][0-9.]*[KMGTP]?)\s*\|\s*(.+?)\s*\|\s*/msg\s+(\S+)\s+XDCC\s+SEND\s+(\d+)`,
			FieldMapping: `{"downloads_count":1,"filesize":2,"filename":3,"bot_nick":4,"pack_number":5}`,
			Priority:     75,
		},
		// EliteWarez/EWG format: filename  (Command:  /msg PackBot XDCC SEND #N  ) Gets: N Size: SizeU
		{
			Name:         "ewg-command-xdcc",
			Regex:        `^(.+?)\s{2,}\(Command:\s*/msg\s+(\S+)\s+XDCC\s+SEND\s+#(\d+)\s*\)\s*Gets:\s*(\d+)\s*Size:\s*([0-9][0-9.]*\s*[KMGTP]?B?)`,
			FieldMapping: `{"filename":1,"bot_nick":2,"pack_number":3,"downloads_count":4,"filesize":5}`,
			Priority:     88,
		},
		// Beast/paren format: (SizeU) filename (Nx) /msg BotNick xdcc send #N
		{
			Name:         "paren-file-msg-xdcc",
			Regex:        `^\(([0-9]+(?:\.[0-9]+)?[KMGTP]B?)\)\s+(.+?)\s+\((\d+)x\)\s+/msg\s+(\S+)\s+xdcc\s+send\s+#(\d+)`,
			FieldMapping: `{"filesize":1,"filename":2,"downloads_count":3,"bot_nick":4,"pack_number":5}`,
			Priority:     82,
		},
	}
}

// SeedPatterns inserts built-in patterns into the database if they don't already exist.
// Existing patterns (matched by name) are left untouched so user edits are preserved.
// Auto-disabled builtin patterns are re-enabled so they remain available after server restarts.
func SeedPatterns(store db.Store) error {
	existing, err := store.GetAllParsePatterns()
	if err != nil {
		return err
	}

	existingByName := make(map[string]*db.ParsePattern)
	for i := range existing {
		existingByName[existing[i].Name] = &existing[i]
	}

	reenabled := 0
	for _, bp := range BuiltinPatterns() {
		if p, found := existingByName[bp.Name]; found {
			// Ensure builtin patterns are always globally scoped and enabled.
			// A user may have accidentally scoped a pattern to a specific channel
			// via the realm form, which would prevent it from working on other channels.
			needsUpdate := false
			if p.AutoDisabled || !p.Enabled {
				p.AutoDisabled = false
				p.Enabled = true
				p.FailCount = 0
				needsUpdate = true
				reenabled++
			}
			if p.ServerID != nil || p.Channel != "" {
				p.ServerID = nil
				p.Channel = ""
				needsUpdate = true
				log.Printf("SeedPatterns: reset scope of builtin pattern %q to global", p.Name)
			}
			if !p.Builtin {
				p.Builtin = true
				needsUpdate = true
			}
			if needsUpdate {
				if err := store.UpdateParsePattern(p); err != nil {
					return err
				}
			}
			continue
		}
		p := &db.ParsePattern{
			Name:         bp.Name,
			Regex:        bp.Regex,
			FieldMapping: bp.FieldMapping,
			Priority:     bp.Priority,
			Builtin:      true,
			Enabled:      true,
		}
		if err := store.CreateParsePattern(p); err != nil {
			return err
		}
	}

	if reenabled > 0 {
		log.Printf("SeedPatterns: re-enabled %d auto-disabled builtin parse patterns", reenabled)
	}

	return nil
}

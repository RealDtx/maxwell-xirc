package parser

import "github.com/maxwell-xirc/xirc/db"

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
	}
}

// SeedPatterns inserts built-in patterns into the database if they don't already exist.
// Existing patterns (matched by name) are left untouched so user edits are preserved.
func SeedPatterns(store db.Store) error {
	existing, err := store.GetParsePatterns()
	if err != nil {
		return err
	}

	existingNames := make(map[string]bool)
	for _, p := range existing {
		existingNames[p.Name] = true
	}

	for _, bp := range BuiltinPatterns() {
		if existingNames[bp.Name] {
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

	return nil
}

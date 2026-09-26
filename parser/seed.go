package parser

import (
	"log"
	"regexp"

	"github.com/RealDtx/maxwell-irc/db"
)

type BuiltinPattern struct {
	Name     string
	Regex    string
	Priority int
	// Dead marks generic formats that never matched real traffic (1.9M log
	// lines, 2026-09). Seeded disabled; users can enable them.
	Dead bool
}

func BuiltinPatterns() []BuiltinPattern {
	return []BuiltinPattern{
		{
			Name:     "hash-x-bracket",
			Regex:    `#(?<pack_number>\d+)\s+(?<downloads_count>\d+)x\s+\[(?<filesize>[^\]]+)\]\s+(?<filename>.+)`,
			Priority: 100,
		},
		{
			Name:     "bracket-hash-dash",
			Regex:    `\[#0*(?<pack_number>\d+)\]\s*-\s*(?<downloads_count>\d+)x\s*-\s*\[(?<filesize>[^\]]+)\]\s*-\s*(?<filename>.+)`,
			Priority: 95,
			Dead:     true,
		},
		{
			Name:     "pipe-separated",
			Regex:    `Pack\s+#(?<pack_number>\d+)\s*\|\s*(?<downloads_count>\d+)\s+downloads?\s*\|\s*(?<filesize>[^\|]+?)\s*\|\s*(?<filename>.+)`,
			Priority: 90,
			Dead:     true,
		},
		{
			Name:     "hash-file-bracket-trailing",
			Regex:    `#(?<pack_number>\d+)\s+(?<filename>.+?)\s+\[(?<filesize>[^\]]+)\]\s+\((?<downloads_count>\d+)\s+downloads?\)`,
			Priority: 85,
			Dead:     true,
		},
		{
			Name:     "triple-colon",
			Regex:    `#(?<pack_number>\d+)\s+:::\s+(?<downloads_count>\d+)x\s+:::\s+\[(?<filesize>[^\]]+)\]\s+:::\s+(?<filename>.+)`,
			Priority: 80,
			Dead:     true,
		},
		{
			Name:     "hash-bracket-file",
			Regex:    `#(?<pack_number>\d+)\s+\[(?<filesize>[^\]]+)\]\s+(?<filename>.+)`,
			Priority: 50,
			Dead:     true,
		},
		{
			Name:     "hash-file-bracket",
			Regex:    `#(?<pack_number>\d+)\s+(?<filename>.+?)\s+\[(?<filesize>[^\]]+)\]\s*$`,
			Priority: 45,
			Dead:     true,
		},
		{
			Name:     "minimal-hash-file",
			Regex:    `#(?<pack_number>\d+)\s+(?<filename>.+\.\w{2,4})\s*$`,
			Priority: 10,
			Dead:     true,
		},
		// BotReign/search-bot format: NNN) Nx | SizeU | filename | /msg BotNick XDCC SEND PackNum
		{
			Name:     "botreign-pipe-xdcc",
			Regex:    `^\d+\)\s+(?<downloads_count>\d+)x\s*\|\s*(?<filesize>[0-9][0-9.]*[KMGTP]?)\s*\|\s*(?<filename>.+?)\s*\|\s*/msg\s+(?<bot_nick>\S+)\s+XDCC\s+SEND\s+(?<pack_number>\d+)`,
			Priority: 75,
		},
		// EliteWarez/EWG format: filename  (Command:  /msg PackBot XDCC SEND #N  ) Gets: N Size: SizeU
		{
			Name:     "ewg-command-xdcc",
			Regex:    `^(?<filename>.+?)\s{2,}\(Command:\s*/msg\s+(?<bot_nick>\S+)\s+XDCC\s+SEND\s+#(?<pack_number>\d+)\s*\)\s*Gets:\s*(?<downloads_count>\d+)\s*Size:\s*(?<filesize>[0-9][0-9.]*\s*[KMGTP]?B?)`,
			Priority: 88,
		},
		// Beast/paren format: (SizeU) filename (Nx) /msg BotNick xdcc send #N
		{
			Name:     "paren-file-msg-xdcc",
			Regex:    `^\((?<filesize>[0-9]+(?:\.[0-9]+)?[KMGTP]B?)\)\s+(?<filename>.+?)\s+\((?<downloads_count>\d+)x\)\s+/msg\s+(?<bot_nick>\S+)\s+xdcc\s+send\s+#(?<pack_number>\d+)`,
			Priority: 82,
		},
		// MG relayed search-result format (optional "pred ... ago" segment):
		// ( #CHANNEL )-( filename )-( TAG x SizeU )-...-( /msg BotNick xdcc send #N )
		{
			Name:     "paren-relay-xdcc",
			Regex:    `(?i)^\(\s*#\S+\s*\)-\(\s*(?<filename>.+?)\s*\)-\(\s*\S+\s+x\s+(?<filesize>[0-9][0-9.]*\s*[KMGTP]?B?)\s*\).*?/msg\s+(?<bot_nick>\S+)\s+xdcc\s+send\s+#?(?<pack_number>\d+)`,
			Priority: 78,
		},
		// New-pack announcement with size: added - [SizeU] - filename - /MSG BotNick XDCC SEND N
		{
			Name:     "added-size-dash-xdcc",
			Regex:    `(?i)^added\s+-\s+\[(?<filesize>[^\]]+)\]\s+-\s+(?<filename>.+?)\s+-\s+/msg\s+(?<bot_nick>\S+)\s+XDCC\s+SEND\s+(?<pack_number>\d+)`,
			Priority: 77,
		},
		// New-pack announcement (EWG): Added - filename - /MSG BotNick XDCC SEND N
		{
			Name:     "added-dash-xdcc",
			Regex:    `(?i)^added\s+-\s+(?<filename>.+?)\s+-\s+/msg\s+(?<bot_nick>\S+)\s+XDCC\s+SEND\s+(?<pack_number>\d+)`,
			Priority: 76,
		},
	}
}

// namedGroupRe matches the opening of a named capture group.
var namedGroupRe = regexp.MustCompile(`\(\?P?<\w+>`)

// positional returns regex with named groups turned into plain groups — the
// pre-named-groups form of a builtin, used to detect unmodified old rows.
func positional(regex string) string {
	return namedGroupRe.ReplaceAllString(regex, "(")
}

// SeedPatterns inserts built-in patterns into the database if they don't already exist.
// Existing patterns (matched by name) are left untouched so user edits are preserved,
// except that unmodified legacy (positional field_mapping) builtins are upgraded once
// to named groups; dead builtins that never matched are disabled during that upgrade.
func SeedPatterns(store db.Store) error {
	existing, err := store.GetAllParsePatterns()
	if err != nil {
		return err
	}

	existingByName := make(map[string]*db.ParsePattern)
	for i := range existing {
		existingByName[existing[i].Name] = &existing[i]
	}

	for _, bp := range BuiltinPatterns() {
		if p, found := existingByName[bp.Name]; found {
			// Ensure builtin patterns are always globally scoped.
			// A user may have accidentally scoped a pattern to a specific channel
			// via the realm form, which would prevent it from working on other channels.
			needsUpdate := false
			if p.Regex != bp.Regex && p.Regex == positional(bp.Regex) {
				p.Regex = bp.Regex
				p.FieldMapping = "{}"
				if bp.Dead && p.MatchCount == 0 {
					p.Enabled = false
					log.Printf("SeedPatterns: disabled never-matching builtin pattern %q", p.Name)
				}
				needsUpdate = true
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
			FieldMapping: "{}",
			Priority:     bp.Priority,
			Builtin:      true,
			Enabled:      !bp.Dead,
		}
		if err := store.CreateParsePattern(p); err != nil {
			return err
		}
	}

	return nil
}

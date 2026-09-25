package library

import "testing"

func TestDetect_PiTree(t *testing.T) {
	root := buildPiTree(t)
	cfg := Detect(root)

	if cfg.MediaRoot != root {
		t.Errorf("media_root = %q, want %q", cfg.MediaRoot, root)
	}
	if !cfg.AutoOrganize {
		t.Error("expected auto_organize on by default")
	}
	if len(cfg.Categories) != len(kindOrder) {
		t.Fatalf("expected %d categories, got %d", len(kindOrder), len(cfg.Categories))
	}

	byKind := make(map[string]Category, len(cfg.Categories))
	for _, c := range cfg.Categories {
		byKind[c.Kind] = c
	}

	tests := []struct {
		kind string
		dir  string
	}{
		{"series", "Series"},
		{"movie", "Movies"},
		{"show", "Show"},
		// Not present on the tree — falls back to the English default name.
		{"music", "Music"},
		{"magazine", "Magazines"},
		{"ebook", "Books"},
		{"game", "Games"},
		{"software", "Software"},
	}
	for _, tt := range tests {
		c, ok := byKind[tt.kind]
		if !ok {
			t.Errorf("missing category for kind %q", tt.kind)
			continue
		}
		if c.Dir != tt.dir {
			t.Errorf("%s dir = %q, want %q", tt.kind, c.Dir, tt.dir)
		}
		if !c.Enabled {
			t.Errorf("%s: expected enabled", tt.kind)
		}
	}

	// 5x "SNN" style (Skinwalker Ranch x4, Rick & Morty x1) beats 3x "snn"
	// (Star Trek SNW) and 1x "SeasonN" (Ghosts) — most common wins.
	if got := byKind["series"].SeasonDir; got != "S{season:02}" {
		t.Errorf("series season_dir = %q, want %q", got, "S{season:02}")
	}

	// 27 of 29 movie files sit flat — more than half — so path stays flat.
	if got := byKind["movie"].Path; got != "" {
		t.Errorf("movie path = %q, want flat (\"\")", got)
	}
}

func TestDetect_MovieSubfoldered(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Movies/Foo (2020)", "Movies/Bar (2021)")
	touch(t, root, "Movies/Foo (2020)/Foo.2020.mkv")
	touch(t, root, "Movies/Bar (2021)/Bar.2021.mkv")

	cfg := Detect(root)
	for _, c := range cfg.Categories {
		if c.Kind == "movie" {
			if c.Path != "{title} ({year})" {
				t.Errorf("movie path = %q, want %q", c.Path, "{title} ({year})")
			}
			return
		}
	}
	t.Fatal("no movie category in Detect output")
}

func TestDetect_SeasonDirStyle_SeasonN(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Series/Ghosts/Season5")

	cfg := Detect(root)
	for _, c := range cfg.Categories {
		if c.Kind == "series" {
			if c.SeasonDir != "Season{season}" {
				t.Errorf("season_dir = %q, want %q", c.SeasonDir, "Season{season}")
			}
			return
		}
	}
	t.Fatal("no series category in Detect output")
}

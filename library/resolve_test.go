package library

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolve_PiSamples runs Resolve for the real (and a few invented)
// filenames from the plan's Verification section against a config detected
// from a Pi-layout replica, checking the resulting directory relative to
// media_root. This exercises B2 (per-kind parsing), B3 (existing-folder
// reuse) and B4 (Detect) together, the same way the real engine would.
func TestResolve_PiSamples(t *testing.T) {
	root := buildPiTree(t)
	cfg := Detect(root)

	tests := []struct {
		name     string
		filename string
		wantDir  string // relative to root
	}{
		{"simpsons flat, no season folder", "The.Simpsons.S37E01.WEB-GRP.mkv", "Series/Simpsons"},
		{"rick and morty name match", "rick.and.morty.s07e04.WEB-GRP.mkv", "Series/Rick & Morty/S07"},
		{"strange new worlds lowercase season style", "Star.Trek.Strange.New.Worlds.S04E10.WEB-GRP.mkv", "Series/Star Trek Strange New Worlds/s04"},
		{"fargo show+season folder created", "Fargo.S05E01.WEB-GRP.mkv", "Series/Fargo/S05"},
		{"james bond collection folder reused", "James.Bond.007.GoldenEye.1995.WEB-GRP.mkv", "Movies/James Bond"},
		{"oppenheimer flat movie", "Oppenheimer.2023.WEB-GRP.mkv", "Movies"},
		{"glastonbury concert", "Glastonbury.2023.Viagra.Boys.WEB-GRP.mkv", "Show"},
		{"louis ck standup", "Louis.C.K.Live.at.the.Dolby.2023.WEB-GRP.mkv", "Show"},
		{"magazine issue", "Stiftung.Warentest.Finanztest.Nr.10.2025.DE.MAGAZiNE.PDF.EBOOK-MG.pdf", "Magazines/Stiftung Warentest Finanztest/2025"},
		{"music scene album", "Daft_Punk-Random_Access_Memories-WEB-2013-GRP.zip", "Music/Daft Punk/Random Access Memories (2013)"},
		{"music single, no album", "Artist - Song.mp3", "Music/Artist"},
		{"game scene release", "Cyberpunk.2077.Phantom.Liberty-RUNE.iso", "Games/Cyberpunk 2077 Phantom Liberty"},
		{"software versioned zip", "Blender.v4.2.x64.zip", "Software/Blender"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, cat, fields, ok := Resolve(&cfg, tt.filename)
			if !ok {
				t.Fatalf("Resolve(%q): no category matched", tt.filename)
			}
			want := filepath.Join(root, tt.wantDir)
			if dir != want {
				t.Errorf("Resolve(%q) = %q (category %s, fields %v), want %q", tt.filename, dir, cat.ID, fields, want)
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Errorf("Resolve(%q): resulting dir %q was not created", tt.filename, dir)
			}
		})
	}
}

func TestResolve_NoMatch(t *testing.T) {
	root := buildPiTree(t)
	cfg := Detect(root)
	if _, _, _, ok := Resolve(&cfg, "readme.txt"); ok {
		t.Error("expected no category to match readme.txt")
	}
}

func TestResolve_CreateFoldersOffStopsAtDeepestExisting(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Cat/Show1")

	cat := Category{ID: "t", Kind: "magazine", Dir: "Cat", Path: "{title}/{missing}", CreateFolders: false}

	// Segment 1 exists (reused, regardless of create_folders), segment 2
	// ("missing") does not and create_folders is off — dir must stop there.
	dir := resolveTemplateDir(&cat, filepath.Join(root, "Cat"), map[string]string{"title": "Show1", "missing": "New"}, false)
	want := filepath.Join(root, "Cat", "Show1")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}
	if _, err := os.Stat(filepath.Join(root, "Cat", "Show1", "New")); err == nil {
		t.Error("expected the second segment not to be created")
	}

	// Segment 1 itself doesn't exist and create_folders is off — stop at
	// the category dir.
	dir = resolveTemplateDir(&cat, filepath.Join(root, "Cat"), map[string]string{"title": "NoSuchShow", "missing": "New"}, false)
	want = filepath.Join(root, "Cat")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}
}

func TestFindExistingMatch(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "Rick & Morty", "Simpsons", "Skinwalker Ranch")

	tests := []struct {
		target string
		want   string
	}{
		{"rick.and.morty", "Rick & Morty"},
		{"The Simpsons", "Simpsons"},
		{"skinwalker.ranch.extended", "Skinwalker Ranch"},
		{"Completely Unrelated", ""},
	}
	for _, tt := range tests {
		if got := findExistingMatch(root, tt.target); got != tt.want {
			t.Errorf("findExistingMatch(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestRenderTemplateSegments(t *testing.T) {
	tests := []struct {
		tmpl   string
		fields map[string]string
		want   []string
	}{
		{"{title}/{season_dir}", map[string]string{"title": "Fargo"}, []string{"Fargo"}},
		{"{album} ({year})", map[string]string{"album": "Foo"}, []string{"Foo"}},
		{"{album} ({year})", map[string]string{"album": "Foo", "year": "2013"}, []string{"Foo (2013)"}},
		{"{album} ({year})", map[string]string{}, nil},
		{"", map[string]string{"title": "X"}, nil},
	}
	for _, tt := range tests {
		got := renderTemplateSegments(tt.tmpl, tt.fields)
		if !equalStrings(got, tt.want) {
			t.Errorf("renderTemplateSegments(%q, %v) = %v, want %v", tt.tmpl, tt.fields, got, tt.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

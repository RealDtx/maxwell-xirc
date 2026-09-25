package library

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildPiTree replicates (a scaled-down version of) the real Pi's
// /data/media layout described in the library plan, so B2/B3 path
// resolution and B4 detection can be tested against realistic folder
// reuse/season-style/flat-vs-collection scenarios instead of a synthetic one.
func buildPiTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mkdirs(t, root,
		"Movies/James Bond",
		"Series/Skinwalker Ranch/S01",
		"Series/Skinwalker Ranch/S02",
		"Series/Skinwalker Ranch/S04",
		"Series/Skinwalker Ranch/S06",
		"Series/Rick & Morty/S07",
		"Series/Star Trek Strange New Worlds/s02",
		"Series/Star Trek Strange New Worlds/s03",
		"Series/Star Trek Strange New Worlds/s04",
		"Series/Ghosts/Season5",
		"Series/Ghosts/German",
		"Series/Scrubs",
		"Series/Simpsons",
		"Show",
		"Downloads",
	)

	// Movies: 27 flat files, 2 inside the "James Bond" collection folder —
	// more than half flat, as on the Pi (27 of 29).
	for i := 1; i <= 27; i++ {
		touch(t, root, fmt.Sprintf("Movies/Filler.Movie.%d.2020.WEB-GRP.mkv", i))
	}
	touch(t, root, "Movies/James Bond/James.Bond.007.Skyfall.2012.WEB-GRP.mkv")
	touch(t, root, "Movies/James Bond/James.Bond.007.Spectre.2015.WEB-GRP.mkv")

	for _, f := range []string{
		"Series/Skinwalker Ranch/S01/ep1.mkv", "Series/Skinwalker Ranch/S02/ep1.mkv",
		"Series/Skinwalker Ranch/S04/ep1.mkv", "Series/Skinwalker Ranch/S06/ep1.mkv",
		"Series/Rick & Morty/S07/ep1.mkv",
		"Series/Star Trek Strange New Worlds/s02/ep1.mkv",
		"Series/Star Trek Strange New Worlds/s03/ep1.mkv",
		"Series/Star Trek Strange New Worlds/s04/ep1.mkv",
		"Series/Ghosts/Season5/ep1.mkv", "Series/Ghosts/German/ep1.mkv",
		"Series/Scrubs/Scrubs.S01E01.mkv", "Series/Scrubs/Scrubs.S01E02.mkv",
		"Series/Simpsons/The.Simpsons.S01E01.mkv", "Series/Simpsons/The.Simpsons.S01E02.mkv",
		"Show/Glastonbury.2019.WEB-GRP.mkv",
		"Series/Fargo.S01E01.mkv", // loose episode directly in Series/, no "Fargo" folder
	} {
		touch(t, root, f)
	}
	for i := 1; i <= 7; i++ {
		touch(t, root, fmt.Sprintf("Downloads/Stiftung.Warentest.Finanztest.Nr.%d.2024.DE.MAGAZiNE.PDF.EBOOK-MG.pdf", i))
	}
	return root
}

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0775); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
}

func touch(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0775); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatalf("touch %s: %v", rel, err)
	}
	f.Close()
}

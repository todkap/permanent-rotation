package enrich

import (
	"testing"
	"time"

	"github.com/todkap/permanent-rotation/internal/csvio"
)

func TestAssignTiers(t *testing.T) {
	recs := []Record{
		{Song: "a", LastfmPlaycount: 900},
		{Song: "b", LastfmPlaycount: 800},
		{Song: "c", LastfmPlaycount: 700},
		{Song: "d", LastfmPlaycount: 600},
		{Song: "e", LastfmPlaycount: 500},
		{Song: "f", LastfmPlaycount: 400},
		{Song: "g", LastfmPlaycount: 300},
		{Song: "h", LastfmPlaycount: 200},
		{Song: "i", LastfmPlaycount: 100},
	}
	AssignTiers(recs)
	got := map[string]string{}
	for _, r := range recs {
		got[r.Song] = r.Tier
	}
	if got["a"] != "canon" || got["b"] != "canon" || got["c"] != "canon" {
		t.Fatalf("top third should be canon: %v", got)
	}
	if got["g"] != "deep-cut" || got["h"] != "deep-cut" || got["i"] != "deep-cut" {
		t.Fatalf("bottom third should be deep-cut: %v", got)
	}
	for _, mid := range []string{"d", "e", "f"} {
		if got[mid] != "middle" {
			t.Fatalf("%s should be middle: %v", mid, got)
		}
	}
}

func TestMergeKeepsMemoryVerbatimAndOverridesWin(t *testing.T) {
	songs := []csvio.Song{
		{Song: "Mayonaise", Artist: "Smashing Pumpkins", LifeYear: 1994, Chapter: "College", Memory: "  kept exactly as typed  "},
	}
	fetched := map[string]Record{
		"mayonaise|smashing pumpkins": {SpotifyID: "wrong", ReleaseYear: 2020, LastfmPlaycount: 10},
	}
	overrides := map[string]Override{
		"mayonaise|smashing pumpkins": {SpotifyID: "pinned123", ReleaseYear: 1993},
	}
	out, problems := Merge(songs, fetched, &File{}, overrides, time.Now())
	if len(problems) != 0 || len(out.Songs) != 1 {
		t.Fatalf("merge: %v %v", out.Songs, problems)
	}
	r := out.Songs[0]
	if r.Memory != "  kept exactly as typed  " {
		t.Fatalf("memory must be verbatim: %q", r.Memory)
	}
	if r.SpotifyID != "pinned123" || r.ReleaseYear != 1993 || r.MatchConfidence != "manual" {
		t.Fatalf("override should win: %+v", r)
	}
}

func TestUnmatchedIsListedNotInvented(t *testing.T) {
	songs := []csvio.Song{{Song: "Ghost", Artist: "Nobody"}}
	out, problems := Merge(songs, map[string]Record{}, &File{}, nil, time.Now())
	if len(out.Songs) != 0 || len(problems) != 1 {
		t.Fatalf("unmatched song should be listed and skipped: %v %v", out.Songs, problems)
	}
}

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

func TestUnmatchedRendersWithoutInventedData(t *testing.T) {
	songs := []csvio.Song{{Song: "Deep Cover", Artist: "Dr. Dre and Snoop Dogg", LifeYear: 1991, Chapter: "Starting College", Memory: "verbatim words"}}
	fetched := map[string]Record{
		"deep cover|dr. dre and snoop dogg": {MatchConfidence: "unmatched"},
	}
	out, problems := Merge(songs, fetched, &File{}, nil, time.Now())
	if len(out.Songs) != 1 {
		t.Fatalf("unmatched song should still render: %v", out.Songs)
	}
	r := out.Songs[0]
	if r.SpotifyID != "" || r.SpotifyURL != "" || r.ReleaseYear != 0 || r.Album != "" {
		t.Fatalf("nothing may be invented for an unmatched song: %+v", r)
	}
	if r.Memory != "verbatim words" || r.MatchConfidence != "unmatched" {
		t.Fatalf("CSV data must survive: %+v", r)
	}
	if len(problems) != 1 {
		t.Fatalf("unmatched should be flagged: %v", problems)
	}
}

func TestZeroPlaycountGetsNoTier(t *testing.T) {
	recs := []Record{
		{Song: "a", LastfmPlaycount: 100},
		{Song: "b", LastfmPlaycount: 50},
		{Song: "c", LastfmPlaycount: 10},
		{Song: "unmatched", LastfmPlaycount: 0},
	}
	AssignTiers(recs)
	if recs[3].Tier != "" {
		t.Fatalf("no metrics, no tier: %+v", recs[3])
	}
	if recs[0].Tier != "canon" || recs[2].Tier != "deep-cut" {
		t.Fatalf("tiering of the metric-bearing records changed: %v %v", recs[0].Tier, recs[2].Tier)
	}
}

func TestOverridePinsAlbumAndYear(t *testing.T) {
	songs := []csvio.Song{{Song: "So What'Cha Want", Artist: "Beastie Boys"}}
	fetched := map[string]Record{key("So What'Cha Want", "Beastie Boys"): {
		Song: "So What'Cha Want", Artist: "Beastie Boys",
		SpotifyID: "25pl7kV4OxafIc9wPAggYf", Album: "Anthology: The Sounds Of Science", ReleaseYear: 1999,
	}}
	ov := map[string]Override{key("So What'Cha Want", "Beastie Boys"): {
		SpotifyID: "5h9mwffuipb2TSeTFp8Pu0", ReleaseYear: 1992, Album: "Check Your Head",
	}}
	out, _ := Merge(songs, fetched, &File{}, ov, time.Now())
	r := out.Songs[0]
	if r.SpotifyID != "5h9mwffuipb2TSeTFp8Pu0" || r.ReleaseYear != 1992 || r.Album != "Check Your Head" {
		t.Fatalf("override: %+v", r)
	}
	if r.MatchConfidence != "manual" {
		t.Fatalf("confidence: %s", r.MatchConfidence)
	}
}

func TestSeenLiveAndArtworkPassThrough(t *testing.T) {
	songs := []csvio.Song{{Song: "A", Artist: "B", SeenLive: "Boston, 1995", Memory: "My words."}}
	cached := &File{Songs: []Record{{Song: "A", Artist: "B", SpotifyID: "id1", AlbumArt: "https://example.com/one.jpg"}}}
	out, _ := Merge(songs, nil, cached, nil, time.Now())
	if out.Songs[0].SeenLive != "Boston, 1995" || out.Songs[0].AlbumArt != cached.Songs[0].AlbumArt || out.Songs[0].Memory != "My words." {
		t.Fatal(out.Songs[0])
	}
	out, _ = Merge(songs, nil, cached, map[string]Override{"a|b": {SpotifyID: "id2"}}, time.Now())
	if out.Songs[0].AlbumArt != "" {
		t.Fatal("changing pinned ID must clear old artwork")
	}
}

func TestTitleCorrectionCarriesPinnedRecord(t *testing.T) {
	oldSong := "Freshman"
	songs := []csvio.Song{{Song: "The Freshmen", Artist: "Verve Pipe", SeenLive: "Cincinnati, 2026", Memory: "My words."}}
	cached := &File{Songs: []Record{{Song: oldSong, Artist: "Verve Pipe", SpotifyID: "pinned", Album: "Villains", ReleaseYear: 1996, AlbumArt: "https://example.com/art.jpg", LastfmListeners: 10, LastfmPlaycount: 20}}}
	out, _ := Merge(songs, map[string]Record{"the freshmen|verve pipe": {Song: "The Freshmen", Artist: "Verve Pipe", SpotifyID: "pinned", Album: "Villains", ReleaseYear: 1996, AlbumArt: "https://example.com/art.jpg"}}, cached, nil, time.Now())
	if out.Songs[0].SpotifyID != "pinned" || out.Songs[0].Album != "Villains" || out.Songs[0].AlbumArt == "" || out.Songs[0].SeenLive != "Cincinnati, 2026" || out.Songs[0].Memory != "My words." {
		t.Fatal(out.Songs[0])
	}
}

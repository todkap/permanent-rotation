package csvio

import (
	"fmt"
	"strings"
	"testing"
)

func genCSV(n int) string {
	var b strings.Builder
	b.WriteString("song,artist,era,tags,memory,angle\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "Song %d,Artist %d,College (~1991),tag,a memory,\n", i, i)
	}
	return b.String()
}

func TestMinimumEnforced(t *testing.T) {
	if _, err := Read(strings.NewReader(genCSV(24)), DefaultMinSongs); err == nil {
		t.Fatal("24 rows should be rejected")
	}
	if _, err := Read(strings.NewReader(genCSV(25)), DefaultMinSongs); err != nil {
		t.Fatalf("25 rows should be accepted: %v", err)
	}
	got, err := Read(strings.NewReader(genCSV(100)), DefaultMinSongs)
	if err != nil || len(got) != 100 {
		t.Fatalf("100 rows should be accepted: %v (%d)", err, len(got))
	}
}

func TestMinFlagOverride(t *testing.T) {
	if _, err := Read(strings.NewReader(genCSV(3)), 3); err != nil {
		t.Fatalf("template with --min 3 should pass: %v", err)
	}
}

// The real list: messy headers, typos in titles, a trailing slash in tags.
// Parsing it must keep every memory verbatim.
func TestRealList(t *testing.T) {
	songs, err := Load("../../examples/todd/songs.csv", DefaultMinSongs)
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 41 {
		t.Fatalf("expected 41 rows, got %d", len(songs))
	}
	var regret *Song
	for i := range songs {
		if songs[i].Song == "Regret" {
			regret = &songs[i]
		}
	}
	if regret == nil {
		t.Fatal("Regret not found")
	}
	if regret.Artist != "New Order" {
		t.Fatalf("artist mismatch: %q", regret.Artist)
	}
	if !strings.HasPrefix(regret.Memory, "Getting exposed to") {
		t.Fatalf("memory column not mapped: %q", regret.Memory)
	}
	if regret.Chapter != "College / Dorm Life" || regret.LifeYear != 1993 {
		t.Fatalf("era parse: %q %d", regret.Chapter, regret.LifeYear)
	}
	for _, s := range songs {
		if s.Chapter == "" || s.LifeYear == 0 {
			t.Errorf("row %q missing era parse (%q)", s.Song, s.Era)
		}
	}
}

func TestParseEra(t *testing.T) {
	cases := []struct {
		era, chapter string
		year         int
	}{
		{"College Freshman Year (~1991)", "College Freshman Year", 1991},
		{" Middle School (~1985)", "Middle School", 1985},
		{"Marriage & Family (~1994-Present)", "Marriage & Family", 1994},
		{"Childhood / Prospect St (~1985–1986)", "Childhood / Prospect St", 1985}, // en dash range: start year wins
		{"Middle Age & Family (~mid-2000s)", "Middle Age & Family", 2000},
		{"Childhood / Prospect St (~85)", "Childhood / Prospect St", 1985},
		{"No year at all", "No year at all", 0},
	}
	for _, c := range cases {
		ch, y := ParseEra(c.era)
		if ch != c.chapter || y != c.year {
			t.Errorf("%q -> %q, %d; want %q, %d", c.era, ch, y, c.chapter, c.year)
		}
	}
}

func TestAngleNeverRequired(t *testing.T) {
	// a 5-column file (no angle column) still parses positionally
	var b strings.Builder
	b.WriteString("song,artist,era,tags,memory\n")
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "S%d,A%d,Era (~1990),tag,mem\n", i, i)
	}
	got, err := Read(strings.NewReader(b.String()), DefaultMinSongs)
	if err != nil || len(got) != 25 {
		t.Fatalf("5-column csv: %v", err)
	}
}

func TestParseEraPrefersParenYear(t *testing.T) {
	cases := []struct {
		era  string
		year int
	}{
		{"Fatherhood / Early 2000s (~2003)", 2003}, // label digits lose to parens
		{"Mid-2000s / Pop-Punk Pivot (~2006)", 2006},
		{"2010s / Family & Reflection (~2013)", 2013},
		{"Late 2010s / 2020s Threshold (~2019)", 2019},
		{"Late Discovery / Wilmington Era (~2000s)", 2000}, // decade in parens still parses
		{"College / Dorm Life (~1993)", 1993},              // unchanged behavior
		{"College Roommates (~1992–1993)", 1992},           // range takes the start year
	}
	for _, c := range cases {
		if _, y := ParseEra(c.era); y != c.year {
			t.Fatalf("ParseEra(%q) year = %d, want %d", c.era, y, c.year)
		}
	}
}

func TestParseEraFallsBackToLabelDigits(t *testing.T) {
	// no parens at all: first digits in the label, as before
	if _, y := ParseEra("Late 2000s Era"); y != 2000 {
		t.Fatalf("label-digit fallback = %d, want 2000", y)
	}
	// parens present but no 4-digit year inside: label digits still win
	if _, y := ParseEra("Mid-2000s Era (~back then)"); y != 2000 {
		t.Fatalf("empty-parens fallback = %d, want 2000", y)
	}
}

package site

import (
	"testing"

	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/notes"
)

func TestChaptersSortByEarliestSongYear(t *testing.T) {
	recs := []enrich.Record{
		// Middle School's first-listed song is 1985, latest is 1986;
		// the chapter key must be the EARLIEST year (1985)
		{Song: "Home Sweet Home", Artist: "Motley Crue", Chapter: "Middle School", LifeYear: 1985},
		{Song: "Dreams", Artist: "Van Halen", Chapter: "Middle School", LifeYear: 1986},
		// Prospect St's range parses to its start year 1985, tying Middle
		// School's earliest; the tie breaks alphabetically so Childhood
		// still comes before Middle School
		{Song: "Silent Running", Artist: "Mike + The Mechanics", Chapter: "Childhood / Prospect St", LifeYear: 1985},
		{Song: "Later Song", Artist: "X", Chapter: "College", LifeYear: 1991},
	}
	ch := groupByChapter(recs, &notes.Context{})
	got := []string{ch[0].Name, ch[1].Name, ch[2].Name}
	want := []string{"Childhood / Prospect St", "Middle School", "College"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chapter order %v, want %v", got, want)
		}
	}
	// within a chapter, songs run early to late
	if ch[1].Songs[0].Song != "Home Sweet Home" {
		t.Fatalf("within-chapter order wrong: %+v", ch[1].Songs)
	}
}

func TestUnknownYearSinksToEnd(t *testing.T) {
	recs := []enrich.Record{
		{Song: "No Year", Artist: "A", Chapter: "Mystery Era", LifeYear: 0},
		{Song: "Dated", Artist: "B", Chapter: "Nineties", LifeYear: 1994},
		{Song: "No Year 2", Artist: "C", Chapter: "Nineties", LifeYear: 0},
	}
	ch := groupByChapter(recs, &notes.Context{})
	if ch[0].Name != "Nineties" || ch[1].Name != "Mystery Era" {
		t.Fatalf("unknown-year chapter should sort last: %v %v", ch[0].Name, ch[1].Name)
	}
	if ch[0].Songs[0].Song != "Dated" {
		t.Fatalf("unknown-year song should sort last within chapter: %+v", ch[0].Songs)
	}
}

func TestStageRankBreaksYearTies(t *testing.T) {
	recs := []enrich.Record{
		{Song: "A", Artist: "X", Chapter: "College / First Girlfriend", LifeYear: 1990},
		{Song: "B", Artist: "X", Chapter: "High School Prom", LifeYear: 1990},
		{Song: "C", Artist: "X", Chapter: "High School to College Transition", LifeYear: 1990},
		{Song: "D", Artist: "X", Chapter: "Starting College", LifeYear: 1991},
	}
	ch := groupByChapter(recs, &notes.Context{})
	got := []string{ch[0].Name, ch[1].Name, ch[2].Name, ch[3].Name}
	want := []string{"High School Prom", "High School to College Transition", "College / First Girlfriend", "Starting College"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestUnknownStageLandsMidRank(t *testing.T) {
	// same-year tie: a stage-less label sorts after school/college labels
	// and before adult-life labels, never first and never sunk to the end
	recs := []enrich.Record{
		{Song: "A", Artist: "X", Chapter: "Pop-Punk Era", LifeYear: 2000},
		{Song: "B", Artist: "X", Chapter: "College Roommates", LifeYear: 2000},
		{Song: "C", Artist: "X", Chapter: "Marriage & Adulthood", LifeYear: 2000},
	}
	ch := groupByChapter(recs, &notes.Context{})
	got := []string{ch[0].Name, ch[1].Name, ch[2].Name}
	want := []string{"College Roommates", "Pop-Punk Era", "Marriage & Adulthood"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPostCollegeRanksAfterCollege(t *testing.T) {
	recs := []enrich.Record{
		{Song: "A", Artist: "X", Chapter: "College to Post-College Transition", LifeYear: 1994},
		{Song: "B", Artist: "X", Chapter: "College / Dorm Life", LifeYear: 1994},
	}
	ch := groupByChapter(recs, &notes.Context{})
	if ch[0].Name != "College / Dorm Life" {
		t.Fatalf("college should precede the post-college transition: %v %v", ch[0].Name, ch[1].Name)
	}
}

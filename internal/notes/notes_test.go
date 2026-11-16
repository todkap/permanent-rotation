package notes

import "testing"

func TestStyleOK(t *testing.T) {
	bad := []string{
		"As you noted, its massive hooks made it stand out.",
		"During your time living in Boston, the song resonated.",
		"Introduced to you by your junior year college roommate.",
		"Released in 1983... wait, no. Released in 1993.",
		"It was actually, surprisingly, a hit.",
		"YOU might remember this one.",
	}
	for _, n := range bad {
		if StyleOK(n) {
			t.Fatalf("should reject: %q", n)
		}
	}
	good := []string{
		"For this listener, the track evokes dorm life.",
		"The song's youth-oriented marketing worked.", // "youth" is not "you"
		"Released in 1993, it bridged synth-pop and alternative rock.",
	}
	for _, n := range good {
		if !StyleOK(n) {
			t.Fatalf("should accept: %q", n)
		}
	}
}

func TestClaimsOK(t *testing.T) {
	facts := `- MusicBrainz first release: "Born on a Pirate Ship" (1996)
- Wikipedia article "The Old Apartment" intro: "The Old Apartment" is a song by Canadian group Barenaked Ladies, from the album Born on a Pirate Ship. It reached number five in Canada.
- The listener places this song around 1997 in their own life.
The Old Apartment
Barenaked Ladies`
	bad := []string{
		"Released as a single from Barenaked Ladies' album Born on a Gerrard Street, it was a hit.",   // invented album
		"Released in 1994 on the album Born on a Pirate Ship, it was a hit.",                          // year not in facts
		"From the album 'Born on a Dunedin Street', the track became a radio staple.",                 // invented album, single quotes
		"The \u201cBorn on a Pirate Ship\u201d follow-up \u201cOne Week\u201d topped charts in 1999.", // ungrounded second title + year
	}
	for _, n := range bad {
		if ClaimsOK(n, facts) {
			t.Fatalf("should reject: %q", n)
		}
	}
	good := []string{
		"Released in 1996 as a single from Barenaked Ladies' album Born on a Pirate Ship, \"The Old Apartment\" reached number five in Canada.",
		"The listener's post-college years in Waltham were soundtracked by this one.", // possessive apostrophes are not quotes
		"Released in 1996, it evokes the late nineties for this listener, who places it around 1997.",
	}
	for _, n := range good {
		if !ClaimsOK(n, facts) {
			t.Fatalf("should accept: %q", n)
		}
	}
}

func TestMigrateTitleKeys(t *testing.T) {
	n := map[string]Note{"freshman|verve pipe": {Note: "keep", SourceURL: "https://example.com"}}
	Migrate(n, "Freshman", "Verve Pipe", "The Freshmen")
	if _, ok := n["freshman|verve pipe"]; ok {
		t.Fatal("old key left behind")
	}
	if n["the freshmen|verve pipe"].Note != "keep" {
		t.Fatal(n)
	}
}

func TestStyleOKRejectsSecondPerson(t *testing.T) {
	if StyleOK("You heard it on MTV.") || StyleOK("your dorm") {
		t.Fatal("second person passed")
	}
	if !StyleOK("The listener heard it on MTV.") {
		t.Fatal("third person failed")
	}
}

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/todkap/permanent-rotation/internal/csvio"
)

func TestCleanAlbum(t *testing.T) {
	cases := map[string]string{
		"Born on a Pirate Ship (Enhanced Edition)":            "Born on a Pirate Ship",
		"The Joshua Tree (Super Deluxe)":                      "The Joshua Tree",
		"Dirt (2022 Remaster)":                                "Dirt",
		"Whatever And Ever Amen (Remastered Edition)":         "Whatever And Ever Amen",
		"Siamese Dream (Deluxe Edition)":                      "Siamese Dream",
		"Transatlanticism (10th Anniversary Edition)":         "Transatlanticism",
		"Nevermind (Remastered)":                              "Nevermind",
		"Hybrid Theory (Bonus Edition)":                       "Hybrid Theory",
		"Violator (Deluxe)":                                   "Violator",
		"The Great Adventures Of Slick Rick (Deluxe Edition)": "The Great Adventures Of Slick Rick",
		"Misguided Roses":                                     "Misguided Roses",
		"11 Minutes (with Halsey feat. Travis Barker)":        "11 Minutes (with Halsey feat. Travis Barker)",
		"Speakerboxxx/The Love Below":                         "Speakerboxxx/The Love Below",
	}
	for in, want := range cases {
		if got := cleanAlbum(in); got != want {
			t.Errorf("cleanAlbum(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalTitleMigrationPreservesPinnedData(t *testing.T) {
	songs, err := csvio.Load("../../examples/todd/songs.csv", 1)
	if err != nil {
		t.Fatal(err)
	}
	canonicalTitles(songs)
	want := map[string]string{
		"nirvana":         "Smells Like Teen Spirit",
		"the smiths":      "There Is a Light That Never Goes Out",
		"alice in chains": "Would?",
		"verve pipe":      "The Freshmen",
	}
	for _, s := range songs {
		if expected, ok := want[strings.ToLower(s.Artist)]; ok && s.Song != expected {
			t.Fatalf("%s: %q, want %q", s.Artist, s.Song, expected)
		}
	}
}

func TestPromptWarnsAboutSecondPersonTitles(t *testing.T) {
	prompt := fmt.Sprintf(notesPrompt, "With or Without You", "U2", "tag", "", "facts", "")
	if !strings.Contains(prompt, `NEVER write "you", "your", "yours"`) || !strings.Contains(prompt, "including inside song titles") {
		t.Fatal("prompt does not constrain second-person titles")
	}
}

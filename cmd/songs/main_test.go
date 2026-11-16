package main

import "testing"

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

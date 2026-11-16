package spotify

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(body string) *Client {
	return &Client{token: "test", tokenExp: time.Now().Add(time.Hour), hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}
func TestSearchAlbumArt(t *testing.T) {
	c := fixtureClient(`{"tracks":{"items":[{"id":"pinned","name":"A","artists":[{"name":"B"}],"album":{"name":"Album","images":[{"url":"large"},{"url":"medium"},{"url":"small"}]}}]}}`)
	got, err := c.Search("A", "B")
	if err != nil || len(got) != 1 || got[0].AlbumArt != "medium" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCoverArtByPinnedID(t *testing.T) {
	c := fixtureClient(`{"album":{"images":[{"url":"large"},{"url":"medium"}]}}`)
	art, err := c.CoverArt("pinned")
	if err != nil || art != "medium" {
		t.Fatalf("%q %v", art, err)
	}
	c = fixtureClient(`{"album":{"images":[]}}`)
	art, err = c.CoverArt("no-art")
	if err != nil || art != "" {
		t.Fatalf("%q %v", art, err)
	}
}

func TestPickBestPrefersOriginalAlbumOverCompilation(t *testing.T) {
	// Zombie: Spotify's top hit is the 2008 Gold hits package; the original
	// is No Need To Argue (1994). Both are exact title+artist matches.
	tracks := []Track{
		{ID: "gold", Name: "Zombie", Artist: "The Cranberries", Album: "Gold", AlbumType: "compilation", IsVarious: true, ReleaseDate: "2008-03-31"},
		{ID: "nnta", Name: "Zombie", Artist: "The Cranberries", Album: "No Need To Argue", AlbumType: "album", ReleaseDate: "1994-10-03"},
		{ID: "sessions", Name: "Zombie", Artist: "The Cranberries", Album: "No Need To Argue (The Complete Sessions 1994-1995)", AlbumType: "album", ReleaseDate: "1994-10-03"},
	}
	best, exact := PickBest(tracks, "Zombie", "The Cranberries")
	if !exact || best.ID != "nnta" {
		t.Fatalf("want nnta (1994), got %+v exact=%v", best, exact)
	}
}

func TestPickBestPrefersStudioOverGreatestHits(t *testing.T) {
	// Beastie Boys: The Sounds of Science anthology (1999) vs Check Your
	// Head (1992).
	tracks := []Track{
		{ID: "anth", Name: "So What'Cha Want", Artist: "Beastie Boys", Album: "The Sounds of Science (Anthology)", AlbumType: "compilation", IsVarious: true, ReleaseDate: "1999"},
		{ID: "cyc", Name: "So What'Cha Want", Artist: "Beastie Boys", Album: "Check Your Head", AlbumType: "album", ReleaseDate: "1992-01-21"},
	}
	best, exact := PickBest(tracks, "So What'Cha Want", "Beastie Boys")
	if !exact || best.ID != "cyc" {
		t.Fatalf("want cyc (1992), got %+v exact=%v", best, exact)
	}
}

func TestPickBestEarliestYearEvenWhenTopHitIsLater(t *testing.T) {
	// Footloose: a greatest-hits package outranks the 1984 soundtrack.
	tracks := []Track{
		{ID: "gh", Name: "Footloose", Artist: "Kenny Loggins", Album: "Yesterday, Today, Tomorrow - The Greatest Hits", AlbumType: "compilation", IsVarious: true, ReleaseDate: "1997"},
		{ID: "ost", Name: "Footloose", Artist: "Kenny Loggins", Album: "Footloose (Original Motion Picture Soundtrack)", AlbumType: "album", ReleaseDate: "1984"},
	}
	best, exact := PickBest(tracks, "Footloose", "Kenny Loggins")
	if !exact || best.ID != "ost" {
		t.Fatalf("want ost (1984), got %+v exact=%v", best, exact)
	}
}

func TestPickBestEarliestWinsEvenWhenBothCompilations(t *testing.T) {
	// Crazy for You: Celebration (2009) vs the Vision Quest soundtrack
	// (1985). Both read as compilations; earliest original release wins.
	tracks := []Track{
		{ID: "cel", Name: "Crazy for You", Artist: "Madonna", Album: "Celebration (Deluxe Version)", AlbumType: "compilation", IsVarious: true, ReleaseDate: "2009"},
		{ID: "vq", Name: "Crazy for You", Artist: "Madonna", Album: "Vision Quest", AlbumType: "compilation", IsVarious: true, ReleaseDate: "1985"},
	}
	best, exact := PickBest(tracks, "Crazy for You", "Madonna")
	if !exact || best.ID != "vq" {
		t.Fatalf("want vq (1985), got %+v exact=%v", best, exact)
	}
}

func TestPickBestSameYearTiePrefersPlainAlbum(t *testing.T) {
	// Same album, same year: the deluxe reissue loses the tie to the plain
	// original, so cards stop reading "Violator (Deluxe)".
	tracks := []Track{
		{ID: "deluxe", Name: "Enjoy the Silence", Artist: "Depeche Mode", Album: "Violator (Deluxe)", AlbumType: "album", ReleaseDate: "1990"},
		{ID: "plain", Name: "Enjoy the Silence", Artist: "Depeche Mode", Album: "Violator", AlbumType: "album", ReleaseDate: "1990"},
	}
	best, exact := PickBest(tracks, "Enjoy the Silence", "Depeche Mode")
	if !exact || best.ID != "plain" {
		t.Fatalf("want plain, got %+v exact=%v", best, exact)
	}
}

func TestPickBestOnlyCompilationAvailable(t *testing.T) {
	// Never invent a downgrade: when the compilation is the only exact
	// match, keep it rather than falling back to a fuzzy one.
	tracks := []Track{
		{ID: "gold", Name: "Zombie", Artist: "The Cranberries", Album: "Gold", AlbumType: "compilation", IsVarious: true, ReleaseDate: "2008"},
		{ID: "cover", Name: "Zombie", Artist: "Some Cover Band", Album: "Covers", AlbumType: "album", ReleaseDate: "1994"},
	}
	best, exact := PickBest(tracks, "Zombie", "The Cranberries")
	if !exact || best.ID != "gold" {
		t.Fatalf("want gold fallback, got %+v exact=%v", best, exact)
	}
}

func TestPickBestNoExactKeepsFirst(t *testing.T) {
	// Old fuzzy behavior: no exact title+artist match means the first
	// Spotify result stands.
	tracks := []Track{
		{ID: "radio", Name: "Hey Ya! - Radio Mix", Artist: "OutKast", Album: "Speakerboxxx/The Love Below", AlbumType: "album", ReleaseDate: "2003"},
		{ID: "other", Name: "Hey Ya!", Artist: "OutKast", Album: "Speakerboxxx/The Love Below", AlbumType: "album", ReleaseDate: "2003"},
	}
	best, exact := PickBest(tracks, "Hey Ya", "Outkast")
	if exact || best.ID != "radio" {
		t.Fatalf("want first fuzzy result, got %+v exact=%v", best, exact)
	}
}

func TestPickBestStripsFeaturingCredit(t *testing.T) {
	tracks := []Track{
		{ID: "rfsm", Name: "Running From My Shadow", Artist: "Mike Shinoda", Album: "Post Traumatic (Deluxe Remastered Version)", AlbumType: "album", ReleaseDate: "2018-05-24"},
	}
	best, exact := PickBest(tracks, "Running From My Shadow", "Mike Shinoda (feat. grandson)")
	if !exact || best.ID != "rfsm" {
		t.Fatalf("want exact via stripped credit, got %+v exact=%v", best, exact)
	}
}

func TestLooksLikeCompilation(t *testing.T) {
	yes := []string{"Gold", "Greatest Hits", "The Sounds of Science (Anthology)", "Celebration (Deluxe Version)", "Yesterday, Today, Tomorrow - The Greatest Hits", "Violator (Deluxe)", "Nevermind (Remastered)", "Singles: Original Motion Picture Soundtrack"}
	no := []string{"Dirt", "Republic", "No Need To Argue", "Check Your Head", "Vision Quest", "Villains", "Speakerboxxx/The Love Below", "The Colour and the Shape"}
	for _, a := range yes {
		if !LooksLikeCompilation(a) {
			t.Fatalf("want compilation: %q", a)
		}
	}
	for _, a := range no {
		if LooksLikeCompilation(a) {
			t.Fatalf("false positive: %q", a)
		}
	}
}

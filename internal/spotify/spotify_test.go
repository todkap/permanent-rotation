package spotify

import (
	"fmt"
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

func TestTargetedSearchQueries(t *testing.T) {
	// The compilation rescue depends on asking Spotify for the original
	// album or year directly; assert the query fields are formed right.
	var gotQ string
	c := &Client{token: "test", tokenExp: time.Now().Add(time.Hour), hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotQ = r.URL.Query().Get("q")
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"tracks":{"items":[]}}`)), Header: make(http.Header)}, nil
	})}}
	if _, err := c.SearchAlbum("Zombie", "The Cranberries", "No Need To Argue"); err != nil {
		t.Fatal(err)
	}
	if gotQ != "track:Zombie artist:The Cranberries album:No Need To Argue" {
		t.Fatalf("album query = %q", gotQ)
	}
	if _, err := c.SearchYear("Zombie", "The Cranberries", 1994); err != nil {
		t.Fatal(err)
	}
	if gotQ != "track:Zombie artist:The Cranberries year:1994" {
		t.Fatalf("year query = %q", gotQ)
	}
	if _, err := c.Search("Zombie", "The Cranberries"); err != nil {
		t.Fatal(err)
	}
	if gotQ != "track:Zombie artist:The Cranberries" {
		t.Fatalf("plain query = %q", gotQ)
	}
}

func TestSearchParsesAlbumTypeAndCompilationFlag(t *testing.T) {
	c := fixtureClient(`{"tracks":{"items":[{"id":"x","name":"A","artists":[{"name":"B"}],"album":{"name":"Gold","album_type":"compilation","release_date":"2008","images":[]}},{"id":"y","name":"A","artists":[{"name":"B"}],"album":{"name":"Studio","album_type":"album","release_date":"1994","images":[]}}]}}`)
	got, err := c.Search("A", "B")
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].AlbumType != "compilation" || !got[0].IsVarious {
		t.Fatalf("compilation not flagged: %+v", got[0])
	}
	if got[1].AlbumType != "album" || got[1].IsVarious {
		t.Fatalf("album misflagged: %+v", got[1])
	}
}

func TestPickBestMatchesRemasteredSuffix(t *testing.T) {
	// A remastered pressing of the same recording still exact-matches the
	// catalog title, so the original album can win.
	tracks := []Track{
		{ID: "rem", Name: "Zombie - 2011 Remaster", Artist: "The Cranberries", Album: "No Need To Argue", AlbumType: "album", ReleaseDate: "1994-10-03"},
	}
	best, exact := PickBest(tracks, "Zombie", "The Cranberries")
	if !exact || best.ID != "rem" {
		t.Fatalf("want remaster exact match, got %+v exact=%v", best, exact)
	}
}

func TestPickBestKeepsVersionsDistinct(t *testing.T) {
	// Live, acoustic, remix and re-recorded versions are different
	// recordings: never silently pin them as the original.
	for _, name := range []string{"Zombie - Live", "Zombie (Acoustic Version)", "Zombie (Remix)", "Zombie (Re-Recorded)"} {
		tracks := []Track{{ID: "v", Name: name, Artist: "The Cranberries", Album: "Some Live Album", AlbumType: "album", ReleaseDate: "1994"}}
		if _, exact := PickBest(tracks, "Zombie", "The Cranberries"); exact {
			t.Fatalf("%q must not exact-match the studio title", name)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := map[string]string{
		"Zombie - 2011 Remaster":            "Zombie",
		"Zombie - Remastered":               "Zombie",
		"Zombie (Remastered)":               "Zombie",
		"Zombie - Radio Edit":               "Zombie",
		"Zombie (Single Version)":           "Zombie",
		"Zombie - Mono":                     "Zombie",
		"Zombie":                            "Zombie",
		"Zombie - Live":                     "Zombie - Live",
		"Zombie (Acoustic Version)":         "Zombie (Acoustic Version)",
		"Version 2.0":                       "Version 2.0",
		"Enjoy the Silence":                 "Enjoy the Silence",
		"Enjoy the Silence - 2006 Remaster": "Enjoy the Silence",
	}
	for in, want := range cases {
		if got := normalizeTitle(in); got != want {
			t.Fatalf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStrictlyBetter(t *testing.T) {
	pin := Track{Album: "Violator (Deluxe)"}
	if !StrictlyBetter(Track{Album: "Earlier Album", ReleaseDate: "1989"}, "Violator", 1990) {
		t.Fatal("earlier year must win")
	}
	if StrictlyBetter(Track{Album: "Later Album", ReleaseDate: "1991"}, "Violator", 1990) {
		t.Fatal("later year must lose")
	}
	if !StrictlyBetter(Track{Album: "Violator", ReleaseDate: "1990"}, pin.Album, 1990) {
		t.Fatal("same year, plainer packaging must win")
	}
	if StrictlyBetter(Track{Album: "Violator (Deluxe)", ReleaseDate: "1990"}, "Violator", 1990) {
		t.Fatal("same year, worse packaging must lose")
	}
	if StrictlyBetter(Track{Album: "No Date"}, "Violator", 1990) {
		t.Fatal("unknown date must never win")
	}
}

// offsetClient answers search requests page by page.
func offsetClient(pages map[int]string, calls *[]int) *Client {
	return &Client{token: "test", tokenExp: time.Now().Add(time.Hour), hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		off := 0
		fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &off)
		*calls = append(*calls, off)
		body, ok := pages[off]
		if !ok {
			return &http.Response{StatusCode: 500, Status: "500", Body: io.NopCloser(strings.NewReader("boom")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}

func item(id, album, date string) string {
	return fmt.Sprintf(`{"id":%q,"name":"Zombie","artists":[{"name":"The Cranberries"}],"album":{"name":%q,"album_type":"album","release_date":%q,"images":[]}}`, id, album, date)
}

func TestSearchAlbumPaginatesAndDedupes(t *testing.T) {
	// The original pressing sits on page two; page one repeats one ID.
	var page1 []string
	for i := 0; i < 9; i++ {
		page1 = append(page1, item(fmt.Sprintf("c%d", i), "Gold", "2008"))
	}
	page1 = append(page1, item("dup", "Stars", "2002"))
	pages := map[int]string{
		0:  `{"tracks":{"items":[` + strings.Join(page1, ",") + `]}}`,
		10: `{"tracks":{"items":[` + item("dup", "Stars", "2002") + `,` + item("nnta", "No Need To Argue", "1994-10-03") + `]}}`,
	}
	var calls []int
	c := offsetClient(pages, &calls)
	got, err := c.SearchAlbum("Zombie", "The Cranberries", "No Need To Argue")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 11 {
		t.Fatalf("want 11 deduped tracks, got %d", len(got))
	}
	best, exact := PickBest(got, "Zombie", "The Cranberries")
	if !exact || best.ID != "nnta" {
		t.Fatalf("original on page two must win, got %+v", best)
	}
	if len(calls) != 2 {
		t.Fatalf("want 2 page requests, got %v", calls)
	}
}

func TestSearchPaginatedStopsOnShortPage(t *testing.T) {
	pages := map[int]string{
		0: `{"tracks":{"items":[` + item("a", "Album", "1994") + `]}}`,
	}
	var calls []int
	c := offsetClient(pages, &calls)
	got, err := c.SearchYear("Zombie", "The Cranberries", 1994)
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if len(calls) != 1 {
		t.Fatalf("short page must end the scan, got %v", calls)
	}
}

func TestSearchPaginatedPageErrorKeepsResults(t *testing.T) {
	var page1 []string
	for i := 0; i < 10; i++ {
		page1 = append(page1, item(fmt.Sprintf("c%d", i), "Gold", "2008"))
	}
	pages := map[int]string{0: `{"tracks":{"items":[` + strings.Join(page1, ",") + `]}}`} // offset 10 errors
	var calls []int
	c := offsetClient(pages, &calls)
	got, err := c.SearchAlbum("Zombie", "The Cranberries", "No Need To Argue")
	if err != nil || len(got) != 10 {
		t.Fatalf("page-one results must survive a page-two error: %+v %v", got, err)
	}
}

func TestNormalizeTitleSoundtrackBranding(t *testing.T) {
	// Footloose sits on the 1984 OST under a soundtrack-branded track name;
	// it is the same master and must exact-match the catalog title.
	cases := map[string]string{
		`Footloose - From "Footloose" Soundtrack`: "Footloose",
		`Footloose (From "Footloose" Soundtrack)`: "Footloose",
		"Footloose - From \u201cFootloose\u201d Soundtrack": "Footloose",
	}
	for in, want := range cases {
		if got := normalizeTitle(in); got != want {
			t.Fatalf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
	tracks := []Track{
		{ID: "ost", Name: `Footloose - From "Footloose" Soundtrack`, Artist: "Kenny Loggins", Album: "Footloose (Original Motion Picture Soundtrack)", AlbumType: "compilation", ReleaseDate: "1984-01-23"},
	}
	best, exact := PickBest(tracks, "Footloose", "Kenny Loggins")
	if !exact || best.ID != "ost" {
		t.Fatalf("OST master must exact-match, got %+v exact=%v", best, exact)
	}
}

func TestQuerySafeStripsColons(t *testing.T) {
	var gotQ string
	c := &Client{token: "test", tokenExp: time.Now().Add(time.Hour), hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotQ = r.URL.Query().Get("q")
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"tracks":{"items":[]}}`)), Header: make(http.Header)}, nil
	})}}
	if _, err := c.SearchAlbum("Title and Registration", "Death Cab for Cutie", "Transatlanticism: 10th Anniversary"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotQ, "album:Transatlanticism:") {
		t.Fatalf("colon leaked into query: %q", gotQ)
	}
	if _, err := c.SearchISRC("USEE10001993"); err != nil {
		t.Fatal(err)
	}
	if gotQ != "isrc:USEE10001993" {
		t.Fatalf("isrc query = %q", gotQ)
	}
}

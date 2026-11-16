// Package spotify wraps the parts of the Spotify Web API this tool uses:
// client-credentials auth, track search, and playlist creation. New apps
// run in Development Mode (Feb 2026 rules): Premium required on the owner
// account, one Client ID, 5 authorized users, no popularity field, search
// limit max 10.
package spotify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	tokenURL  = "https://accounts.spotify.com/api/token"
	searchURL = "https://api.spotify.com/v1/search"
)

type Client struct {
	id, secret string
	hc         *http.Client
	token      string
	tokenExp   time.Time
}

func New(id, secret string) *Client {
	return &Client{id: id, secret: secret, hc: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) authorize() error {
	if c.token != "" && time.Now().Before(c.tokenExp) {
		return nil
	}
	req, _ := http.NewRequest("POST", tokenURL, strings.NewReader("grant_type=client_credentials"))
	req.SetBasicAuth(c.id, c.secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("spotify auth: %s: %s", resp.Status, b)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return err
	}
	c.token = tok.AccessToken
	c.tokenExp = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	return nil
}

// Track is one search result, flattened.
type Track struct {
	ID          string
	URL         string
	Name        string
	Artist      string
	Album       string
	AlbumType   string // Spotify's album | single | compilation
	AlbumArt    string // ~300px cover image URL on Spotify's CDN
	ReleaseDate string
	Popularity  *int // nil for Dev Mode apps created after Feb 2026
	IsVarious   bool // compilation flag, deprioritized in matching
}

// Search pages up to 30 candidates, ten per request (Dev Mode cap).
func (c *Client) Search(title, artist string) ([]Track, error) {
	return c.searchPaginated(fmt.Sprintf("track:%s artist:%s", title, artist), 3)
}

// SearchAlbum narrows the search to one album title. Used to rescue catalog
// songs whose top hits are all compilations: MusicBrainz names the original
// album, and this asks Spotify for that album's version of the track
// directly, since the plain search may not surface it in 10 results.
func (c *Client) SearchAlbum(title, artist, album string) ([]Track, error) {
	return c.searchPaginated(fmt.Sprintf("track:%s artist:%s album:%s", title, artist, querySafe(album)), 3)
}

// SearchISRC finds every catalog instance of one recording (ISRCs identify
// the recording, not the release). The most precise rescue pool: it surfaces
// the original pressing even when album/year field filters miss it, and when
// it returns only compilations, that confirms the original is simply not in
// Spotify's catalog.
func (c *Client) SearchISRC(isrc string) ([]Track, error) {
	return c.searchPaginated("isrc:"+querySafe(isrc), 2)
}

// querySafe strips characters that are query syntax to Spotify (colons,
// quotes) from a field value and collapses whitespace.
func querySafe(s string) string {
	s = strings.NewReplacer(":", " ", "\"", " ", "\u201c", " ", "\u201d", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// SearchYear narrows the search to one release year; the fallback when the
// album-targeted rescue finds nothing (album titles differ across services).
func (c *Client) SearchYear(title, artist string, year int) ([]Track, error) {
	return c.searchPaginated(fmt.Sprintf("track:%s artist:%s year:%d", title, artist, year), 3)
}

// searchPaginated pages a query by offset (10 per page, the Dev Mode cap)
// and dedupes by track ID. Targeted album/year searches page deeper because
// the original pressing is often buried past the first page of hits. A
// failed later page returns the results already collected rather than
// losing them, and a short page ends the scan.
func (c *Client) searchPaginated(query string, pages int) ([]Track, error) {
	var all []Track
	seen := map[string]bool{}
	for p := 0; p < pages; p++ {
		tracks, err := c.search(query, p*10)
		if err != nil {
			if len(all) > 0 {
				return all, nil
			}
			return nil, err
		}
		for _, t := range tracks {
			if !seen[t.ID] {
				seen[t.ID] = true
				all = append(all, t)
			}
		}
		if len(tracks) < 10 {
			break
		}
	}
	return all, nil
}

func (c *Client) search(query string, offset int) ([]Track, error) {
	if err := c.authorize(); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("type", "track")
	q.Set("limit", "10")
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	req, _ := http.NewRequest("GET", searchURL+"?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("spotify search %q: %s: %s", query, resp.Status, b)
	}
	var out struct {
		Tracks struct {
			Items []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				URI     string `json:"uri"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name        string `json:"name"`
					AlbumType   string `json:"album_type"`
					ReleaseDate string `json:"release_date"`
					Images      []struct {
						URL    string `json:"url"`
						Width  int    `json:"width"`
						Height int    `json:"height"`
					} `json:"images"`
				} `json:"album"`
				Popularity *int `json:"popularity"`
			} `json:"items"`
		} `json:"tracks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var tracks []Track
	for _, it := range out.Tracks.Items {
		t := Track{
			ID:          it.ID,
			URL:         "https://open.spotify.com/track/" + it.ID,
			Name:        it.Name,
			Album:       it.Album.Name,
			AlbumType:   it.Album.AlbumType,
			ReleaseDate: it.Album.ReleaseDate,
			Popularity:  it.Popularity,
		}
		if len(it.Artists) > 0 {
			t.Artist = it.Artists[0].Name
		}
		t.IsVarious = strings.EqualFold(t.AlbumType, "compilation") || strings.EqualFold(t.Artist, "Various Artists")
		// Spotify returns large/medium/small; the middle size fits a card.
		if len(it.Album.Images) > 0 {
			img := it.Album.Images[0]
			if len(it.Album.Images) > 1 {
				img = it.Album.Images[1]
			}
			t.AlbumArt = img.URL
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

// CoverArt reads artwork for an existing track ID without changing its match.
func (c *Client) CoverArt(id string) (string, error) {
	if err := c.authorize(); err != nil {
		return "", err
	}
	req, err := http.NewRequest("GET", "https://api.spotify.com/v1/tracks/"+url.PathEscape(id), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("track artwork: %s", resp.Status)
	}
	var out struct {
		Album struct {
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"album"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Album.Images) > 1 {
		return out.Album.Images[1].URL, nil
	}
	if len(out.Album.Images) == 1 {
		return out.Album.Images[0].URL, nil
	}
	return "", nil
}

// compilationWords mark re-release packaging in an album title: hits
// collections, anniversary remasters, deluxe expansions. Matched on word
// boundaries, case-insensitive. "Gold" is listed because Spotify's top hit
// for many catalog songs is a hits package named Gold; the tradeoff is that
// a real studio album called Gold loses same-year ties, which the
// earliest-release ordering almost always decides first anyway.
var compilationWords = []string{
	"greatest hits", "best of", "anthology", "essential", "collection",
	"singles", "gold", "deluxe", "remaster", "remastered", "anniversary",
	"expanded", "reissue", "reissued", "re-issue",
}

// LooksLikeCompilation reports whether an album title reads like a hits
// package or expanded re-release rather than the original studio album.
// Used both to break same-year ties and to decide which cached matches are
// worth re-checking against a live search.
func LooksLikeCompilation(album string) bool {
	a := strings.ToLower(album)
	a = strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "-", " ", ":", " ", ",", " ", ".", " ").Replace(a)
	a = " " + strings.Join(strings.Fields(a), " ") + " "
	for _, w := range compilationWords {
		if strings.Contains(a, " "+w+" ") {
			return true
		}
	}
	return false
}

// PickBest chooses the track to pin from a search result set. Exact
// title+artist matches win; among those the earliest release wins, because
// Spotify's top hit for a catalog song is often a later compilation or
// remaster (e.g. "Zombie" topping the 2008 Gold package instead of No Need
// To Argue, 1994). Same-date ties prefer non-compilations, then Spotify's
// own ordering. With no exact match the first result stands, preserving the
// old fuzzy behavior. The bool reports whether an exact match was found.
func PickBest(tracks []Track, title, artist string) (Track, bool) {
	if len(tracks) == 0 {
		return Track{}, false
	}
	wantArtist := strings.ToLower(strings.TrimSpace(stripFeaturing(artist)))
	wantTitle := normalizeTitle(title)
	var exact []int
	for i, tr := range tracks {
		if alternateRecording(tr) {
			continue
		}
		if !strings.EqualFold(normalizeTitle(tr.Name), wantTitle) {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stripFeaturing(tr.Artist))) == wantArtist {
			exact = append(exact, i)
		}
	}
	if len(exact) == 0 {
		return tracks[0], false
	}
	best := exact[0]
	for _, i := range exact[1:] {
		if betterCandidate(tracks[i], tracks[best]) {
			best = i
		}
	}
	return tracks[best], true
}

// betterCandidate orders exact matches: earlier release first, then
// non-compilation over compilation.
func betterCandidate(a, b Track) bool {
	ka, kb := releaseKey(a.ReleaseDate), releaseKey(b.ReleaseDate)
	if ka[:4] != kb[:4] {
		return ka[:4] < kb[:4]
	}
	if releaseKind(a) != releaseKind(b) {
		return releaseKind(a) < releaseKind(b)
	}
	if compilationPenalty(a) != compilationPenalty(b) {
		return compilationPenalty(a) < compilationPenalty(b)
	}
	return ka < kb
}

// OSTs may be typed compilation by Spotify, but are album releases, not
// greatest-hits collections. A single is never a same-year album upgrade.
func releaseKind(t Track) int {
	if strings.EqualFold(t.AlbumType, "single") {
		return 1
	}
	return 0
}
func compilationPenalty(t Track) int {
	a := strings.ToLower(t.Album)
	// Movie titles like Singles are not evidence of a hits collection.
	soundtrack := strings.Contains(a, "soundtrack") || strings.Contains(a, "motion picture")
	for _, w := range []string{"greatest hits", "best of", "anthology", "essential", "collection", "gold"} {
		if strings.Contains(" "+a+" ", " "+w+" ") {
			return 2
		}
	}
	if LooksLikeCompilation(t.Album) && !(soundtrack && strings.HasPrefix(a, "singles")) {
		return 1
	}
	return 0
}

// releaseKey pads partial Spotify dates (YYYY, YYYY-MM) so they compare
// lexicographically; unknown dates sort last.
func releaseKey(date string) string {
	switch {
	case len(date) >= 10:
		return date[:10]
	case len(date) == 7:
		return date + "-01"
	case len(date) == 4:
		return date + "-01-01"
	}
	return "9999-99-99"
}

// titleSuffixes are recording-neutral reissue markers Spotify appends to
// track names. They are stripped before identity comparison so a remastered
// or mono pressing of the same recording still exact-matches the catalog
// title. Live, remix, acoustic, demo and re-recorded versions are NOT
// stripped: those are different recordings and must never pin as the
// original.
var titleSuffixes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*-\s*(\d{4}\s+)?remaster(ed)?\s*$`),
	regexp.MustCompile(`(?i)\s*\((\d{4}\s+)?remaster(ed)?\)\s*$`),
	regexp.MustCompile(`(?i)\s*-\s*(radio edit|single version|album version|mono version|stereo version|mono|stereo)\s*$`),
	regexp.MustCompile(`(?i)\s*\((radio edit|single version|album version|mono version|stereo version|mono|stereo)\)\s*$`),
	// Soundtrack branding on the same recording: `Footloose - From
	// "Footloose" Soundtrack` is the 1984 OST master, not a new version.
	regexp.MustCompile(`(?i)\s*-\s*From\s+["'\x{201c}\x{2018}][^"'\x{201d}\x{2019}]*["'\x{201d}\x{2019}]\s+Soundtrack\s*$`),
	regexp.MustCompile(`(?i)\s*\(From\s+["'\x{201c}\x{2018}][^"'\x{201d}\x{2019}]*["'\x{201d}\x{2019}]\s+Soundtrack\)\s*$`),
}

// normalizeTitle strips recording-neutral suffixes; if stripping would
// leave nothing, the unstripped title stands.
func normalizeTitle(title string) string {
	out := strings.TrimSpace(title)
	for {
		next := out
		for _, re := range titleSuffixes {
			next = re.ReplaceAllString(next, "")
		}
		next = strings.TrimSpace(next)
		if next == "" {
			return out
		}
		if next == out {
			return out
		}
		out = next
	}
}

// StrictlyBetter reports whether a candidate deserves to replace the cached
// pin: an earlier release year wins outright; the same year wins only with
// plainer packaging (the original pressing over a deluxe or hits reissue).
// Anything later or equally packaged keeps the pin, so a shuffled Spotify
// result order can never move a card backwards.
func StrictlyBetter(c Track, album string, year int) bool {
	return BetterPin(c, Track{Album: album, ReleaseDate: fmt.Sprint(year)})
}

// BetterPin uses the same ordering as PickBest, but never churns on an
// intra-year date or ID tie: year, album-v-single, then packaging quality.
func BetterPin(c, old Track) bool {
	cy, oy := releaseKey(c.ReleaseDate)[:4], releaseKey(old.ReleaseDate)[:4]
	if cy == "9999" {
		return false
	}
	if cy != oy {
		return cy < oy
	}
	if releaseKind(c) != releaseKind(old) {
		return releaseKind(c) < releaseKind(old)
	}
	return compilationPenalty(c) < compilationPenalty(old)
}

// Details supplies release type for older cached pins before comparing a
// single with an album. Unknown metadata is never guessed from its title.
func (c *Client) Details(id string) (Track, error) {
	if err := c.authorize(); err != nil {
		return Track{}, err
	}
	req, _ := http.NewRequest("GET", "https://api.spotify.com/v1/tracks/"+url.PathEscape(id), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return Track{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Track{}, fmt.Errorf("track details: %s", resp.Status)
	}
	var v struct {
		ID, Name string
		Album    struct {
			Name string `json:"name"`
			Type string `json:"album_type"`
			Date string `json:"release_date"`
		}
	}
	if err = json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return Track{}, err
	}
	if v.Album.Type == "" {
		return Track{}, fmt.Errorf("track details missing album_type")
	}
	return Track{ID: v.ID, Name: v.Name, Album: v.Album.Name, AlbumType: v.Album.Type, ReleaseDate: v.Album.Date}, nil
}

// stripFeaturing drops " (feat. ...)"-style suffixes so a CSV credit like
// "Mike Shinoda (feat. grandson)" still exact-matches Spotify's "Mike Shinoda".
func stripFeaturing(artist string) string {
	if i := strings.Index(strings.ToLower(artist), "(feat"); i >= 0 {
		return strings.TrimSpace(artist[:i])
	}
	return artist
}

// Some catalog uploads put the altered version in the release title rather
// than the track name. Do not treat those as an original recording.
func alternateRecording(t Track) bool {
	a := strings.ToLower(t.Album)
	for _, v := range []string{"sped up", "sped-up", "speed up", "slowed", "nightcore", "karaoke", "tribute", "re-recorded", "rerecorded", "remix", "(live", "[live", "live at ", "live in ", "(acoustic", "unplugged", "(demo"} {
		if strings.Contains(a, v) {
			return true
		}
	}
	return false
}

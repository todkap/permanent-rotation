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

// Search looks up one song and returns up to 10 candidates (Dev Mode cap).
func (c *Client) Search(title, artist string) ([]Track, error) {
	if err := c.authorize(); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("q", fmt.Sprintf("track:%s artist:%s", title, artist))
	q.Set("type", "track")
	q.Set("limit", "10")
	req, _ := http.NewRequest("GET", searchURL+"?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("spotify search %q: %s: %s", title, resp.Status, b)
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
	var exact []int
	for i, tr := range tracks {
		if !strings.EqualFold(strings.TrimSpace(tr.Name), strings.TrimSpace(title)) {
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
	if ka != kb {
		return ka < kb
	}
	return compilationPenalty(a) < compilationPenalty(b)
}

func compilationPenalty(t Track) int {
	if t.IsVarious || LooksLikeCompilation(t.Album) {
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

// stripFeaturing drops " (feat. ...)"-style suffixes so a CSV credit like
// "Mike Shinoda (feat. grandson)" still exact-matches Spotify's "Mike Shinoda".
func stripFeaturing(artist string) string {
	if i := strings.Index(strings.ToLower(artist), "(feat"); i >= 0 {
		return strings.TrimSpace(artist[:i])
	}
	return artist
}

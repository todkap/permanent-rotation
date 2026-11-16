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
			ReleaseDate: it.Album.ReleaseDate,
			Popularity:  it.Popularity,
		}
		if len(it.Artists) > 0 {
			t.Artist = it.Artists[0].Name
		}
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

// Package musicbrainz is the optional cross-check for release years.
// Remasters can make Spotify report a later year; MusicBrainz knows the
// first release. Disagreement is flagged, never silently resolved.
package musicbrainz

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiURL = "https://musicbrainz.org/ws/2/release-group/"

type Client struct {
	hc *http.Client
}

func New() *Client {
	return &Client{hc: &http.Client{Timeout: 15 * time.Second}}
}

// FirstReleaseYear returns the earliest release-group year it can find for
// the recording, or 0 when there is no confident answer. Call no more than
// once per second (MusicBrainz rate limit).
func (c *Client) FirstReleaseYear(artist, recording string) (int, error) {
	_, year, err := c.FirstRelease(artist, recording)
	return year, err
}

// FirstRelease returns the earliest release-group title and year it can find
// for the recording, or empty/0 when there is no confident answer. Call no
// more than once per second (MusicBrainz rate limit).
func (c *Client) FirstRelease(artist, recording string) (string, int, error) {
	q := url.Values{}
	q.Set("query", fmt.Sprintf(`recording:"%s" AND artist:"%s"`, recording, artist))
	q.Set("fmt", "json")
	q.Set("limit", "5")
	req, _ := http.NewRequest("GET", apiURL+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", "permanent-rotation/0.1 (personal music list tool)")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("musicbrainz %q: %s", recording, resp.Status)
	}
	var out struct {
		ReleaseGroups []struct {
			Title            string `json:"title"`
			FirstReleaseDate string `json:"first-release-date"`
		} `json:"release-groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, err
	}
	best := 0
	album := ""
	for _, rg := range out.ReleaseGroups {
		if len(rg.FirstReleaseDate) < 4 {
			continue
		}
		var y int
		fmt.Sscanf(rg.FirstReleaseDate[:4], "%d", &y)
		if y > 0 && (best == 0 || y < best) {
			best = y
			album = rg.Title
		}
	}
	return album, best, nil
}

const recURL = "https://musicbrainz.org/ws/2/recording/"

// RecordingISRC returns one ISRC for the recording (the exact-match titled
// recording when possible). ISRCs identify the recording itself, so Spotify
// can find every catalog instance of it regardless of packaging. Returns ""
// when MusicBrainz has no usable answer. Same 1-call-per-second courtesy.
func (c *Client) RecordingISRC(artist, recording string) (string, error) {
	q := url.Values{}
	q.Set("query", fmt.Sprintf(`recording:"%s" AND artist:"%s"`, recording, artist))
	q.Set("fmt", "json")
	q.Set("limit", "5")
	q.Set("inc", "isrcs")
	req, _ := http.NewRequest("GET", recURL+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", "permanent-rotation/0.1 (personal music list tool)")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("musicbrainz recording %q: %s", recording, resp.Status)
	}
	var out struct {
		Recordings []struct {
			Title string   `json:"title"`
			ISRCs []string `json:"isrcs"`
		} `json:"recordings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	fallback := ""
	for _, r := range out.Recordings {
		if len(r.ISRCs) == 0 {
			continue
		}
		if fallback == "" {
			fallback = r.ISRCs[0]
		}
		if strings.EqualFold(strings.TrimSpace(r.Title), strings.TrimSpace(recording)) {
			return r.ISRCs[0], nil
		}
	}
	return fallback, nil
}

// Package musicbrainz is the optional cross-check for release years.
// Remasters can make Spotify report a later year; MusicBrainz knows the
// first release. Disagreement is flagged, never silently resolved.
package musicbrainz

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	q := url.Values{}
	q.Set("query", fmt.Sprintf(`recording:"%s" AND artist:"%s"`, recording, artist))
	q.Set("fmt", "json")
	q.Set("limit", "5")
	req, _ := http.NewRequest("GET", apiURL+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", "permanent-rotation/0.1 (personal music list tool)")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("musicbrainz %q: %s", recording, resp.Status)
	}
	var out struct {
		ReleaseGroups []struct {
			FirstReleaseDate string `json:"first-release-date"`
		} `json:"release-groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	best := 0
	for _, rg := range out.ReleaseGroups {
		if len(rg.FirstReleaseDate) < 4 {
			continue
		}
		var y int
		fmt.Sscanf(rg.FirstReleaseDate[:4], "%d", &y)
		if y > 0 && (best == 0 || y < best) {
			best = y
		}
	}
	return best, nil
}

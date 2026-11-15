// Package lastfm reads all-time listener and play counts, which drive the
// canon vs deep-cut split. Spotify removed the popularity field for new
// Dev Mode apps in Feb 2026, so Last.fm is the metric path.
package lastfm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const apiURL = "https://ws.audioscrobbler.com/2.0/"

type Client struct {
	key string
	hc  *http.Client
}

func New(key string) *Client {
	// free tier: stay well under the rate limit, one call at a time
	return &Client{key: key, hc: &http.Client{Timeout: 15 * time.Second}}
}

// TrackInfo returns all-time listeners and playcount for a track.
// A track Last.fm does not know returns zeros with no error.
func (c *Client) TrackInfo(artist, track string) (listeners, playcount int64, err error) {
	q := url.Values{}
	q.Set("method", "track.getInfo")
	q.Set("api_key", c.key)
	q.Set("artist", artist)
	q.Set("track", track)
	q.Set("format", "json")
	resp, err := c.hc.Get(apiURL + "?" + q.Encode())
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, 0, fmt.Errorf("lastfm %q: %s: %s", track, resp.Status, b)
	}
	var out struct {
		Track struct {
			Listeners string `json:"listeners"`
			Playcount string `json:"playcount"`
		} `json:"track"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0, err
	}
	listeners, _ = strconv.ParseInt(out.Track.Listeners, 10, 64)
	playcount, _ = strconv.ParseInt(out.Track.Playcount, 10, 64)
	return listeners, playcount, nil
}

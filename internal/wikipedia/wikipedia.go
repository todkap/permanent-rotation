// Package wikipedia fetches the intro excerpt of a song's Wikipedia article
// so the notes step can ground the model in sourced facts (chart peaks,
// release context) instead of letting it rely on its own memory.
package wikipedia

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiURL = "https://en.wikipedia.org/w/api.php"

// MaxExtract caps the intro excerpt sent to the model, in characters.
const MaxExtract = 1500

type Client struct {
	hc     *http.Client
	apiURL string // overridable for tests
}

func New() *Client {
	return &Client{hc: &http.Client{Timeout: 15 * time.Second}, apiURL: apiURL}
}

// Lookup finds the best Wikipedia article for the song and returns its
// canonical title, URL, and plain-text intro excerpt. err is nil with empty
// values when there is no good match.
func (c *Client) Lookup(song, artist string) (title, pageURL, extract string, err error) {
	title, err = c.search(song, artist)
	if err != nil || title == "" {
		return "", "", "", err
	}
	extract, err = c.intro(title)
	if err != nil {
		return "", "", "", err
	}
	extract = strings.Join(strings.Fields(extract), " ") // collapse newlines/whitespace
	if len(extract) > MaxExtract {
		extract = extract[:MaxExtract]
	}
	return title, "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(title, " ", "_"), extract, nil
}

func (c *Client) search(song, artist string) (string, error) {
	q := url.Values{}
	q.Set("action", "query")
	q.Set("list", "search")
	q.Set("srsearch", fmt.Sprintf(`"%s" %s song`, song, artist))
	q.Set("srlimit", "3")
	q.Set("format", "json")
	body, err := c.get(q)
	if err != nil {
		return "", err
	}
	var out struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Query.Search) == 0 {
		return "", nil
	}
	return out.Query.Search[0].Title, nil
}

func (c *Client) intro(title string) (string, error) {
	q := url.Values{}
	q.Set("action", "query")
	q.Set("prop", "extracts")
	q.Set("explaintext", "1")
	q.Set("exintro", "1")
	q.Set("redirects", "1")
	q.Set("titles", title)
	q.Set("format", "json")
	body, err := c.get(q)
	if err != nil {
		return "", err
	}
	var out struct {
		Query struct {
			Pages map[string]struct {
				Extract string `json:"extract"`
				Missing bool   `json:"missing"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	for _, p := range out.Query.Pages {
		if !p.Missing {
			return p.Extract, nil
		}
	}
	return "", nil
}

func (c *Client) get(q url.Values) ([]byte, error) {
	req, _ := http.NewRequest("GET", c.apiURL+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", "permanent-rotation/0.1 (personal music list tool)")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("wikipedia: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

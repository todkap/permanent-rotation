// Package wikipedia resolves source articles before notes or release searches use them.
package wikipedia

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const apiURL = "https://en.wikipedia.org/w/api.php"
const MaxExtract = 1500

type Client struct {
	hc     *http.Client
	apiURL string
}

func New() *Client { return &Client{hc: &http.Client{Timeout: 15 * time.Second}, apiURL: apiURL} }
func (c *Client) Lookup(song, artist string) (string, string, string, error) {
	t, u, e, _, err := c.LookupRelease(song, artist, "")
	return t, u, e, err
}

// LookupRelease checks several song results, then the selected album. The
// returned URL and title belong to the resolved page, never the input alias.
// Disambiguation pages cannot supply facts or a note source. The optional
// album hint is read from the song infobox; it does not override identity.
func (c *Client) LookupRelease(song, artist, album string) (title, pageURL, extract, originalAlbum string, err error) {
	queries := []string{fmt.Sprintf(`"%s" %s song`, song, artist)}
	if album != "" {
		queries = append(queries, fmt.Sprintf(`"%s" %s album`, album, artist))
	}
	seen := map[string]bool{}
	var lastErr error
	for _, query := range queries {
		titles, e := c.searchTitles(query)
		if e != nil {
			lastErr = e
			continue
		}
		for _, t := range titles {
			if seen[t] {
				continue
			}
			seen[t] = true
			page, e := c.page(t)
			if e != nil {
				lastErr = e
				continue
			}
			if page.disambiguation || page.extract == "" {
				continue
			}
			// Search rank alone is not a binding to the requested performer.
			if !strings.Contains(strings.ToLower(page.extract), strings.ToLower(strings.TrimSpace(stripFeat(artist)))) {
				continue
			}
			identity := strings.ToLower(page.title + " " + page.extract)
			songOK := strings.Contains(identity, strings.ToLower(song))
			albumOK := album != "" && strings.Contains(strings.ToLower(page.title), strings.ToLower(album))
			if !songOK && !albumOK {
				continue
			}
			text := strings.Join(strings.Fields(page.extract), " ")
			if len(text) > MaxExtract {
				text = text[:MaxExtract]
			}
			return page.title, page.url, text, albumHint(page.wikitext), nil
		}
	}
	if lastErr != nil {
		return "", "", "", "", lastErr
	}
	return "", "", "", "", fmt.Errorf("no verified non-disambiguation article for %s / %s", song, artist)
}
func stripFeat(s string) string {
	if i := strings.Index(strings.ToLower(s), "(feat"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
func (c *Client) searchTitles(query string) ([]string, error) {
	b, err := c.get(url.Values{"action": {"query"}, "list": {"search"}, "srsearch": {query}, "srlimit": {"3"}, "format": {"json"}})
	if err != nil {
		return nil, err
	}
	var x struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	if err = json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	var out []string
	for _, r := range x.Query.Search {
		out = append(out, r.Title)
	}
	return out, nil
}

type article struct {
	title, url, extract, wikitext string
	disambiguation                bool
}

func (c *Client) page(title string) (article, error) {
	q := url.Values{"action": {"query"}, "prop": {"extracts|pageprops|info|revisions"}, "explaintext": {"1"}, "exintro": {"1"}, "redirects": {"1"}, "inprop": {"url"}, "rvprop": {"content"}, "rvslots": {"main"}, "titles": {title}, "format": {"json"}}
	b, err := c.get(q)
	if err != nil {
		return article{}, err
	}
	var x struct {
		Query struct {
			Pages map[string]struct {
				Title, FullURL, Extract string
				Missing                 bool
				PageProps               map[string]json.RawMessage `json:"pageprops"`
				Revisions               []struct {
					Slots struct {
						Main struct {
							Content string `json:"*"`
						} `json:"main"`
					} `json:"slots"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err = json.Unmarshal(b, &x); err != nil {
		return article{}, err
	}
	for _, p := range x.Query.Pages {
		if !p.Missing {
			t := p.Title
			if t == "" {
				t = title
			}
			u := p.FullURL
			if u == "" {
				u = "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(t, " ", "_")
			}
			_, disamb := p.PageProps["disambiguation"]
			low := strings.ToLower(p.Extract)
			disamb = disamb || strings.Contains(low, "may refer to:") || strings.Contains(low, "may also refer to:")
			w := ""
			if len(p.Revisions) > 0 {
				w = p.Revisions[0].Slots.Main.Content
			}
			return article{t, u, p.Extract, w, disamb}, nil
		}
	}
	return article{}, fmt.Errorf("missing article %s", title)
}

var albumField = regexp.MustCompile(`(?mi)^\|\s*album\s*=\s*(.+)$`)
var wikiLink = regexp.MustCompile(`\[\[([^]\n]+)\]\]`)

func albumHint(w string) string {
	m := albumField.FindStringSubmatch(w)
	if m == nil {
		return ""
	}
	l := wikiLink.FindStringSubmatch(m[1])
	if l == nil {
		return ""
	}
	// Target title, not a display alias. Spotify may ignore the disambiguator.
	name := strings.Split(l[1], "|")[0]
	return regexp.MustCompile(`\s*\([^)]*\)$`).ReplaceAllString(name, "")
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

// SourceUsable validates an existing Wikipedia note URL without following
// instructions in its content. Non-Wikipedia links are outside this check.
func (c *Client) SourceUsable(raw string) (bool, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return false, err
	}
	if u.Host != "en.wikipedia.org" || !strings.HasPrefix(u.Path, "/wiki/") {
		return false, fmt.Errorf("source is not a resolved Wikipedia article")
	}
	title, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/wiki/"))
	if err != nil {
		return false, err
	}
	page, err := c.page(strings.ReplaceAll(title, "_", " "))
	if err != nil {
		return false, err
	}
	return !page.disambiguation && page.extract != "", nil
}

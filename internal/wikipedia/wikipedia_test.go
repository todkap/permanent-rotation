package wikipedia

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch q.Get("list") {
		case "search":
			if !strings.Contains(q.Get("srsearch"), "The Old Apartment") {
				t.Errorf("unexpected search query: %s", q.Get("srsearch"))
			}
			w.Write([]byte(`{"query":{"search":[{"title":"The Old Apartment"}]}}`))
		default:
			if !strings.Contains(q.Get("prop"), "extracts") {
				t.Errorf("unexpected request: %s", r.URL)
			}
			w.Write([]byte(`{"query":{"pages":{"123":{"extract":"\"The Old Apartment\" is a song by the Canadian alternative rock group Barenaked Ladies.\n\nIt was the band's first entry on the US Billboard Hot 100."}}}}`))
		}
	}))
	defer srv.Close()

	c := New()
	c.apiURL = srv.URL
	title, pageURL, extract, err := c.Lookup("The Old Apartment", "Barenaked Ladies")
	if err != nil {
		t.Fatal(err)
	}
	if title != "The Old Apartment" {
		t.Fatalf("title: %q", title)
	}
	if pageURL != "https://en.wikipedia.org/wiki/The_Old_Apartment" {
		t.Fatalf("url: %q", pageURL)
	}
	if !strings.Contains(extract, "Barenaked Ladies") || strings.Contains(extract, "\n") {
		t.Fatalf("extract not collapsed: %q", extract)
	}
}

func TestLookupNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"query":{"search":[]}}`))
	}))
	defer srv.Close()

	c := New()
	c.apiURL = srv.URL
	title, pageURL, extract, err := c.Lookup("Nonexistent Song XYZ", "Nobody")
	if err == nil {
		t.Fatal("missing article should report source failure")
	}
	if title != "" || pageURL != "" || extract != "" {
		t.Fatalf("expected empty result, got %q %q %q", title, pageURL, extract)
	}
}

func TestV5RejectDisambiguationAndUseCanonicalAlbum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if q.Get("list") == "search" {
			if strings.Contains(q.Get("srsearch"), `"Tim"`) {
				w.Write([]byte(`{"query":{"search":[{"title":"Tim (album)"},{"title":"Tim (The Replacements album)"}]}}`))
			} else {
				w.Write([]byte(`{"query":{"search":[{"title":"Tim (album)"}]}}`))
			}
			return
		}
		if q.Get("titles") == "Tim (album)" {
			w.Write([]byte(`{"query":{"pages":{"1":{"title":"Tim (album)","extract":"Tim may refer to:","pageprops":{"disambiguation":""}}}}}`))
			return
		}
		w.Write([]byte(`{"query":{"pages":{"2":{"title":"Tim (The Replacements album)","fullurl":"https://en.wikipedia.org/wiki/Tim_(The_Replacements_album)","extract":"Tim is the 1985 album by The Replacements.","pageprops":{}}}}}`))
	}))
	defer srv.Close()
	c := New()
	c.apiURL = srv.URL
	title, u, text, _, err := c.LookupRelease("Here Comes a Regular", "The Replacements", "Tim")
	if err != nil || title != "Tim (The Replacements album)" || !strings.Contains(u, "Tim_(The_Replacements_album)") || !strings.Contains(text, "1985") {
		t.Fatalf("%q %q %q %v", title, u, text, err)
	}
	ok, err := c.SourceUsable("https://en.wikipedia.org/wiki/Tim_(album)")
	if err != nil || ok {
		t.Fatalf("disambiguation accepted: %v %v", ok, err)
	}
}
func TestV5OriginalAlbumHintAndRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list") == "search" {
			w.Write([]byte(`{"query":{"search":[{"title":"Zombie (song)"}]}}`))
			return
		}
		w.Write([]byte(`{"query":{"pages":{"1":{"title":"Zombie (The Cranberries song)","fullurl":"https://en.wikipedia.org/wiki/Zombie_(The_Cranberries_song)","extract":"Zombie is a song by the Cranberries from No Need to Argue.","revisions":[{"slots":{"main":{"*":"{{Infobox song\n| album = [[No Need to Argue]]\n}}"}}}]}}}}`))
	}))
	defer srv.Close()
	c := New()
	c.apiURL = srv.URL
	title, u, _, album, err := c.LookupRelease("Zombie", "The Cranberries", "Zombie")
	if err != nil || album != "No Need to Argue" || title != "Zombie (The Cranberries song)" || !strings.Contains(u, "Cranberries_song") {
		t.Fatalf("%s %s %s %v", title, u, album, err)
	}
}
func TestV5WrongPerformerSourceRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list") == "search" {
			w.Write([]byte(`{"query":{"search":[{"title":"Zombie"}]}}`))
			return
		}
		w.Write([]byte(`{"query":{"pages":{"1":{"title":"Zombie","extract":"Zombie is a recording by a different performer."}}}}`))
	}))
	defer srv.Close()
	c := New()
	c.apiURL = srv.URL
	if _, _, _, _, err := c.LookupRelease("Zombie", "The Cranberries", ""); err == nil {
		t.Fatal("wrong performer accepted")
	}
}

func TestSourceIdentityAndEncodedQuestion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("titles") {
		case "Would?":
			fmt.Fprint(w, `{"query":{"pages":{"1":{"title":"Would?","extract":"Would? is a song by Alice in Chains.","fullurl":"https://en.wikipedia.org/wiki/Would%3F"}}}}`)
		case "Tim (album)":
			fmt.Fprint(w, `{"query":{"pages":{"1":{"title":"Tim (album)","extract":"Tim may refer to:","pageprops":{"disambiguation":""}}}}}`)
		case "Scenario (A Tribe Called Quest song)":
			fmt.Fprint(w, `{"query":{"pages":{"-1":{"title":"Scenario (A Tribe Called Quest song)","missing":""}}}}`)
		default:
			fmt.Fprint(w, `{"query":{"pages":{"1":{"title":"Would","extract":"Would is an English modal auxiliary verb."}}}}`)
		}
	}))
	defer srv.Close()
	c := &Client{hc: srv.Client(), apiURL: srv.URL}
	for _, tc := range []struct {
		url, song, artist, album string
		ok                       bool
	}{
		{"https://en.wikipedia.org/wiki/Would?", "Would?", "Alice in Chains", "", false},
		{"https://en.wikipedia.org/wiki/Would%3F", "Would?", "Alice in Chains", "", true},
		{"https://en.wikipedia.org/wiki/Would", "Would?", "Alice in Chains", "", false},
		{"https://en.wikipedia.org/wiki/Tim_(album)", "Here Comes a Regular", "The Replacements", "Tim", false},
		{"https://en.wikipedia.org/wiki/Scenario_(A_Tribe_Called_Quest_song)", "Scenario", "A Tribe Called Quest", "The Low End Theory", false},
	} {
		_, ok, err := c.CheckSource(tc.url, tc.song, tc.artist, tc.album)
		if err != nil || ok != tc.ok {
			t.Errorf("%s: ok=%v err=%v", tc.url, ok, err)
		}
	}
}
func TestPerformerPunctuationAndJointCredit(t *testing.T) {
	for _, tc := range []struct{ song, artist, text string }{
		{"Deep Cover", "Dr. Dre and Snoop Dogg", "Deep Cover is a song by Dr. Dre, featuring Snoop Doggy Dogg. Later Dr. Dre and Snoop Dogg remade it."},
		{"Nightswimming", "R.E.M.", "Nightswimming is a song by American band R.E.M."},
		{"So What'Cha Want", "Beastie Boys", "So What'cha Want is a song by the Beastie Boys."},
		{"I Write Sins Not Tragedies", "Panic! at the Disco", "I Write Sins Not Tragedies is a song by Panic! at the Disco."},
	} {
		if !belongs(article{title: tc.song, extract: tc.text}, tc.song, tc.artist, "") {
			t.Error("identity failed: " + tc.song)
		}
	}
}

func TestRetryAfterAndStopOn429(t *testing.T) {
	now := time.Now()
	for _, h := range []string{"3600", now.Add(2 * time.Hour).UTC().Format(http.TimeFormat)} {
		until := retryUntil(h, now, 1)
		if until.Sub(now) < 59*time.Minute {
			t.Fatal("ignored Retry-After", h, until)
		}
	}
	if retryUntil("", now, 2).Sub(now) <= retryUntil("", now, 1).Sub(now) {
		t.Fatal("no exponential backoff")
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := &Client{hc: srv.Client(), apiURL: srv.URL}
	_, _, _, _, err := c.LookupRelease("Song", "Artist", "Album")
	if err == nil {
		t.Fatal("429 ignored")
	}
	c.LookupRelease("Another", "Artist", "Album")
	if calls != 1 {
		t.Fatal("requests continued during cooldown", calls)
	}
	if time.Until(c.RetryNotBefore()) < 59*time.Minute {
		t.Fatal("cooldown not exposed")
	}
}
func TestRequestCacheAndBudget(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"query":{"search":[]}}`) }))
	defer srv.Close()
	c := &Client{hc: srv.Client(), apiURL: srv.URL}
	q := url.Values{"x": {"1"}}
	c.get(q)
	c.get(q)
	if calls != 1 {
		t.Fatal("same request not cached")
	}
	c.requests = 20
	if _, err := c.get(url.Values{"x": {"2"}}); err == nil {
		t.Fatal("budget exceeded")
	}
	if calls != 1 {
		t.Fatal("network after budget")
	}
}

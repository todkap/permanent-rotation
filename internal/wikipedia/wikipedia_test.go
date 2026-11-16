package wikipedia

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

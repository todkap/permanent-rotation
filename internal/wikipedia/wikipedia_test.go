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
			if q.Get("prop") != "extracts" {
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
	if err != nil {
		t.Fatal(err)
	}
	if title != "" || pageURL != "" || extract != "" {
		t.Fatalf("expected empty result, got %q %q %q", title, pageURL, extract)
	}
}

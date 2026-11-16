package main

import (
	"encoding/json"
	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/notes"
	"github.com/todkap/permanent-rotation/internal/spotify"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportAndFlushSummary(t *testing.T) {
	f := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", f)
	report("re-matched: A - B (Gold 2008 -> No Need To Argue 1994)")
	report("note: dropped (no source URL in generated note): C - D")
	flushSummary("Enrich: matching decisions")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "### Enrich: matching decisions") ||
		!strings.Contains(string(b), "- re-matched: A - B (Gold 2008 -> No Need To Argue 1994)") ||
		!strings.Contains(string(b), "- note: dropped (no source URL in generated note): C - D") {
		t.Fatalf("summary content: %q", b)
	}
	if len(reportLines) != 0 {
		t.Fatal("reportLines not reset after flush")
	}
}

func TestFlushSummaryNoEnvIsQuiet(t *testing.T) {
	report("matched: A - B -> X 1994 (exact)")
	flushSummary("Enrich: matching decisions") // no GITHUB_STEP_SUMMARY: must not fail
	if len(reportLines) != 1 {
		t.Fatal("lines should be kept when no summary file is set")
	}
	reportLines = nil
}

func TestSimilarTitles(t *testing.T) {
	yes := [][2]string{{"Would?", "Would"}, {"The Freshmen", "Freshman"}, {"There Is a Light That Never Goes Out", "There is a light that never goes out"}}
	for _, c := range yes {
		if !similarTitles(c[0], c[1]) {
			t.Fatalf("want similar: %q / %q", c[0], c[1])
		}
	}
	no := [][2]string{{"Zombie", "Linger"}, {"Would?", "Go"}, {"Basket Case", "Longview"}}
	for _, c := range no {
		if similarTitles(c[0], c[1]) {
			t.Fatalf("want distinct: %q / %q", c[0], c[1])
		}
	}
}

func TestFlushSummaryEscapesExternalText(t *testing.T) {
	f := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", f)
	report("card: A | B -> Violator [Deluxe] *1990* [kept]")
	flushSummary("Enrich: per-card final state")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "A | B") || strings.Contains(string(b), "*1990*") || strings.Contains(string(b), "[kept]") {
		t.Fatalf("unescaped external text in summary: %q", b)
	}
}

func TestV5MarkerSurvivesAndAdoptionRecordsAlbumType(t *testing.T) {
	rec := enrich.Record{Song: "Here Comes a Regular", Artist: "The Replacements"}
	copyReleaseState(&rec, enrich.Record{ReleaseChanged: true})
	if !rec.ReleaseChanged {
		t.Fatal("marker lost before notes")
	}
	adoptPin(&rec, spotify.Track{ID: "album", Album: "Tim", AlbumType: "album", ReleaseDate: "1985"}, "test")
	if !rec.ReleaseChanged || rec.AlbumType != "album" || rec.ReleaseYear != 1985 {
		t.Fatal(rec)
	}
	reportLines = nil
}

type noteTestTransport func(*http.Request) (*http.Response, error)

func (f noteTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestNotesRetryGapsAndRepairSourceBeforeSkip(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	calls := 0
	http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		if r.URL.Host == "en.wikipedia.org" {
			q := r.URL.Query()
			title := q.Get("titles")
			if q.Get("list") == "search" {
				song := "Would?"
				if strings.Contains(q.Get("srsearch"), "So What") {
					song = "So What'cha Want"
				}
				if strings.Contains(q.Get("srsearch"), "I Write") {
					song = "I Write Sins Not Tragedies"
				}
				b, _ := json.Marshal(map[string]any{"query": map[string]any{"search": []any{map[string]any{"title": song}}}})
				body = string(b)
			} else {
				artist := "Alice in Chains"
				if strings.Contains(title, "So What") {
					artist = "Beastie Boys"
				}
				if strings.Contains(title, "I Write") {
					artist = "Panic! at the Disco"
				}
				b, _ := json.Marshal(map[string]any{"query": map[string]any{"pages": map[string]any{"1": map[string]any{"title": title, "extract": title + " is a song by " + artist + ".", "fullurl": (&url.URL{Scheme: "https", Host: "en.wikipedia.org", Path: "/wiki/" + strings.ReplaceAll(title, " ", "_")}).String()}}}})
				body = string(b)
			}
		} else if r.URL.Host == "generativelanguage.googleapis.com" {
			calls++
			result, _ := json.Marshal(map[string]string{"note": "This track is a song with a lasting place in the catalog. For this listener, it remains memorable.", "source_url": "https://en.wikipedia.org/wiki/Placeholder"})
			b, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": string(result)}}}}}})
			body = string(b)
		} else {
			t.Fatalf("unexpected network host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Setenv("GEMINI_API_KEY", "mock-not-a-secret")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "songs.csv")
	ctxPath := filepath.Join(dir, "context.json")
	efPath := filepath.Join(dir, "enriched.json")
	os.WriteFile(csvPath, []byte("song,artist,era,tags,memory,angle\nWould?,Alice in Chains,College (~1992),,,\nSo What'Cha Want,Beastie Boys,College (~1992),,,\nI Write Sins Not Tragedies,Panic! at the Disco,Adulthood (~2006),,,\n"), 0600)
	ctx := &notes.Context{Notes: map[string]notes.Note{notes.Key("Would?", "Alice in Chains"): {Note: "An existing note.", SourceURL: "https://en.wikipedia.org/wiki/Would?"}}}
	if err := ctx.Save(ctxPath); err != nil {
		t.Fatal(err)
	}
	ef := &enrich.File{}
	if err := ef.Save(efPath); err != nil {
		t.Fatal(err)
	}
	runNotes(csvPath, ctxPath, efPath, 1, false, false, 0)
	got, err := notes.Load(ctxPath)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(got.Notes) != 3 {
		t.Fatalf("wanted 3 generation calls and 3 accepted sourced notes, got %d/%d", calls, len(got.Notes))
	}
	if got.Notes[notes.Key("Would?", "Alice in Chains")].SourceURL != "https://en.wikipedia.org/wiki/Would%3F" {
		t.Fatal("raw question-mark source survived")
	}
}

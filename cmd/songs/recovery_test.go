package main

import (
	"encoding/json"
	"fmt"
	"github.com/todkap/permanent-rotation/internal/csvio"
	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/musicbrainz"
	"github.com/todkap/permanent-rotation/internal/notes"
	"github.com/todkap/permanent-rotation/internal/spotify"
	"github.com/todkap/permanent-rotation/internal/wikipedia"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransientRateLimitKeepsOldNoteAndMarker(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	requests := 0
	http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	dir := t.TempDir()
	csv := filepath.Join(dir, "songs.csv")
	ctxPath := filepath.Join(dir, "context.json")
	efPath := filepath.Join(dir, "enriched.json")
	os.WriteFile(csv, []byte("song,artist,era,tags,memory,angle\nNightswimming,R.E.M.,College (~1994),,,\n"), 0600)
	old := notes.Note{Note: "Released in 1993 on the album Automatic for the People, this song is remembered.", SourceURL: "https://en.wikipedia.org/wiki/Nightswimming"}
	c := &notes.Context{Notes: map[string]notes.Note{notes.Key("Nightswimming", "R.E.M."): old}}
	c.Save(ctxPath)
	ef := &enrich.File{Songs: []enrich.Record{{Song: "Nightswimming", Artist: "R.E.M.", Album: "Automatic for the People", ReleaseYear: 1992, ReleaseChanged: true}}}
	ef.Save(efPath)
	runNotes(csv, ctxPath, efPath, 1, false, false, 0)
	got, _ := notes.Load(ctxPath)
	if got.Notes[notes.Key("Nightswimming", "R.E.M.")] != old {
		t.Fatal("transient source failure changed prior note")
	}
	reread, _ := enrich.LoadFile(efPath)
	if !reread.Songs[0].ReleaseChanged {
		t.Fatal("marker cleared on failure")
	}
	until, err := time.Parse(time.RFC3339, got.RetryNotBefore)
	if err != nil || time.Until(until) < 59*time.Minute {
		t.Fatal("Retry-After was not persisted", got.RetryNotBefore)
	}
	runNotes(csv, ctxPath, efPath, 1, false, false, 0)
	if requests != 1 {
		t.Fatal("cooldown made more requests", requests)
	}
}
func TestPersistentStampAndInvalidation(t *testing.T) {
	now := time.Now().UTC()
	n := notes.Note{SourceURL: "https://en.wikipedia.org/wiki/Example"}
	stampSource(&n, "Song", "Artist", now)
	if !sourceStampValid(n, "Song", "Artist", now.Add(time.Hour)) {
		t.Fatal("valid stamp missed")
	}
	if sourceStampValid(n, "Another", "Artist", now) || sourceStampValid(n, "Song", "Artist", now.Add(31*24*time.Hour)) {
		t.Fatal("wrong identity/expired stamp accepted")
	}
	n.SourceURL += "_changed"
	if sourceStampValid(n, "Song", "Artist", now) {
		t.Fatal("changed source accepted")
	}
}
func TestSmallerBatchResumesPriorities(t *testing.T) {
	c := &notes.Context{Notes: map[string]notes.Note{"old|artist": {PendingVerification: true}}, LastAttempt: map[string]string{}}
	if notePriority(c, nil, csvio.Song{Song: "Missing", Artist: "Artist"}) >= notePriority(c, nil, csvio.Song{Song: "Old", Artist: "Artist"}) {
		t.Fatal("gaps not first")
	}
	if !strings.Contains(notesPrompt, "omit the album name") || !strings.Contains(notesPrompt, "Do not quote lyrics") {
		t.Fatal("strict album/lyrics prompt exception missing")
	}
	if notes.StyleIssue("A Fever You Can't Sweat Out is the album.", "I Write Sins Not Tragedies", "Panic! At The Disco") == "" {
		t.Fatal("style silently loosened")
	}
	if notes.SecondPersonTerm("The album You Can't Sweat Out.", "Song", "Artist") != "You" {
		t.Fatal("diagnostic term missing")
	}
}
func TestGitRecoveryMergesSixWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	cmd := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(b))
		}
		return strings.TrimSpace(string(b))
	}
	cmd("init", "-q")
	cmd("config", "user.email", "test@example.invalid")
	cmd("config", "user.name", "Test")
	prior := &notes.Context{Notes: map[string]notes.Note{}}
	for i := 0; i < 7; i++ {
		prior.Notes[fmt.Sprint(i)] = notes.Note{Note: "Prior accepted note", SourceURL: "https://en.wikipedia.org/wiki/Example"}
	}
	path := filepath.Join(dir, "context.json")
	prior.Save(path)
	cmd("add", "context.json")
	cmd("commit", "-qm", "baseline")
	ref := cmd("rev-parse", "HEAD")
	current := &notes.Context{Notes: map[string]notes.Note{"0": {Note: "New successful note", SourceURL: "https://en.wikipedia.org/wiki/New"}}}
	current.Save(path)
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	if err := recoverNotes("context.json", ref); err != nil {
		t.Fatal(err)
	}
	got, _ := notes.Load("context.json")
	if len(got.Notes) != 7 || got.Notes["0"].Note != "New successful note" {
		t.Fatal("lost successful work")
	}
	n := 0
	for _, v := range got.Notes {
		if v.PendingVerification {
			n++
		}
	}
	if n != 6 {
		t.Fatal("recovery count", n)
	}
	delete(got.Notes, "1")
	got.Save("context.json")
	recoverNotes("context.json", ref)
	got, _ = notes.Load("context.json")
	if _, ok := got.Notes["1"]; ok {
		t.Fatal("resurrected note after recovery receipt")
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), "recovery_applied") {
		t.Fatal("receipt not saved")
	}
}

func TestSuccessfulSpotifyRecheckReplacesStaleFailure(t *testing.T) {
	for _, outcome := range []string{"confirmed", "no-exact", "no-better"} {
		rec := enrich.Record{SpotifyID: "keep-id", Album: "Keep Album", MatchReason: "kept: Spotify search failed; original remains unverified"}
		setRecheckReason(&rec, outcome)
		if strings.Contains(rec.MatchReason, "failed") || rec.SpotifyID != "keep-id" || rec.Album != "Keep Album" {
			t.Fatal("stale reason or changed pin", outcome, rec)
		}
	}
}
func TestVerifiedUnchangedNoteUsesNoNetwork(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatal("persisted verification should skip all source requests")
		return nil, nil
	})
	dir := t.TempDir()
	csv := filepath.Join(dir, "songs.csv")
	ctx := filepath.Join(dir, "context.json")
	ef := filepath.Join(dir, "enriched.json")
	os.WriteFile(csv, []byte("song,artist,era,tags,memory,angle\nSong,Artist,College (~1992),,,\n"), 0600)
	n := notes.Note{Note: "Existing verified note.", SourceURL: "https://en.wikipedia.org/wiki/Song", Album: "Album", ReleaseYear: 1991}
	stampSource(&n, "Song", "Artist", time.Now())
	c := &notes.Context{Notes: map[string]notes.Note{notes.Key("Song", "Artist"): n}}
	c.Save(ctx)
	e := &enrich.File{Songs: []enrich.Record{{Song: "Song", Artist: "Artist", Album: "Album", ReleaseYear: 1991}}}
	e.Save(ef)
	runNotes(csv, ctx, ef, 1, false, false, 0)
	got, _ := notes.Load(ctx)
	if got.Notes[notes.Key("Song", "Artist")] != n {
		t.Fatal("unchanged note modified")
	}
}

func TestFreshResolutionRateLimitKeepsEvenPositivelyBadOldSource(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	requests := 0
	http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	dir := t.TempDir()
	csv := filepath.Join(dir, "songs.csv")
	ctxPath := filepath.Join(dir, "context.json")
	efPath := filepath.Join(dir, "enriched.json")
	os.WriteFile(csv, []byte("song,artist,era,tags,memory,angle\nNightswimming,R.E.M.,College (~1994),,,\n"), 0600)
	old := notes.Note{Note: "Released in 1993 on the album Automatic for the People, this song is remembered.", SourceURL: "https://en.wikipedia.org/wiki/Would?"}
	c := &notes.Context{Notes: map[string]notes.Note{notes.Key("Nightswimming", "R.E.M."): old}}
	c.Save(ctxPath)
	ef := &enrich.File{Songs: []enrich.Record{{Song: "Nightswimming", Artist: "R.E.M.", Album: "Automatic for the People", ReleaseYear: 1992, ReleaseChanged: true}}}
	ef.Save(efPath)
	runNotes(csv, ctxPath, efPath, 1, false, false, 0)
	got, _ := notes.Load(ctxPath)
	if got.Notes[notes.Key("Nightswimming", "R.E.M.")] != old {
		t.Fatal("transient source failure changed prior note")
	}
	reread, _ := enrich.LoadFile(efPath)
	if !reread.Songs[0].ReleaseChanged {
		t.Fatal("marker cleared on failure")
	}
	until, err := time.Parse(time.RFC3339, got.RetryNotBefore)
	if err != nil || time.Until(until) < 59*time.Minute {
		t.Fatal("Retry-After was not persisted", got.RetryNotBefore)
	}
	runNotes(csv, ctxPath, efPath, 1, false, false, 0)
	if requests != 1 {
		t.Fatal("cooldown made more requests", requests)
	}
}

func TestRepeatedThrottlesBackOffAcrossRuns(t *testing.T) {
	c := &notes.Context{}
	now := time.Now().UTC()
	for i, want := range []time.Duration{15 * time.Minute, 30 * time.Minute, 60 * time.Minute, 120 * time.Minute, 120 * time.Minute} {
		recordThrottle(c, now.Add(time.Minute), now)
		got, err := time.Parse(time.RFC3339, c.RetryNotBefore)
		if err != nil || got.Before(now.Add(want-time.Second)) {
			t.Fatalf("throttle %d: %s", i, c.RetryNotBefore)
		}
	}
	recordThrottle(c, now.Add(5*time.Hour), now)
	got, _ := time.Parse(time.RFC3339, c.RetryNotBefore)
	if got.Before(now.Add(5*time.Hour - time.Second)) {
		t.Fatal("server Retry-After shortened")
	}
}
func TestUnknownSourceFailuresPreserveNotes(t *testing.T) {
	for _, kind := range []string{"network", "503", "bad-json"} {
		t.Run(kind, func(t *testing.T) {
			oldTransport := http.DefaultTransport
			defer func() { http.DefaultTransport = oldTransport }()
			http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
				if kind == "network" {
					return nil, fmt.Errorf("temporary connection failure")
				}
				code, body := 503, "unavailable"
				if kind == "bad-json" {
					code = 200
					body = "{"
				}
				return &http.Response{StatusCode: code, Status: fmt.Sprint(code), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			dir := t.TempDir()
			csv := filepath.Join(dir, "songs.csv")
			ctx := filepath.Join(dir, "context.json")
			ef := filepath.Join(dir, "enriched.json")
			os.WriteFile(csv, []byte("song,artist,era,tags,memory,angle\nSong,Artist,College (~1992),,,\n"), 0600)
			old := notes.Note{Note: "Released in 1993 on the album Album, this song is remembered.", SourceURL: "https://en.wikipedia.org/wiki/Song"}
			(&notes.Context{Notes: map[string]notes.Note{notes.Key("Song", "Artist"): old}}).Save(ctx)
			(&enrich.File{Songs: []enrich.Record{{Song: "Song", Artist: "Artist", Album: "Album", ReleaseYear: 1991, ReleaseChanged: true}}}).Save(ef)
			runNotes(csv, ctx, ef, 1, false, false, 0)
			got, _ := notes.Load(ctx)
			e, _ := enrich.LoadFile(ef)
			if got.Notes[notes.Key("Song", "Artist")] != old || !e.Songs[0].ReleaseChanged {
				t.Fatal("unknown source changed existing note or marker")
			}
		})
	}
}
func TestWorkflowRecoveryAndBatchWiring(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/build.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"ref: ${{ github.ref_name }}", "fetch-depth: 0", "if: github.repository == 'todkap/permanent-rotation'", "--batch \"$NOTES_BATCH\" --wiki-requests \"$WIKI_REQUESTS\" --pace 8", "options: ['12', '4', '50']"} {
		if !strings.Contains(s, want) {
			t.Fatal("workflow missing", want)
		}
	}
	if strings.Index(s, "notes-recover --context") > strings.Index(s, "- name: enrich") {
		t.Fatal("recovery must precede enrich")
	}
}

func TestLaterEnrichRescuesRespectSharedCooldown(t *testing.T) {
	for _, code := range []int{429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			requests := 0
			http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, `{"recordings":[]}`
				h := http.Header{}
				if r.URL.Host == "en.wikipedia.org" {
					requests++
					status = code
					body = "busy"
					h.Set("Retry-After", "3600")
					ua := r.Header.Get("User-Agent")
					if !strings.Contains(ua, "https://github.com/todkap/permanent-rotation") {
						t.Fatal("contact missing", ua)
					}
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			wk := wikipedia.New()
			sp := spotify.New("", "")
			mb := musicbrainz.New()
			for _, song := range []string{"First", "Second", "Third"} {
				rec := enrich.Record{Song: song, Artist: "Artist"}
				rescueCompilationPin(sp, mb, wk, song, "Artist", "", 0, &rec)
			}
			if requests != 1 {
				t.Fatal("later rescue contacted throttled source", requests)
			}
		})
	}
}
func TestEnrichWiresOneWikipediaClientForEveryRescue(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "func runEnrich(")
	end := strings.Index(s[start:], "func similarTitles(")
	enrich := s[start : start+end]
	if strings.Count(enrich, "wk := wikipedia.NewWithBudget(wikiRequests)") != 1 || strings.Count(enrich, "rescueCompilationPin(sp, mb, wk,") != 2 {
		t.Fatal("enrich rescues do not share one client")
	}
	helper := s[strings.Index(s, "func rescueCompilationPin("):start]
	if strings.Contains(helper, "wikipedia.New()") {
		t.Fatal("rescue resets source state")
	}
}

func TestConfigurableLimits(t *testing.T) {
	for _, pair := range [][2]int{{4, 20}, {12, 60}, {50, 120}} {
		if err := validateLimits(pair[0], pair[1]); err != nil {
			t.Fatal(pair, err)
		}
	}
	for _, pair := range [][2]int{{0, 20}, {51, 60}, {12, 0}, {12, 121}} {
		if validateLimits(pair[0], pair[1]) == nil {
			t.Fatal("accepted invalid limits", pair)
		}
	}
}
func TestBudgetStopsNotesWithoutAttemptingRemaining(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	oldBatch, oldBudget := notesBatch, wikiRequests
	notesBatch, wikiRequests = 50, 1
	defer func() { notesBatch, wikiRequests = oldBatch, oldBudget }()
	calls := 0
	http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"query":{"search":[{"title":"Song0"}]}}`)), Header: http.Header{}, Request: r}, nil
	})
	dir := t.TempDir()
	csv := filepath.Join(dir, "songs.csv")
	ctx := filepath.Join(dir, "context.json")
	ef := filepath.Join(dir, "enriched.json")
	text := "song,artist,era,tags,memory,angle\n"
	c := &notes.Context{Notes: map[string]notes.Note{}}
	e := &enrich.File{}
	for i := 0; i < 3; i++ {
		song := fmt.Sprint("Song", i)
		text += song + ",Artist,College (~1992),,,\n"
		c.Notes[notes.Key(song, "Artist")] = notes.Note{Note: "Existing note.", SourceURL: "https://en.wikipedia.org/wiki/Bad?", PendingVerification: true}
		e.Songs = append(e.Songs, enrich.Record{Song: song, Artist: "Artist", ReleaseChanged: true})
	}
	os.WriteFile(csv, []byte(text), 0600)
	c.Save(ctx)
	e.Save(ef)
	runNotes(csv, ctx, ef, 1, false, false, 0)
	got, _ := notes.Load(ctx)
	gotE, _ := enrich.LoadFile(ef)
	if calls != 1 || len(got.LastAttempt) != 1 {
		t.Fatal("budget ran beyond first partial song", calls, got.LastAttempt)
	}
	for k, n := range c.Notes {
		if got.Notes[k] != n {
			t.Fatal("budget changed prior text", k)
		}
	}
	for _, r := range gotE.Songs {
		if !r.ReleaseChanged {
			t.Fatal("budget cleared marker")
		}
	}
}
func TestFiftySongPassSkipsPersistedSources(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatal("fresh persisted source caused network")
		return nil, nil
	})
	oldBatch, oldBudget := notesBatch, wikiRequests
	notesBatch, wikiRequests = 50, 120
	defer func() { notesBatch, wikiRequests = oldBatch, oldBudget }()
	dir := t.TempDir()
	csv := filepath.Join(dir, "songs.csv")
	ctx := filepath.Join(dir, "context.json")
	ef := filepath.Join(dir, "enriched.json")
	text := "song,artist,era,tags,memory,angle\n"
	c := &notes.Context{Notes: map[string]notes.Note{}}
	e := &enrich.File{}
	for i := 0; i < 50; i++ {
		song := fmt.Sprint("Song", i)
		text += song + ",Artist,College (~1992),,,\n"
		n := notes.Note{Note: "Verified note.", SourceURL: "https://en.wikipedia.org/wiki/" + song, Album: "Album", ReleaseYear: 1991}
		stampSource(&n, song, "Artist", time.Now())
		c.Notes[notes.Key(song, "Artist")] = n
		e.Songs = append(e.Songs, enrich.Record{Song: song, Artist: "Artist", Album: "Album", ReleaseYear: 1991})
	}
	os.WriteFile(csv, []byte(text), 0600)
	c.Save(ctx)
	e.Save(ef)
	runNotes(csv, ctx, ef, 1, false, false, 0)
	got, _ := notes.Load(ctx)
	if len(got.Notes) != 50 || len(got.LastAttempt) != 0 {
		t.Fatal("full verified pass did not skip safely")
	}
}
func TestWorkflowConfigurableBudgets(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/build.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"default: '12'", "options: ['60', '20', '120']", "default: '60'", "timeout-minutes: 90"} {
		if !strings.Contains(s, want) {
			t.Fatal("missing workflow config", want)
		}
	}
	if strings.Count(s, "--wiki-requests \"$WIKI_REQUESTS\"") != 2 {
		t.Fatal("both commands need explicit budget")
	}
}

func TestLimitsStopBetweenSongsWithoutAttemptingTail(t *testing.T) {
	for _, code := range []int{200, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			oldTransport := http.DefaultTransport
			defer func() { http.DefaultTransport = oldTransport }()
			oldBatch, oldBudget := notesBatch, wikiRequests
			notesBatch, wikiRequests = 50, 1
			defer func() { notesBatch, wikiRequests = oldBatch, oldBudget }()
			calls := 0
			http.DefaultTransport = noteTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: code, Status: fmt.Sprint(code), Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(`{"query":{"pages":{"1":{"title":"Song0","fullurl":"https://en.wikipedia.org/wiki/Song0","extract":"Song0 is a song by Artist."}}}}`)), Request: r}, nil
			})
			dir := t.TempDir()
			csv := filepath.Join(dir, "songs.csv")
			ctx := filepath.Join(dir, "context.json")
			ef := filepath.Join(dir, "enriched.json")
			text := "song,artist,era,tags,memory,angle\n"
			c := &notes.Context{Notes: map[string]notes.Note{}}
			e := &enrich.File{}
			for i := 0; i < 3; i++ {
				song := fmt.Sprint("Song", i)
				text += song + ",Artist,College (~1992),,,\n"
				c.Notes[notes.Key(song, "Artist")] = notes.Note{Note: "Existing note.", SourceURL: "https://en.wikipedia.org/wiki/" + song}
				e.Songs = append(e.Songs, enrich.Record{Song: song, Artist: "Artist"})
			}
			os.WriteFile(csv, []byte(text), 0600)
			c.Save(ctx)
			e.Save(ef)
			runNotes(csv, ctx, ef, 1, false, false, 0)
			got, _ := notes.Load(ctx)
			if calls != 1 || len(got.LastAttempt) != 1 {
				t.Fatal("remaining songs counted attempted", calls, got.LastAttempt)
			}
			for k, n := range c.Notes {
				if got.Notes[k].Note != n.Note {
					t.Fatal("existing text lost", k)
				}
			}
			for _, song := range []string{"Song1", "Song2"} {
				k := notes.Key(song, "Artist")
				if got.Notes[k] != c.Notes[k] {
					t.Fatal("tail note changed", k)
				}
			}
			if code != 200 && got.RetryNotBefore == "" {
				t.Fatal("cooldown not persisted")
			}
		})
	}
}

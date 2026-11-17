package main

import (
	"encoding/json"
	"fmt"
	"github.com/todkap/permanent-rotation/internal/csvio"
	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/notes"
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
	for _, want := range []string{"ref: ${{ github.ref_name }}", "fetch-depth: 0", "if: github.repository == 'todkap/permanent-rotation'", "--batch \"$NOTES_BATCH\" --pace 8", "options: ['4', '2', '1']"} {
		if !strings.Contains(s, want) {
			t.Fatal("workflow missing", want)
		}
	}
	if strings.Index(s, "notes-recover --context") > strings.Index(s, "- name: enrich") {
		t.Fatal("recovery must precede enrich")
	}
}

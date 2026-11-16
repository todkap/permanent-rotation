// songs is the CLI behind permanent-rotation:
//
//	songs enrich   match songs.csv against Spotify + Last.fm, write enriched.json
//	songs site     render the static site from enriched.json
//	songs playlist create/update the Spotify playlist (local run, owner login)
//	songs notes    opt-in Gemini background notes layer (reviewed before commit)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/todkap/permanent-rotation/internal/csvio"
	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/gemini"
	"github.com/todkap/permanent-rotation/internal/lastfm"
	"github.com/todkap/permanent-rotation/internal/musicbrainz"
	"github.com/todkap/permanent-rotation/internal/notes"
	"github.com/todkap/permanent-rotation/internal/site"
	"github.com/todkap/permanent-rotation/internal/spotify"
	"github.com/todkap/permanent-rotation/internal/wikipedia"
)

func fail(err error) {
	fmt.Fprintln(os.Stderr, "songs:", err)
	os.Exit(1)
}

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: songs <enrich|site|playlist|notes> [flags]")
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	csvPath := fs.String("csv", "songs.csv", "input songs CSV")
	outPath := fs.String("out", "data/enriched.json", "enriched data file")
	siteDir := fs.String("site", "site", "output directory for the rendered site")
	baseURL := fs.String("base", "", "absolute URL of the published site, for og:image and link previews")
	minSongs := fs.Int("min", csvio.DefaultMinSongs, "minimum valid songs required")
	contextPath := fs.String("context", "data/context.json", "background notes file")
	includeMemory := fs.Bool("include-memory", true, "send memory text to the model (opt out with -include-memory=false)")
	paceSecs := fs.Int("pace", 5, "seconds between Gemini requests (free tier allows 15/min; lower this on a paid tier)")
	forceNotes := fs.Bool("force", false, "notes: regenerate even songs that already have a note (default fills only missing ones)")
	fs.Parse(args)

	switch cmd {
	case "enrich":
		runEnrich(*csvPath, *outPath, *minSongs)
	case "site":
		runSite(*outPath, *siteDir, *contextPath, *baseURL)
	case "playlist":
		fmt.Println("playlist is not implemented in this skeleton yet.")
		fmt.Println("Design (spec section 6): local run only, browser consent once,")
		fmt.Println("redirect http://127.0.0.1:8888/callback, then POST /me/playlists.")
		fmt.Println("Order follows life chapters, no ranking.")
		os.Exit(1)
	case "notes":
		runNotes(*csvPath, *contextPath, *outPath, *minSongs, *includeMemory, *forceNotes, time.Duration(*paceSecs)*time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}

func runEnrich(csvPath, outPath string, minSongs int) {
	songs, err := csvio.Load(csvPath, minSongs)
	if err != nil {
		fail(err)
	}
	canonicalTitles(songs)
	cached, err := enrich.LoadFile(outPath)
	if err != nil {
		fail(err)
	}
	overrides, err := enrich.LoadOverrides("data/overrides.csv")
	if err != nil {
		fail(err)
	}
	sp := spotify.New(env("SPOTIFY_CLIENT_ID"), env("SPOTIFY_CLIENT_SECRET"))
	lf := lastfm.New(env("LASTFM_API_KEY"))
	mb := musicbrainz.New()

	fetched := map[string]enrich.Record{}
	var lowConf []string
	for _, s := range songs {
		k := strings.ToLower(s.Song) + "|" + strings.ToLower(s.Artist)
		rec := enrich.Record{Song: s.Song, Artist: s.Artist}
		// metrics are refetched every run so the numbers stay current
		if env("LASTFM_API_KEY") != "" {
			if l, p, err := lf.TrackInfo(s.Artist, s.Song); err == nil {
				rec.LastfmListeners, rec.LastfmPlaycount = l, p
			}
			time.Sleep(250 * time.Millisecond)
		}
		// matching is deterministic and cached: only new or changed rows search Spotify
		needMatch := true
		for _, old := range cached.Songs {
			if strings.EqualFold(old.Song, s.Song) && strings.EqualFold(old.Artist, s.Artist) && old.SpotifyID != "" {
				rec.SpotifyID, rec.SpotifyURL = old.SpotifyID, old.SpotifyURL
				rec.Album, rec.ReleaseYear = old.Album, old.ReleaseYear
				rec.MatchConfidence = old.MatchConfidence
				rec.SpotifyPopularity = old.SpotifyPopularity
				rec.AlbumArt = old.AlbumArt
				rec.YearConflict = old.YearConflict
				// Existing matches stay pinned, including while adding art.
				needMatch = false
				break
			}
		}
		// Re-resolve cached matches that landed on a hits package or expanded
		// re-release: Spotify's top hit for a catalog song is often a later
		// compilation (e.g. Zombie pinned to Gold 2008 instead of No Need To
		// Argue 1994). One live search per flagged song; the pin only moves
		// when the same title+artist exists on an earlier release, and every
		// move is printed so the run log shows what changed.
		if !needMatch && env("SPOTIFY_CLIENT_ID") != "" && (spotify.LooksLikeCompilation(rec.Album) || rec.YearConflict) {
			if tracks, err := sp.Search(s.Song, s.Artist); err != nil {
				fmt.Fprintln(os.Stderr, "warn:", err)
			} else if best, exact := spotify.PickBest(tracks, s.Song, s.Artist); exact {
				by := 0
				if len(best.ReleaseDate) >= 4 {
					fmt.Sscanf(best.ReleaseDate[:4], "%d", &by)
				}
				if by > 0 && by < rec.ReleaseYear {
					fmt.Printf("re-matched: %s - %s (%s %d -> %s %d)\n", s.Song, s.Artist, rec.Album, rec.ReleaseYear, best.Album, by)
					rec.SpotifyID, rec.SpotifyURL = best.ID, best.URL
					rec.Album, rec.ReleaseYear = best.Album, by
					rec.AlbumArt = best.AlbumArt
					rec.SpotifyPopularity = best.Popularity
					rec.MatchConfidence = "exact"
					rec.YearConflict = false
				}
			}
		}
		if needMatch && env("SPOTIFY_CLIENT_ID") != "" {
			tracks, err := sp.Search(s.Song, s.Artist)
			if err != nil {
				fmt.Fprintln(os.Stderr, "warn:", err)
			} else if len(tracks) > 0 {
				best, exactMatch := spotify.PickBest(tracks, s.Song, s.Artist)
				rec.SpotifyID, rec.SpotifyURL = best.ID, best.URL
				rec.Album = best.Album
				rec.AlbumArt = best.AlbumArt
				rec.SpotifyPopularity = best.Popularity
				if len(best.ReleaseDate) >= 4 {
					fmt.Sscanf(best.ReleaseDate[:4], "%d", &rec.ReleaseYear)
				}
				if exactMatch {
					rec.MatchConfidence = "exact"
				} else {
					rec.MatchConfidence = "fuzzy"
					lowConf = append(lowConf, fmt.Sprintf("fuzzy: %s - %s matched to %s - %s", s.Song, s.Artist, best.Name, best.Artist))
				}
				// cross-check the release year; flag disagreement, never resolve silently
				if y, err := mb.FirstReleaseYear(s.Artist, s.Song); err == nil && y > 0 && rec.ReleaseYear > 0 && y < rec.ReleaseYear {
					rec.YearConflict = true
				}
				time.Sleep(time.Second) // musicbrainz courtesy
			}
		}
		// Never invent a match. A CSV title correction with the same artist is a
		// migration, not a new song: keep the pinned track data instead of
		// rematching against the changed title.
		if rec.SpotifyID == "" {
			for _, old := range cached.Songs {
				if strings.EqualFold(old.Artist, s.Artist) && !strings.EqualFold(old.Song, s.Song) && old.SpotifyID != "" {
					rec.SpotifyID, rec.SpotifyURL = old.SpotifyID, old.SpotifyURL
					rec.Album, rec.ReleaseYear = old.Album, old.ReleaseYear
					rec.MatchConfidence = "manual"
					rec.AlbumArt = old.AlbumArt
					rec.SpotifyPopularity = old.SpotifyPopularity
					break
				}
			}
		}
		// never invent a match: unmatched songs keep their CSV data and any
		// Last.fm metrics, render without a Spotify link, and are flagged
		if rec.SpotifyID == "" {
			rec.MatchConfidence = "unmatched"
		}
		rec.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		fetched[k] = rec
	}

	out, problems := enrich.Merge(songs, fetched, cached, overrides, time.Now())
	// Fetch missing artwork by the FINAL pinned ID, never by rematching a
	// cached song. This also respects manual album/track overrides.
	if env("SPOTIFY_CLIENT_ID") != "" {
		for i := range out.Songs {
			r := &out.Songs[i]
			if r.SpotifyID != "" && r.AlbumArt == "" {
				art, err := sp.CoverArt(r.SpotifyID)
				if err != nil {
					fmt.Fprintln(os.Stderr, "warn: cover art:", r.Song, err)
				} else {
					r.AlbumArt = art
				}
				time.Sleep(250 * time.Millisecond)
			}
		}
	}
	if err := out.Save(outPath); err != nil {
		fail(err)
	}
	for _, p := range append(problems, lowConf...) {
		fmt.Println(p)
	}
	matched := 0
	for _, r := range out.Songs {
		if r.SpotifyID != "" {
			matched++
		}
	}
	fmt.Printf("enriched %d/%d songs (%d unmatched) -> %s\n", matched, len(songs), len(songs)-matched, outPath)
}

func canonicalTitles(songs []csvio.Song) {
	// Keep display, enrichment, and note keys in sync after an official-title
	// correction. Values here are exact, curated title changes.
	for i := range songs {
		switch strings.ToLower(songs[i].Song) + "|" + strings.ToLower(songs[i].Artist) {
		case "smells like teen spirit|nirvana":
			songs[i].Song = "Smells Like Teen Spirit"
		case "there is a light that never goes out|the smiths":
			songs[i].Song = "There Is a Light That Never Goes Out"
		case "would|alice in chains":
			songs[i].Song = "Would?"
		case "freshman|verve pipe":
			songs[i].Song = "The Freshmen"
		}
	}
}

func runSite(outPath, siteDir, contextPath, base string) {
	f, err := enrich.LoadFile(outPath)
	if err != nil {
		fail(err)
	}
	ctx, err := notes.Load(contextPath)
	if err != nil {
		fail(err)
	}
	// optional narrative chapter order: a plain text file next to the
	// enriched data, one chapter name per line, editable in the web UI.
	// Missing file just means the year+stage fallback orders everything.
	var chapterOrder []string
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(outPath), "chapter_order.txt")); err == nil {
		chapterOrder = strings.Split(string(b), "\n")
	}
	if err := site.Render(f, ctx, siteDir, base, chapterOrder); err != nil {
		fail(err)
	}
	fmt.Printf("rendered %d songs -> %s\n", len(f.Songs), siteDir)
}

func oldTitle(s csvio.Song) string {
	switch strings.ToLower(s.Song) + "|" + strings.ToLower(s.Artist) {
	case "smells like teen spirit|nirvana":
		return "Smells like Teen Spirit"
	case "there is a light that never goes out|the smiths":
		return "There is a light that never goes out"
	case "would?|alice in chains":
		return "Would"
	case "the freshmen|verve pipe":
		return "Freshman"
	}
	return ""
}

const notesPrompt = `You are writing a background note for a personal music page.

Song: %s
Artist: %s
Tags from the listener: %s
%s
Verified facts, sourced from Spotify and Wikipedia:
%s
Write exactly 2 sentences about the song. Sentence 1: release context, chart history, or musical context, using ONLY the verified facts above - never state an album name, year, chart position, or any other specific that is not written there; if the facts do not include one, write around it, a shorter accurate note is better than a detailed wrong one. If the verified facts name the album the song appears on, mention it in sentence 1. Sentence 2: how the song connects to the listener's memory above, or its lasting resonance if no memory was shared. %sNever invent facts about the listener's life; you may only reference what they wrote above.

Style rules, no exceptions: write in the third person only; refer to the person as "this listener" or "the listener". NEVER write "you", "your", "yours", "you're", "you've", or "you'll" ANYWHERE in the response, including inside song titles, quoted lyrics, album titles, or band names. If the song or album title contains one of those words, still refer to the song only by the exact title from the Song line when quoting it, or avoid quotation marks. This validator has no exceptions for titles. No self-corrections, restarts, or hedges (never write "wait", "actually", "no,", or similar). No meta commentary about the writing itself.

Respond with JSON only: {"note": "...", "source_url": "..."} where source_url is a real URL (Wikipedia, official chart archive, Last.fm) supporting the note. If you cannot give a real source URL, respond {"note": "", "source_url": ""}.`

// reissueSuffix matches trailing reissue markers on album titles from
// Spotify (Deluxe Edition, 2022 Remaster, ...) so notes name the album,
// not the reissue. Parentheticals that are part of the title survive.
var reissueSuffix = regexp.MustCompile(`(?i)\s*\((?:[^)]*(?:remaster|deluxe|edition|expanded|reissue|bonus|anniversary)[^)]*)\)\s*$`)

func cleanAlbum(title string) string {
	for {
		out := strings.TrimSpace(reissueSuffix.ReplaceAllString(title, ""))
		if out == title {
			return out
		}
		title = out
	}
}

func runNotes(csvPath, contextPath, enrichedPath string, minSongs int, includeMemory, force bool, pace time.Duration) {
	songs, err := csvio.Load(csvPath, minSongs)
	if err != nil {
		fail(err)
	}
	canonicalTitles(songs)
	ctx, err := notes.Load(contextPath)
	if err != nil {
		fail(err)
	}
	for _, s := range songs {
		old := oldTitle(s)
		if old != "" {
			notes.Migrate(ctx.Notes, old, s.Artist, s.Song)
		}
	}
	g := gemini.New(env("GEMINI_API_KEY"), env("GEMINI_MODEL"))
	wk := wikipedia.New()
	enriched := map[string]enrich.Record{}
	if ef, err := enrich.LoadFile(enrichedPath); err != nil {
		fmt.Fprintln(os.Stderr, "warn: enriched data:", err, "(notes will lack album facts)")
	} else {
		for _, r := range ef.Songs {
			enriched[notes.Key(r.Song, r.Artist)] = r
		}
	}

	type reply struct {
		Note      string `json:"note"`
		SourceURL string `json:"source_url"`
	}
	var added, dropped, kept, skipped, retried int
	for _, s := range songs {
		k := notes.Key(s.Song, s.Artist)
		if !force {
			if old, ok := ctx.Notes[k]; ok && notes.Valid(old) {
				skipped++
				continue // existing note preserved; use --force to regenerate
			}
		}
		memCtx, memRule := "", "The listener did not share a personal memory for this one. "
		if includeMemory && s.Memory != "" {
			memCtx = "The listener's own words about it: " + s.Memory + "\n"
			memRule = "You may acknowledge the listener's association with the song. "
		}

		// Grounding: pull sourced facts first so the model is only ever
		// handed real album names, years and chart history.
		var factsLines []string
		if rec, ok := enriched[notes.Key(s.Song, s.Artist)]; ok && rec.Album != "" {
			album := cleanAlbum(rec.Album)
			if strings.EqualFold(album, s.Song) {
				// title matches the song: the release is the single itself
				factsLines = append(factsLines, fmt.Sprintf("- Release (Spotify data): %q (%d), a single", album, rec.ReleaseYear))
			} else {
				factsLines = append(factsLines, fmt.Sprintf("- Album (Spotify data): %q (%d)", album, rec.ReleaseYear))
			}
		}
		wikiURL := ""
		if wTitle, wURL, wExtract, err := wk.Lookup(s.Song, s.Artist); err != nil {
			fmt.Fprintln(os.Stderr, "warn: wikipedia:", s.Song, "-", err)
		} else if wExtract != "" {
			wikiURL = wURL
			factsLines = append(factsLines, fmt.Sprintf("- Wikipedia article %q intro: %s", wTitle, wExtract))
		}
		if s.LifeYear > 0 {
			factsLines = append(factsLines, fmt.Sprintf("- The listener places this song around %d in their own life.", s.LifeYear))
		}
		if len(factsLines) == 0 {
			factsLines = append(factsLines, "- No sourced facts found for this song. State no release year, album name, chart position or other specific.")
		}
		factsBlock := strings.Join(factsLines, "\n")
		// claimsText is what the fact validator checks the note against: the
		// facts plus the song's own title/artist (quoting them is fine).
		claimsText := factsBlock + "\n" + s.Song + "\n" + s.Artist + "\n" + s.Memory + "\n" + s.Tags
		prompt := fmt.Sprintf(notesPrompt, s.Song, s.Artist, s.Tags, memCtx, factsBlock, memRule)
		text, err := g.Generate(prompt)
		if err != nil {
			fmt.Fprintln(os.Stderr, "warn:", s.Song, "-", err)
			dropped++
			time.Sleep(pace)
			continue
		}
		text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
		text = strings.TrimPrefix(text, "json")
		var r reply
		if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &r); err != nil {
			fmt.Printf("dropped (unparseable): %s - %s\n", s.Song, s.Artist)
			dropped++
			time.Sleep(pace)
			continue
		}
		n := notes.Note{Note: strings.TrimSpace(r.Note), SourceURL: strings.TrimSpace(r.SourceURL)}
		if notes.Valid(n) && (!notes.StyleOK(n.Note) || !notes.ClaimsOK(n.Note, claimsText)) {
			// one retry for voice slips or ungrounded claims, then keep the old note
			retried++
			fmt.Printf("style/fact retry: %s - %s\n", s.Song, s.Artist)
			time.Sleep(pace)
			text, err = g.Generate(prompt)
			if err != nil {
				fmt.Fprintln(os.Stderr, "warn:", s.Song, "-", err)
				time.Sleep(pace)
				kept++
				continue
			}
			text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
			text = strings.TrimPrefix(text, "json")
			if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &r); err == nil {
				n = notes.Note{Note: strings.TrimSpace(r.Note), SourceURL: strings.TrimSpace(r.SourceURL)}
			}
		}
		if wikiURL != "" {
			n.SourceURL = wikiURL // the article we actually pulled facts from
		}
		switch {
		case !notes.Valid(n):
			fmt.Printf("dropped (no source): %s - %s\n", s.Song, s.Artist)
			dropped++
		case !notes.StyleOK(n.Note):
			fmt.Printf("kept existing (style check failed after retry): %s - %s\n", s.Song, s.Artist)
			kept++
		case !notes.ClaimsOK(n.Note, claimsText):
			fmt.Printf("kept existing (fact check failed after retry): %s - %s\n", s.Song, s.Artist)
			kept++
		default:
			ctx.Notes[k] = n
			added++
		}
		time.Sleep(pace) // stay under the API rate limit
	}
	ctx.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	ctx.Model = g.Model()
	ctx.IncludeMemory = includeMemory
	if err := ctx.Save(contextPath); err != nil {
		fail(err)
	}
	fmt.Printf("notes: %d written, %d skipped (existing), %d style retries, %d kept old after failed retry, %d dropped -> %s\n", added, skipped, retried, kept, dropped, contextPath)
	fmt.Println("Review the notes before committing - the site only renders what you commit.")
}

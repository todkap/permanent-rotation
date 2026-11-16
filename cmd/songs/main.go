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
		runNotes(*csvPath, *contextPath, *minSongs, *includeMemory, time.Duration(*paceSecs)*time.Second)
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
				needMatch = false
				break
			}
		}
		if needMatch && env("SPOTIFY_CLIENT_ID") != "" {
			tracks, err := sp.Search(s.Song, s.Artist)
			if err != nil {
				fmt.Fprintln(os.Stderr, "warn:", err)
			} else if len(tracks) > 0 {
				best := tracks[0]
				rec.SpotifyID, rec.SpotifyURL = best.ID, best.URL
				rec.Album = best.Album
				rec.SpotifyPopularity = best.Popularity
				if len(best.ReleaseDate) >= 4 {
					fmt.Sscanf(best.ReleaseDate[:4], "%d", &rec.ReleaseYear)
				}
				if strings.EqualFold(best.Name, s.Song) && strings.EqualFold(best.Artist, s.Artist) {
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
		// never invent a match: unmatched songs keep their CSV data and any
		// Last.fm metrics, render without a Spotify link, and are flagged
		if rec.SpotifyID == "" {
			rec.MatchConfidence = "unmatched"
		}
		rec.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		fetched[k] = rec
	}

	out, problems := enrich.Merge(songs, fetched, cached, overrides, time.Now())
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

const notesPrompt = `You are writing a background note for a personal music page.

Song: %s
Artist: %s
Tags from the listener: %s
%s
Write a 2-3 sentence background note about the song: release context, chart history, or musical context. Facts must be real - if you are not confident of a fact, leave it out. %sNever invent facts about the listener's life; you may only reference what they wrote above.

Respond with JSON only: {"note": "...", "source_url": "..."} where source_url is a real URL (Wikipedia, official chart archive, Last.fm) supporting the note. If you cannot give a real source URL, respond {"note": "", "source_url": ""}.`

func runNotes(csvPath, contextPath string, minSongs int, includeMemory bool, pace time.Duration) {
	songs, err := csvio.Load(csvPath, minSongs)
	if err != nil {
		fail(err)
	}
	ctx, err := notes.Load(contextPath)
	if err != nil {
		fail(err)
	}
	g := gemini.New(env("GEMINI_API_KEY"), env("GEMINI_MODEL"))

	type reply struct {
		Note      string `json:"note"`
		SourceURL string `json:"source_url"`
	}
	var added, dropped int
	for _, s := range songs {
		k := notes.Key(s.Song, s.Artist)
		memCtx, memRule := "", "The listener did not share a personal memory for this one. "
		if includeMemory && s.Memory != "" {
			memCtx = "The listener's own words about it: " + s.Memory + "\n"
			memRule = "You may acknowledge the listener's association with the song. "
		}
		prompt := fmt.Sprintf(notesPrompt, s.Song, s.Artist, s.Tags, memCtx, memRule)
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
		if !notes.Valid(n) {
			fmt.Printf("dropped (no source): %s - %s\n", s.Song, s.Artist)
			dropped++
		} else {
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
	fmt.Printf("notes: %d written, %d dropped -> %s\n", added, dropped, contextPath)
	fmt.Println("Review the notes before committing - the site only renders what you commit.")
}

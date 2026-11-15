// songs is the CLI behind permanent-rotation:
//
//	songs enrich   match songs.csv against Spotify + Last.fm, write enriched.json
//	songs site     render the static site from enriched.json
//	songs playlist create/update the Spotify playlist (local run, owner login)
//	songs notes    opt-in Gemini background notes layer (reviewed before commit)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/todkap/permanent-rotation/internal/csvio"
	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/lastfm"
	"github.com/todkap/permanent-rotation/internal/musicbrainz"
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
	minSongs := fs.Int("min", csvio.DefaultMinSongs, "minimum valid songs required")
	fs.Parse(args)

	switch cmd {
	case "enrich":
		runEnrich(*csvPath, *outPath, *minSongs)
	case "site":
		runSite(*outPath, *siteDir)
	case "playlist":
		fmt.Println("playlist is not implemented in this skeleton yet.")
		fmt.Println("Design (spec section 6): local run only, browser consent once,")
		fmt.Println("redirect http://127.0.0.1:8888/callback, then POST /me/playlists.")
		fmt.Println("Order follows life chapters, no ranking.")
		os.Exit(1)
	case "notes":
		fmt.Println("notes is not implemented in this skeleton yet.")
		fmt.Println("Design (spec section 6): opt-in per run, sends title/artist/tags only")
		fmt.Println("(memory text only with --include-memory), Gemini via GEMINI_API_KEY,")
		fmt.Println("every note needs a source URL, output is reviewed before commit.")
		os.Exit(1)
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
		// never invent a match: unmatched songs are listed and skipped by Merge,
		// unless an override pins them by hand
		if rec.SpotifyID != "" {
			rec.FetchedAt = time.Now().UTC().Format(time.RFC3339)
			fetched[k] = rec
		} else if _, pinned := overrides[k]; pinned {
			fetched[k] = rec
		}
	}

	out, problems := enrich.Merge(songs, fetched, cached, overrides, time.Now())
	if err := out.Save(outPath); err != nil {
		fail(err)
	}
	for _, p := range append(problems, lowConf...) {
		fmt.Println(p)
	}
	fmt.Printf("enriched %d/%d songs -> %s\n", len(out.Songs), len(songs), outPath)
}

func runSite(outPath, siteDir string) {
	f, err := enrich.LoadFile(outPath)
	if err != nil {
		fail(err)
	}
	if err := site.Render(f, siteDir); err != nil {
		fail(err)
	}
	fmt.Printf("rendered %d songs -> %s\n", len(f.Songs), siteDir)
}

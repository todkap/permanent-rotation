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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	batch := fs.Int("batch", 4, "maximum songs making source requests per notes run")
	recoveryRef := fs.String("recovery-ref", "", "one-time restore of missing notes from this git commit")
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
	case "notes-recover":
		if err := recoverNotes(*contextPath, *recoveryRef); err != nil {
			fail(err)
		}
	case "notes":
		if *batch < 1 || *batch > 4 {
			fail(fmt.Errorf("notes batch must be between 1 and 4"))
		}
		notesBatch = *batch
		runNotes(*csvPath, *contextPath, *outPath, *minSongs, *includeMemory, *forceNotes, time.Duration(*paceSecs)*time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}
}

// report prints one per-song decision to the run log and remembers it for
// the GitHub job summary, so a run explains every move it made and every
// song it left alone. flushSummary writes the collected lines to
// GITHUB_STEP_SUMMARY when the workflow provides it.
var reportLines []string

func report(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Println(line)
	reportLines = append(reportLines, line)
}

// mdEsc escapes Markdown punctuation in album, title and artist names that
// come from Spotify, MusicBrainz or Wikipedia, so external text cannot break
// the job-summary rendering. The run log keeps the plain text.
var mdEsc = strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "|", "\\|")

func flushSummary(section string) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" || len(reportLines) == 0 {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n### %s\n\n", section)
	for _, l := range reportLines {
		fmt.Fprintf(f, "- %s\n", mdEsc.Replace(l))
	}
	reportLines = nil
}

// adoptPin moves a record's pin to a better candidate, records why, and
// marks the release changed so the notes run regenerates any note written
// against the old album/year.
func adoptPin(rec *enrich.Record, best spotify.Track, why string) {
	y := 0
	if len(best.ReleaseDate) >= 4 {
		fmt.Sscanf(best.ReleaseDate[:4], "%d", &y)
	}
	report("re-matched: %s - %s (%s %d -> %s %d; %s)", rec.Song, rec.Artist, rec.Album, rec.ReleaseYear, best.Album, y, why)
	rec.SpotifyID, rec.SpotifyURL = best.ID, best.URL
	rec.Album, rec.ReleaseYear = best.Album, y
	rec.AlbumType = best.AlbumType
	rec.AlbumArt = best.AlbumArt
	rec.SpotifyPopularity = best.Popularity
	rec.MatchConfidence = "exact"
	rec.MatchReason = why
	rec.ReleaseChanged = true
	rec.YearConflict = false
}

// rescueCompilationPin gives a pinned compilation or late re-release one
// targeted retry: MusicBrainz names the original album and year, and Spotify
// is asked for that album's version of the track directly, because a plain
// search's 10 hits can be all compilations (Zombie surfaced only Gold and
// Stars until asked for No Need To Argue). Both the album-targeted and the
// year-targeted searches run, and the combined pool competes: stopping at
// the first earlier hit can still miss the original pressing. The pin moves
// only to an exact title+artist match that is strictly better than the
// cached pin.
// rescueCompilationPin returns the size of the candidate pool it searched,
// so the caller can distinguish "original not found in the bounded Spotify searches" from
// "candidates found but none better than the pin".
func rescueCompilationPin(sp *spotify.Client, mb *musicbrainz.Client, wk *wikipedia.Client, song, artist, mbAlbum string, mbYear int, rec *enrich.Record) int {
	var pool []spotify.Track
	seen := map[string]bool{rec.SpotifyID: true}
	add := func(tracks []spotify.Track) {
		for _, t := range tracks {
			if !seen[t.ID] {
				seen[t.ID] = true
				pool = append(pool, t)
			}
		}
	}
	if mbAlbum != "" {
		if tracks, err := sp.SearchAlbum(song, artist, mbAlbum); err != nil {
			fmt.Fprintln(os.Stderr, "warn:", err)
		} else {
			add(tracks)
		}
	}
	if mbYear > 0 {
		if tracks, err := sp.SearchYear(song, artist, mbYear); err != nil {
			fmt.Fprintln(os.Stderr, "warn:", err)
		} else {
			add(tracks)
		}
	}
	// ISRC pool: MusicBrainz identifies the recording itself, and Spotify
	// lists every catalog instance of it - the original pressing surfaces
	// even when album/year filters miss it (Footloose sits on the 1984 OST
	// under a "- From ... Soundtrack" track title).
	if isrc, err := mb.RecordingISRC(artist, song); err != nil {
		fmt.Fprintln(os.Stderr, "warn:", err)
	} else if isrc != "" {
		time.Sleep(time.Second) // musicbrainz courtesy
		if tracks, err := sp.SearchISRC(isrc); err != nil {
			fmt.Fprintln(os.Stderr, "warn:", err)
		} else {
			add(tracks)
		}
	}
	if _, _, _, album, err := wk.LookupRelease(song, artist, rec.Album); err != nil {
		report("rescue: %s - %s: Wikipedia source deferred: %v", song, artist, err)
	} else if album != "" {
		if tracks, err := sp.SearchAlbum(song, artist, album); err == nil {
			add(tracks)
		} else {
			report("rescue: %s: album search failed: %v", song, err)
		}
	}
	if len(pool) == 0 {
		return 0
	}
	best, exact := spotify.PickBest(pool, song, artist)
	if exact && spotify.BetterPin(best, spotify.Track{Album: rec.Album, AlbumType: rec.AlbumType, ReleaseDate: fmt.Sprint(rec.ReleaseYear)}) {
		adoptPin(rec, best, "targeted rescue via MusicBrainz original release")
	}
	return len(pool)
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
	overrideKeys := map[string]bool{}
	for k := range overrides {
		overrideKeys[k] = true
	}
	currentRows := map[string]bool{}
	for _, s := range songs {
		currentRows[strings.ToLower(s.Song)+"|"+strings.ToLower(s.Artist)] = true
	}
	sp := spotify.New(env("SPOTIFY_CLIENT_ID"), env("SPOTIFY_CLIENT_SECRET"))
	lf := lastfm.New(env("LASTFM_API_KEY"))
	mb := musicbrainz.New()
	wk := wikipedia.New() // one budget, cache and cooldown across every enrich rescue

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
				// A pin moved in an earlier build keeps its marker until the
				// notes run regenerates that song's note; the reason text is
				// preserved the same way.
				copyReleaseState(&rec, old)
				rec.MatchReason = old.MatchReason
				rec.AlbumType = old.AlbumType
				// Existing matches stay pinned, including while adding art.
				needMatch = false
				break
			}
		}
		// Re-evaluate every automatic pin against the earliest-original rule,
		// every build: a pin can sit on a compilation without any keyword flag
		// (Crazy for You sat on Something to Remember), so a flag gate misses
		// real cases. Manual pins (overrides, carried-over title corrections)
		// are never rematched. The pin moves only when the search produces an
		// exact title+artist match that is strictly better - earlier, or the
		// same year in plainer packaging - so Spotify result-order churn can
		// never move a card backwards.
		manual := strings.EqualFold(rec.MatchConfidence, "manual") || overrideKeys[k]
		switch {
		case needMatch:
			// handled below
		case manual:
			if rec.MatchReason == "" {
				rec.MatchReason = "kept: manual pin, never rematched"
			}
			report("re-check: %s - %s: manual pin, keeping %s %d", s.Song, s.Artist, rec.Album, rec.ReleaseYear)
		case env("SPOTIFY_CLIENT_ID") == "":
			if rec.MatchReason == "" {
				rec.MatchReason = "kept: cached pin (no Spotify credentials this run)"
			}
		default:
			metadataOK := rec.AlbumType != ""
			if !metadataOK {
				if d, err := sp.Details(rec.SpotifyID); err == nil {
					rec.AlbumType = d.AlbumType
					metadataOK = true
				} else {
					report("metadata: %s: %v; retaining pin until metadata is verified", s.Song, err)
				}
			}
			tracks, err := sp.Search(s.Song, s.Artist)
			switch {
			case err != nil:
				rec.MatchReason = "kept: Spotify search failed; original remains unverified"
				report("re-check: %s - %s: search failed (%v), keeping %s %d", s.Song, s.Artist, err, rec.Album, rec.ReleaseYear)
			default:
				best, exact := spotify.PickBest(tracks, s.Song, s.Artist)
				switch {
				case !exact:
					setRecheckReason(&rec, "no-exact")
					report("re-check: %s - %s: no exact title+artist in search results, keeping %s %d", s.Song, s.Artist, rec.Album, rec.ReleaseYear)
				case best.ID == rec.SpotifyID:
					setRecheckReason(&rec, "confirmed")
					report("re-check: %s - %s: pin confirmed %s %d", s.Song, s.Artist, rec.Album, rec.ReleaseYear)
				case metadataOK && spotify.BetterPin(best, spotify.Track{Album: rec.Album, AlbumType: rec.AlbumType, ReleaseDate: fmt.Sprint(rec.ReleaseYear)}):
					adoptPin(&rec, best, "earlier or plainer original in plain search")
				default:
					setRecheckReason(&rec, "no-better")
					report("re-check: %s - %s: search offered nothing better, keeping %s %d", s.Song, s.Artist, rec.Album, rec.ReleaseYear)
				}
			}
			// MusicBrainz names the original release; if the pin still sits
			// later than it (or still reads like a compilation), one targeted
			// rescue asks Spotify for that album/year directly.
			mbAlbum, mbYear, mbErr := mb.FirstRelease(s.Artist, s.Song)
			time.Sleep(time.Second) // musicbrainz courtesy
			if mbErr == nil && mbYear > 0 {
				if metadataOK && (rec.ReleaseYear > mbYear || spotify.LooksLikeCompilation(rec.Album) || rec.AlbumType == "single") {
					before := rec.SpotifyID
					poolSize := rescueCompilationPin(sp, mb, wk, s.Song, s.Artist, mbAlbum, mbYear, &rec)
					if rec.SpotifyID == before {
						if poolSize == 0 {
							report("rescue: %s - %s: original (%s %d) not found in the bounded Spotify searches, keeping %s %d", s.Song, s.Artist, mbAlbum, mbYear, rec.Album, rec.ReleaseYear)
						} else {
							report("rescue: %s - %s: none of %d targeted candidates beats %s %d (original: %s %d)", s.Song, s.Artist, poolSize, rec.Album, rec.ReleaseYear, mbAlbum, mbYear)
						}
					}
				}
				rec.YearConflict = rec.ReleaseYear > mbYear
			}
			if rec.MatchReason == "" {
				rec.MatchReason = "kept: no better original found"
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
				rec.AlbumType = best.AlbumType
				rec.AlbumArt = best.AlbumArt
				rec.SpotifyPopularity = best.Popularity
				if len(best.ReleaseDate) >= 4 {
					fmt.Sscanf(best.ReleaseDate[:4], "%d", &rec.ReleaseYear)
				}
				if exactMatch {
					rec.MatchConfidence = "exact"
					rec.MatchReason = "matched: exact title+artist, earliest original in search"
					report("matched: %s - %s -> %s %d (exact)", s.Song, s.Artist, rec.Album, rec.ReleaseYear)
				} else {
					rec.MatchConfidence = "fuzzy"
					rec.MatchReason = "matched: no exact title+artist, top search result"
					report("matched: %s - %s -> %s %d (fuzzy: %s - %s)", s.Song, s.Artist, rec.Album, rec.ReleaseYear, best.Name, best.Artist)
					lowConf = append(lowConf, fmt.Sprintf("fuzzy: %s - %s matched to %s - %s", s.Song, s.Artist, best.Name, best.Artist))
				}
				// cross-check the release year against MusicBrainz; a later
				// Spotify year gets one targeted rescue search for the
				// original album, and any remaining disagreement is flagged,
				// never silently resolved
				if mbAlbum, mbYear, err := mb.FirstRelease(s.Artist, s.Song); err == nil && mbYear > 0 && rec.ReleaseYear > 0 && mbYear < rec.ReleaseYear {
					rescueCompilationPin(sp, mb, wk, s.Song, s.Artist, mbAlbum, mbYear, &rec)
					rec.YearConflict = rec.ReleaseYear > mbYear
				}
				time.Sleep(time.Second) // musicbrainz courtesy
			}
		}
		// Never invent a match. A CSV title correction with the same artist is
		// a migration, not a new song - but only when the titles are clearly
		// the same song (Would -> Would?, Freshman -> The Freshmen), never
		// merely because the artist matches, and never from a title that is
		// still in the CSV as its own row.
		if rec.SpotifyID == "" {
			for _, old := range cached.Songs {
				if !strings.EqualFold(old.Artist, s.Artist) || strings.EqualFold(old.Song, s.Song) || old.SpotifyID == "" {
					continue
				}
				if currentRows[strings.ToLower(old.Song)+"|"+strings.ToLower(old.Artist)] {
					continue // old title still exists as its own CSV row
				}
				if !similarTitles(s.Song, old.Song) {
					continue
				}
				rec.SpotifyID, rec.SpotifyURL = old.SpotifyID, old.SpotifyURL
				rec.Album, rec.ReleaseYear = old.Album, old.ReleaseYear
				rec.MatchConfidence = "manual"
				rec.MatchReason = fmt.Sprintf("carried over pin from corrected title %q", old.Song)
				rec.AlbumArt = old.AlbumArt
				rec.SpotifyPopularity = old.SpotifyPopularity
				report("migration: %s - %s: carried over pin from %q (%s %d)", s.Song, s.Artist, old.Song, rec.Album, rec.ReleaseYear)
				break
			}
		}
		// never invent a match: unmatched songs keep their CSV data and any
		// Last.fm metrics, render without a Spotify link, and are flagged
		if rec.SpotifyID == "" {
			rec.MatchConfidence = "unmatched"
			rec.MatchReason = "unmatched: no Spotify result accepted"
		}
		rec.FetchedAt = time.Now().UTC().Format(time.RFC3339)
		fetched[k] = rec
		// one terminal line per card, every build: the final pin and why
		if rec.SpotifyID == "" {
			report("card: %s - %s -> (no Spotify match)", s.Song, s.Artist)
		} else {
			report("card: %s - %s -> %s %d [%s]", s.Song, s.Artist, rec.Album, rec.ReleaseYear, rec.MatchReason)
		}
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
	flushSummary("Enrich: matching decisions")
	matched := 0
	for _, r := range out.Songs {
		if r.SpotifyID != "" {
			matched++
		}
	}
	fmt.Printf("enriched %d/%d songs (%d unmatched) -> %s\n", matched, len(songs), len(songs)-matched, outPath)
}

// similarTitles reports whether two titles plausibly name the same song
// after normalization (case, punctuation, leading articles): the guard for
// carrying a pin across a CSV title correction. A ratio of 0.7 accepts
// Would/Would? and Freshman/The Freshmen while keeping genuinely different
// songs by the same artist apart.
func similarTitles(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToLower(s)
		var sb strings.Builder
		for _, r := range s {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				sb.WriteRune(r)
			}
		}
		return strings.TrimPrefix(sb.String(), "the")
	}
	na, nb := norm(a), norm(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	d := levenshtein(na, nb)
	longest := len(na)
	if len(nb) > longest {
		longest = len(nb)
	}
	return float64(longest-d)/float64(longest) >= 0.7
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
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
Write exactly 2 sentences about the song. Sentence 1: release context, chart history, or musical context, using ONLY the verified facts above - never state an album name, year, chart position, or any other specific that is not written there; if the facts do not include one, write around it, a shorter accurate note is better than a detailed wrong one. Keep SINGLE release dates separate from ALBUM release dates. Never write "released in [single year] on the album [album]" when that album has a different year. Use separate explicit clauses, or omit the date if the relationship is uncertain. If the verified facts name the album, mention it only if its title has no second-person words. For titles such as A Fever You Can't Sweat Out, omit the album name and use other verified musical context instead; the site already shows the album. Do not quote lyrics or use any second-person pronoun in either sentence. Sentence 2: how the song connects to the listener's memory above, or its lasting resonance if no memory was shared. %sNever invent facts about the listener's life; you may only reference what they wrote above.

Style rules: write in the third person; refer to the person as "this listener" or "the listener". Do not address the listener as you/your. Exact song or artist names may contain those words. Do not expand initials, abbreviations, or nicknames from the memory into names not provided in the facts. Do not put memory paraphrases in quotation marks. No self-corrections, restarts, or hedges (never write "wait", "actually", "no,", or similar). No meta commentary about the writing itself.

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
	var ef *enrich.File
	enriched := map[string]*enrich.Record{}
	if f, err := enrich.LoadFile(enrichedPath); err != nil {
		fmt.Fprintln(os.Stderr, "warn: enriched data:", err, "(notes will lack album facts)")
	} else {
		ef = f
		for i := range ef.Songs {
			r := &ef.Songs[i]
			enriched[notes.Key(r.Song, r.Artist)] = r
		}
	}

	type reply struct {
		Note      string `json:"note"`
		SourceURL string `json:"source_url"`
	}
	var added, dropped, missing, kept, skipped, retried, clearedMarkers int
	remoteSongs := 0
	now := time.Now().UTC()
	if until, err := time.Parse(time.RFC3339, ctx.RetryNotBefore); err == nil && now.Before(until) {
		report("notes: deferred entire batch until at least %s after prior source throttling; no notes changed", ctx.RetryNotBefore)
		flushSummary("Notes: deferred safely")
		return
	}
	sort.SliceStable(songs, func(i, j int) bool {
		pi, pj := notePriority(ctx, enriched, songs[i]), notePriority(ctx, enriched, songs[j])
		if pi != pj {
			return pi < pj
		}
		return ctx.LastAttempt[notes.Key(songs[i].Song, songs[i].Artist)] < ctx.LastAttempt[notes.Key(songs[j].Song, songs[j].Artist)]
	})
	if ctx.LastAttempt == nil {
		ctx.LastAttempt = map[string]string{}
	}
	for _, s := range songs {
		k := notes.Key(s.Song, s.Artist)
		rec, haveRec := enriched[k]
		old, hasOld := ctx.Notes[k]

		oldSourceOK, sourceNeedsRefresh := true, false
		stamped := hasOld && sourceStampValid(old, s.Song, s.Artist, now)
		if hasOld && stamped && !old.PendingVerification && !force && (!haveRec || !rec.ReleaseChanged) && noteAlbumIssue(old.Note, rec) == "" && (!haveRec || old.Album == cleanAlbum(rec.Album) && old.ReleaseYear == rec.ReleaseYear) {
			skipped++
			report("note: skipped (persisted verified source): %s - %s", s.Song, s.Artist)
			continue
		}
		if remoteSongs >= notesBatch {
			report("note: deferred (batch limit %d, unchanged): %s - %s", notesBatch, s.Song, s.Artist)
			continue
		}
		remoteSongs++
		ctx.LastAttempt[k] = now.Format(time.RFC3339)
		if hasOld && !stamped {
			canonical, ok, sourceErr := wk.CheckSource(old.SourceURL, s.Song, s.Artist, cleanAlbum(recAlbum(rec)))
			if sourceErr != nil {
				report("note: deferred (source UNKNOWN, existing text/marker unchanged): %s - %s: %v", s.Song, s.Artist, sourceErr)
				if until := wk.RetryNotBefore(); !until.IsZero() {
					recordThrottle(ctx, until, now)
					break
				}
				continue
			}
			oldSourceOK = ok
			sourceNeedsRefresh = !ok || canonical != old.SourceURL
			if ok && !sourceNeedsRefresh {
				stampSource(&old, s.Song, s.Artist, now)
				ctx.Notes[k] = old
			}
		}

		albumIssue := ""
		if hasOld && haveRec && rec.AlbumType != "single" {
			albumIssue = notes.AlbumYearIssue(old.Note, cleanAlbum(rec.Album), rec.ReleaseYear)
		}
		regenReason := ""
		switch {
		case force:
			regenReason = "forced regeneration"
		case hasOld && old.PendingVerification:
			regenReason = "recovered note pending verification"
		case sourceNeedsRefresh:
			regenReason = "source missing, changed, unverified or wrong identity"
		case albumIssue != "":
			regenReason = albumIssue
		case hasOld && haveRec && rec.ReleaseChanged:
			regenReason = "release changed since the note was written"
		case hasOld && haveRec && (old.Album != "" || old.ReleaseYear != 0) && rec.Album != "" && (cleanAlbum(rec.Album) != old.Album || rec.ReleaseYear != old.ReleaseYear):
			regenReason = "selected release differs from stamped note facts"
		}
		if !force && hasOld && notes.Valid(old) && regenReason == "" {
			skipped++
			report("note: skipped (existing note, source and release checked): %s - %s", s.Song, s.Artist)
			continue
		}

		memCtx, memRule := "", "The listener did not share a personal memory for this one. "
		if includeMemory && s.Memory != "" {
			memCtx = "The listener's own words about it: " + s.Memory + "\n"
			memRule = "You may acknowledge the listener's association with the song. "
		}

		// Grounding: pull sourced facts first so the model is only ever
		// handed real album names, years and chart history.
		var factsLines []string
		if haveRec && rec.Album != "" {
			album := cleanAlbum(rec.Album)
			if rec.AlbumType == "single" {
				// title matches the song: the release is the single itself
				factsLines = append(factsLines, fmt.Sprintf("- Release (Spotify data): %q (%d), a single", album, rec.ReleaseYear))
			} else {
				factsLines = append(factsLines, fmt.Sprintf("- Selected ALBUM release (Spotify data): %q; ALBUM year: %d. A Wikipedia single date is NOT this album year.", album, rec.ReleaseYear))
			}
		}
		wikiURL := ""
		if wTitle, wURL, wExtract, _, sourceErr := wk.LookupRelease(s.Song, s.Artist, cleanAlbum(recAlbum(rec))); sourceErr != nil {
			report("note: deferred (fresh source UNKNOWN, no destructive validation): %s - %s: %v", s.Song, s.Artist, sourceErr)
			if until := wk.RetryNotBefore(); !until.IsZero() {
				recordThrottle(ctx, until, now)
				break
			}
			continue
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

		// Bounded corrective retries: a failed attempt tells the model
		// exactly what was rejected (the unsupported year/title, the style
		// slip, the missing source) instead of repeating the same prompt.
		var n notes.Note
		issue := ""
		for attempt := 1; attempt <= 3; attempt++ {
			if wikiURL == "" {
				issue = "no verified non-disambiguation source article; no generation attempted"
				break
			}
			text, err := g.Generate(prompt)
			if err != nil {
				issue = fmt.Sprintf("generation failed: %v", err)
				break // API errors have their own backoff; retrying re-writes nothing
			}
			text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
			text = strings.TrimPrefix(text, "json")
			var r reply
			if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &r); err != nil {
				issue = "response was not the requested JSON"
				retried++
				report("note: retry %d (%s): %s - %s", attempt, issue, s.Song, s.Artist)
				prompt += "\nYour previous response was rejected: " + issue + ". Respond with the requested JSON only."
				time.Sleep(pace)
				continue
			}
			n = notes.Note{Note: strings.TrimSpace(r.Note), SourceURL: strings.TrimSpace(r.SourceURL)}
			if wikiURL != "" {
				n.SourceURL = wikiURL // the article we actually pulled facts from
			}
			if wikiURL == "" {
				issue = "no verified non-disambiguation source article; refusing model-supplied URL"
			} else if !notes.Valid(n) {
				issue = "no real source URL in the response"
			} else if si := notes.StyleIssue(n.Note, s.Song, s.Artist); si != "" {
				issue = "style check failed: " + si + "; rejected term: " + notes.SecondPersonTerm(n.Note, s.Song, s.Artist)
			} else if ai := noteAlbumIssue(n.Note, rec); ai != "" {
				issue = "fact check failed: " + ai
			} else if ci := notes.ClaimIssue(n.Note, claimsText); ci != "" {
				issue = "fact check failed: " + ci
			} else {
				issue = ""
				break
			}
			retried++
			report("note: retry %d (%s): %s - %s", attempt, issue, s.Song, s.Artist)
			prompt += "\nYour previous response was rejected: " + issue + ". Fix only that and respond again."
			time.Sleep(pace)
		}

		switch {
		case issue == "":
			// accepted: stamp the facts the note was written against, so a
			// later pin move regenerates it instead of going stale
			if haveRec && rec.Album != "" {
				n.Album, n.ReleaseYear = cleanAlbum(rec.Album), rec.ReleaseYear
			}
			stampSource(&n, s.Song, s.Artist, now)
			n.PendingVerification = false
			ctx.Notes[k] = n
			added++
			if haveRec && rec.ReleaseChanged {
				rec.ReleaseChanged = false
				clearedMarkers++
			}
			if regenReason != "" {
				report("note: written (%s): %s - %s", regenReason, s.Song, s.Artist)
			} else {
				report("note: written: %s - %s", s.Song, s.Artist)
			}
		case hasOld && !oldSourceOK:
			// the old note names facts that no longer hold and the rewrite
			// failed: drop it rather than publish a stale album claim
			delete(ctx.Notes, k)
			dropped++
			report("note: dropped stale note (regeneration failed: %s; old note no longer checks out): %s - %s", issue, s.Song, s.Artist)
		case hasOld:
			kept++
			report("note: kept existing note (regeneration failed: %s): %s - %s", issue, s.Song, s.Artist)
		default:
			missing++
			report("note: none written (%s): %s - %s", issue, s.Song, s.Artist)
		}
		time.Sleep(pace) // stay under the API rate limit
	}
	ctx.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	ctx.Model = g.Model()
	ctx.IncludeMemory = includeMemory
	if err := ctx.Save(contextPath); err != nil {
		fail(err)
	}
	// Clear release_changed markers for notes successfully regenerated this
	// run; the markers for still-failing songs stay set for the next run.
	if clearedMarkers > 0 && ef != nil {
		if err := ef.Save(enrichedPath); err != nil {
			fmt.Fprintln(os.Stderr, "warn: could not clear release markers:", err)
		}
	}
	report("notes: %d written, %d skipped (existing), %d retries, %d kept old after failed regeneration, %d existing removed, %d missing/no note written; %d source-check songs (limit %d) -> %s", added, skipped, retried, kept, dropped, missing, remoteSongs, notesBatch, contextPath)
	fmt.Println("Review the generated note diff and published facts. The supplied Action commits and deploys automatically; it has no human review gate.")
	flushSummary("Notes: per-song outcomes")
}

func recAlbum(r *enrich.Record) string {
	if r == nil {
		return ""
	}
	return r.Album
}

// Keep regeneration state across the enrich step that precedes notes.
func copyReleaseState(rec *enrich.Record, old enrich.Record) {
	rec.ReleaseChanged = old.ReleaseChanged
}

func noteAlbumIssue(text string, rec *enrich.Record) string {
	if rec == nil || rec.AlbumType == "single" {
		return ""
	}
	return notes.AlbumYearIssue(text, cleanAlbum(rec.Album), rec.ReleaseYear)
}

var notesBatch = 4

const sourceVersion = 2

func sourceStampValid(n notes.Note, song, artist string, now time.Time) bool {
	checked, err := time.Parse(time.RFC3339, n.SourceCheckedAt)
	return err == nil && n.SourceVersion == sourceVersion && n.SourceIdentity == notes.Key(song, artist)+"|"+n.SourceURL && !checked.After(now) && now.Sub(checked) < 30*24*time.Hour
}
func stampSource(n *notes.Note, song, artist string, now time.Time) {
	n.SourceCheckedAt = now.UTC().Format(time.RFC3339)
	n.SourceIdentity = notes.Key(song, artist) + "|" + n.SourceURL
	n.SourceVersion = sourceVersion
}
func notePriority(c *notes.Context, records map[string]*enrich.Record, s csvio.Song) int {
	k := notes.Key(s.Song, s.Artist)
	n, ok := c.Notes[k]
	if !ok {
		return 0
	}
	if n.PendingVerification {
		return 1
	}
	if r := records[k]; r != nil && (r.ReleaseChanged || noteAlbumIssue(n.Note, r) != "") {
		return 2
	}
	return 3
}
func recoverNotes(path, ref string) error {
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(ref) {
		return fmt.Errorf("recovery-ref must be an exact 40-character git commit")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(clean, "../") || filepath.IsAbs(path) {
		return fmt.Errorf("recovery context path must be repository-relative")
	}
	b, err := exec.Command("git", "show", ref+":"+clean).Output()
	if err != nil {
		return fmt.Errorf("cannot load pre-run notes from git: %w", err)
	}
	var prior notes.Context
	if err = json.Unmarshal(b, &prior); err != nil {
		return err
	}
	current, err := notes.Load(path)
	if err != nil {
		return err
	}
	count := notes.RestoreMissing(current, &prior, ref)
	if err = current.Save(path); err != nil {
		return err
	}
	fmt.Printf("Recovered %d missing prior notes; existing notes untouched; recovered text pending verification.\n", count)
	return nil
}

func setRecheckReason(rec *enrich.Record, result string) {
	switch result {
	case "confirmed":
		rec.MatchReason = "kept: pin confirmed by this run"
	case "no-exact":
		rec.MatchReason = "kept: no exact title+artist candidate in this run"
	case "no-better":
		rec.MatchReason = "kept: current search offered nothing better"
	}
}

// recordThrottle carries repeated throttles across separate workflow runs.
func recordThrottle(ctx *notes.Context, until, now time.Time) {
	ctx.ThrottleCount++
	backoff := 15 * time.Minute * time.Duration(1<<uint(min(ctx.ThrottleCount-1, 3)))
	if minimum := now.Add(backoff); until.Before(minimum) {
		until = minimum
	}
	ctx.RetryNotBefore = until.UTC().Format(time.RFC3339)
}

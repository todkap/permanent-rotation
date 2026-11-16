// Package enrich matches each CSV row to public music data and merges the
// results into enriched.json. Rules that always hold: never invent a match
// (unmatched songs are flagged and render without a Spotify link), manual
// overrides always win, and matching is deterministic so a song never
// silently changes identity.
package enrich

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/todkap/permanent-rotation/internal/csvio"
)

// Record is one enriched song. It mirrors the spec's enriched.json schema.
type Record struct {
	Song              string `json:"song"`
	Artist            string `json:"artist"`
	LifeYear          int    `json:"life_year"`
	Chapter           string `json:"chapter"`
	Memory            string `json:"memory,omitempty"`
	Tags              string `json:"tags,omitempty"`
	SpotifyID         string `json:"spotify_id,omitempty"`
	SpotifyURL        string `json:"spotify_url,omitempty"`
	ReleaseYear       int    `json:"release_year,omitempty"`
	Album             string `json:"album,omitempty"`
	AlbumType         string `json:"album_type,omitempty"`
	AlbumArt          string `json:"album_art,omitempty"`
	SeenLive          string `json:"seen_live,omitempty"`
	LastfmListeners   int64  `json:"lastfm_listeners"`
	LastfmPlaycount   int64  `json:"lastfm_playcount"`
	SpotifyPopularity *int   `json:"spotify_popularity"`        // nil for post-Feb-2026 Dev Mode apps
	Tier              string `json:"tier"`                      // canon | middle | deep-cut
	MatchConfidence   string `json:"match_confidence"`          // exact | fuzzy | manual
	MatchReason       string `json:"match_reason,omitempty"`    // why the pin is what it is
	ReleaseChanged    bool   `json:"release_changed,omitempty"` // pin moved; the notes run clears this after regenerating that song's note
	YearConflict      bool   `json:"year_conflict,omitempty"`
	FetchedAt         string `json:"fetched_at,omitempty"`
}

// File is the committed enriched.json document.
type File struct {
	GeneratedAt string   `json:"generated_at"`
	Songs       []Record `json:"songs"`
}

func LoadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &f, nil
}

func (f *File) Save(path string) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func key(song, artist string) string {
	return strings.ToLower(strings.TrimSpace(song)) + "|" + strings.ToLower(strings.TrimSpace(artist))
}

// Override pins a match by hand. Overrides always win. Album is optional:
// set it when the pinned ID belongs to a different album than the cached
// match (e.g. original album instead of a compilation).
type Override struct {
	SpotifyID   string
	ReleaseYear int
	Album       string
}

// LoadOverrides reads data/overrides.csv (song,artist,spotify_id,release_year[,album]).
func LoadOverrides(path string) (map[string]Override, error) {
	out := map[string]Override{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || i == 0 {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		var o Override
		o.SpotifyID = strings.TrimSpace(parts[2])
		if len(parts) > 3 {
			fmt.Sscanf(strings.TrimSpace(parts[3]), "%d", &o.ReleaseYear)
		}
		if len(parts) > 4 {
			o.Album = strings.TrimSpace(parts[4])
		}
		out[key(parts[0], parts[1])] = o
	}
	return out, nil
}

// AssignTiers splits records into canon / middle / deep-cut by playcount
// percentile within the list: top third canon, bottom third deep cut. The
// split is relative to the list, so it scales with any song count. Records
// with no metrics (zero playcount, e.g. unmatched songs) get no tier.
func AssignTiers(recs []Record) {
	type pc struct {
		i int
		n int64
	}
	var idx []pc
	for i, r := range recs {
		if r.LastfmPlaycount > 0 {
			idx = append(idx, pc{i, r.LastfmPlaycount})
		}
	}
	sort.Slice(idx, func(a, b int) bool { return idx[a].n > idx[b].n })
	n := len(idx)
	for rank, p := range idx {
		switch {
		case n == 0:
		case rank < n/3 || (n%3 != 0 && rank == n/3 && n < 3):
			recs[p.i].Tier = "canon"
		case rank >= 2*n/3:
			recs[p.i].Tier = "deep-cut"
		default:
			recs[p.i].Tier = "middle"
		}
	}
}

// Merge reconciles freshly fetched records against the cached file.
// Matching fields (spotify id, album, release year) come from the cache for
// unchanged rows; metrics are always fresh. The caller does the fetching;
// Merge only reconciles.
func Merge(songs []csvio.Song, fetched map[string]Record, cached *File, overrides map[string]Override, now time.Time) (*File, []string) {
	cachedByKey := map[string]Record{}
	for _, r := range cached.Songs {
		cachedByKey[key(r.Song, r.Artist)] = r
	}
	out := &File{GeneratedAt: now.UTC().Format(time.RFC3339)}
	var problems []string
	for _, s := range songs {
		k := key(s.Song, s.Artist)
		rec, ok := fetched[k]
		if !ok {
			if old, hit := cachedByKey[k]; hit && old.SpotifyID != "" {
				rec = old // keep a previously matched record rather than dropping it
			} else {
				// defensive: the caller always supplies a record, even
				// unmatched; if somehow there is none, list and skip
				problems = append(problems, fmt.Sprintf("no data: %s - %s", s.Song, s.Artist))
				continue
			}
		}
		rec.Song, rec.Artist = s.Song, s.Artist
		rec.LifeYear, rec.Chapter = s.LifeYear, s.Chapter
		rec.Memory, rec.Tags = s.Memory, s.Tags
		rec.SeenLive = s.SeenLive
		if o, hit := overrides[k]; hit {
			changed := false
			if o.SpotifyID != "" {
				changed = changed || rec.SpotifyID != o.SpotifyID
				if rec.SpotifyID != o.SpotifyID {
					rec.AlbumArt = ""
				}
				rec.SpotifyID = o.SpotifyID
				rec.SpotifyURL = "https://open.spotify.com/track/" + o.SpotifyID
				rec.MatchConfidence = "manual"
			}
			if o.ReleaseYear != 0 {
				changed = changed || rec.ReleaseYear != o.ReleaseYear
				rec.ReleaseYear = o.ReleaseYear
			}
			if o.Album != "" {
				changed = changed || !strings.EqualFold(rec.Album, o.Album)
				rec.Album = o.Album
			}
			rec.MatchReason = "manual override (data/overrides.csv)"
			if changed {
				rec.ReleaseChanged = true // note facts may name the old release
			}
		}
		if rec.MatchConfidence == "" {
			rec.MatchConfidence = "exact"
		}
		if rec.SpotifyID == "" {
			problems = append(problems, fmt.Sprintf("unmatched (renders without Spotify link): %s - %s", s.Song, s.Artist))
		}
		out.Songs = append(out.Songs, rec)
	}
	AssignTiers(out.Songs)
	return out, problems
}

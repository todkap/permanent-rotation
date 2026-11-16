// Package csvio reads a songs.csv file. It tolerates messy headers and
// values: columns are matched by alias first and by position as a fallback,
// so a personal spreadsheet works as-is.
package csvio

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DefaultMinSongs is the recommended minimum list size. Reaching past the
// obvious favorites is the point of the exercise. Override with --min.
const DefaultMinSongs = 25

// Song is one row of the input CSV. Memory is the author's own words and is
// rendered verbatim. Angle is an assistant drafting aid and is never
// published or fed into generated text. SeenLive is the author's own
// where/when note for shows they attended; also rendered verbatim.
type Song struct {
	Song     string
	Artist   string
	Era      string
	Chapter  string
	LifeYear int
	Tags     string
	Memory   string
	Angle    string
	SeenLive string
}

var (
	year4  = regexp.MustCompile(`\d{4}`)
	parens = regexp.MustCompile(`\(([^)]*)\)`)
	year2  = regexp.MustCompile(`~(\d{2})\b`)
)

// ParseEra splits an era string like "College Freshman Year (~1991)" into
// its chapter text and life year. Ranges ("~1994-Present") take the start
// year. A two-digit "~85" style year maps to 1985.
func ParseEra(era string) (chapter string, lifeYear int) {
	chapter = strings.TrimSpace(era)
	if i := strings.Index(chapter, "("); i >= 0 {
		chapter = strings.TrimSpace(chapter[:i])
	}
	// the year he wrote in the (~...) parens is the intended memory year;
	// digits in the label itself (e.g. "Early 2000s") are only a fallback
	target := era
	if m := parens.FindStringSubmatch(era); m != nil && year4.MatchString(m[1]) {
		target = m[1]
	}
	if m := year4.FindString(target); m != "" {
		lifeYear, _ = strconv.Atoi(m)
	} else if m := year2.FindStringSubmatch(era); m != nil {
		lifeYear, _ = strconv.Atoi("19" + m[1])
	}
	return chapter, lifeYear
}

// column aliases, matched lowercase as substrings of the header cell
var aliases = []struct {
	field  int
	needle []string
}{
	{0, []string{"song", "title"}},
	{1, []string{"artist", "band"}},
	{2, []string{"era"}},
	{3, []string{"tag", "blah"}},
	{4, []string{"memory", "source notes", "own words"}},
	{5, []string{"angle", "draft angle"}},
	{6, []string{"seen live", "seenlive", "live show"}},
}

func mapHeader(header []string) []int {
	idx := make([]int, 7)
	for i := range idx {
		idx[i] = -1
	}
	used := make([]bool, len(header))
	for _, a := range aliases {
		for c, cell := range header {
			if used[c] {
				continue
			}
			lc := strings.ToLower(cell)
			for _, n := range a.needle {
				if strings.Contains(lc, n) {
					idx[a.field] = c
					used[c] = true
					break
				}
			}
			if idx[a.field] >= 0 {
				break
			}
		}
	}
	// positional fallback for anything unresolved
	for f := range idx {
		if idx[f] < 0 && f < len(header) && !used[f] {
			idx[f] = f
			used[f] = true
		}
	}
	return idx
}

// Load reads path and returns the valid rows (song and artist non-empty).
// It fails when fewer than min valid rows are present.
func Load(path string, min int) ([]Song, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f, min)
}

// Read parses CSV from r. Lines starting with # are treated as comments.
func Read(r io.Reader, min int) ([]Song, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty csv")
	}
	idx := mapHeader(rows[0])
	var songs []Song
	for _, row := range rows[1:] {
		if len(row) > 0 && strings.HasPrefix(strings.TrimSpace(row[0]), "#") {
			continue
		}
		get := func(f int) string {
			if idx[f] >= 0 && idx[f] < len(row) {
				return strings.TrimSpace(row[idx[f]])
			}
			return ""
		}
		s := Song{
			Song:     get(0),
			Artist:   get(1),
			Era:      get(2),
			Tags:     strings.TrimSuffix(get(3), "/"),
			Memory:   get(4),
			Angle:    get(5),
			SeenLive: get(6),
		}
		if s.Song == "" || s.Artist == "" {
			continue
		}
		s.Chapter, s.LifeYear = ParseEra(s.Era)
		songs = append(songs, s)
	}
	if len(songs) < min {
		return nil, fmt.Errorf("found %d valid songs, need at least %d (override with --min)", len(songs), min)
	}
	return songs, nil
}

// Package notes is the opt-in background layer: short, sourced, model-written
// notes per song, kept strictly separate from the author's own words. Every
// note must cite a source URL or it is dropped. Output is reviewed before
// commit and rendered in a labeled "background" block on the site.
package notes

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Note is one song's background note. Album and ReleaseYear record the
// enriched facts the note was written against; when the pin moves, the notes
// run regenerates instead of publishing a note that names the old release.
// Legacy notes predate these fields and are handled by the release_changed
// marker on the enriched record.
type Note struct {
	Note        string `json:"note"`
	SourceURL   string `json:"source_url"`
	Album       string `json:"album,omitempty"`
	ReleaseYear int    `json:"release_year,omitempty"`
}

// Context is the committed context.json document.
type Context struct {
	GeneratedAt   string          `json:"generated_at"`
	Model         string          `json:"model"`
	IncludeMemory bool            `json:"include_memory"`
	Notes         map[string]Note `json:"notes"`
}

func Key(song, artist string) string {
	return strings.ToLower(strings.TrimSpace(song)) + "|" + strings.ToLower(strings.TrimSpace(artist))
}

// Migrate rekeys notes when a CSV title is corrected. It removes stale keys
// only after their note has landed under the new key, so renaming a song does
// not lose reviewed background text or force regeneration.
func Migrate(n map[string]Note, oldSong, artist, newSong string) {
	oldKey, newKey := Key(oldSong, artist), Key(newSong, artist)
	if oldKey == newKey {
		return
	}
	if note, ok := n[oldKey]; ok {
		if _, exists := n[newKey]; !exists {
			n[newKey] = note
		}
		delete(n, oldKey)
	}
}

func Load(path string) (*Context, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Context{Notes: map[string]Note{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Context
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if c.Notes == nil {
		c.Notes = map[string]Note{}
	}
	return &c, nil
}

func (c *Context) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Valid enforces the guardrail: a note without a real-looking source URL is
// dropped.
func Valid(n Note) bool {
	return strings.TrimSpace(n.Note) != "" && strings.HasPrefix(n.SourceURL, "http")
}

var (
	secondPerson   = regexp.MustCompile(`(?i)\b(you|your|yours|you're|you've|you'll)\b`)
	selfCorrection = regexp.MustCompile(`(?i)(wait\s*[,.…]|\.\.\.\s*wait|\bactually,|\boops\b)`)
)

// StyleOK enforces the voice contract: third person only (the person is
// "this listener", never "you"), and no self-corrections or hedged restarts
// left in the text. Notes failing this are retried once, then dropped.
func StyleOK(note string) bool {
	return !secondPerson.MatchString(note) && !selfCorrection.MatchString(note)
}

var (
	yearClaim   = regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	doubleQuote = regexp.MustCompile(`["\x{201c}](.*?)["\x{201d}]`)
	// A single-quoted title: opening quote not mid-word (that would be an
	// apostrophe), span starts uppercase, no apostrophes inside.
	singleQuote = regexp.MustCompile(`(?:^|[\s("\x{201c}])'([A-Z][^']{2,60}?)'(?:[\s,.!?:;)]|$)`)
	// An unquoted album name: the word "album" followed by title-cased words
	// and small connectors, stopped by punctuation or a lowercase word.
	albumClaim = regexp.MustCompile(`(?i:album)[,:]?\s+["'\x{201c}]?([A-Z][\w&'-]*(?:\s+(?:(?:of|the|a|an|and|in|on|de|la)\s+)*[A-Z][\w&'-]*)*)`)
)

// ClaimsOK enforces the grounding contract: every specific the note states
// must come from the sourced facts the model was given. Any 4-digit year and
// any quoted title (album, film, single) must literally appear in facts.
// Notes failing this are retried once, then dropped.
func ClaimsOK(note, facts string) bool { return ClaimIssue(note, facts) == "" }

// ClaimIssue explains the first unsupported claim for logs and corrective retries.
func ClaimIssue(note, facts string) string {
	low := strings.ToLower(facts)
	for _, y := range yearClaim.FindAllString(note, -1) {
		if !strings.Contains(facts, y) {
			return "unsupported year: " + y
		}
	}
	var spans []string
	for _, m := range doubleQuote.FindAllStringSubmatch(note, -1) {
		spans = append(spans, m[1])
	}
	for _, m := range singleQuote.FindAllStringSubmatch(note, -1) {
		spans = append(spans, m[1])
	}
	for _, m := range albumClaim.FindAllStringSubmatch(note, -1) {
		span := strings.TrimRight(m[1], " ")
		if strings.HasPrefix(strings.ToLower(span), "of the same") {
			continue // "album of the same name" is not a claim
		}
		spans = append(spans, span)
	}
	for _, q := range spans {
		if len(strings.TrimSpace(q)) < 4 {
			continue
		}
		if !strings.Contains(low, strings.ToLower(strings.TrimSpace(q))) {
			return "unsupported title/album: " + strings.TrimSpace(q)
		}
	}
	return ""
}

// StyleIssue ignores the actual song title/artist when checking narration.
func StyleIssue(note, song, artist string) string {
	for _, name := range []string{song, artist} {
		if strings.TrimSpace(name) != "" {
			note = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(name)).ReplaceAllString(note, "the track")
		}
	}
	if secondPerson.MatchString(note) {
		return "second-person narration"
	}
	if selfCorrection.MatchString(note) {
		return "self-correction/restart"
	}
	return ""
}

// AlbumYearIssue checks a relationship, not just a bag of allowed years.
// The year of a single must not be attached to the album it appeared on.
func AlbumYearIssue(note, album string, releaseYear int) string {
	if album == "" || releaseYear == 0 {
		return ""
	}
	title := regexp.QuoteMeta(album)
	patterns := []string{
		`(?i)(?:released|issued|appeared)\s+(?:in\s+)?((?:19|20)\d{2})\s+(?:on|from)\s+(?:the\s+)?(?:[^.!?;\n]{0,80}?\balbum\s+)?["']?` + title,
		`(?i)((?:19|20)\d{2})\s+(?:studio\s+)?album\s+["']?` + title,
		`(?i)album\s+["']?` + title + `["']?\s*\(\s*((?:19|20)\d{2})\s*\)`,
	}
	want := fmt.Sprint(releaseYear)
	for _, pattern := range patterns {
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(note, -1) {
			if m[1] != want {
				return "album-year mismatch: " + album + " is dated " + want + " in the selected release facts; do not attach single year " + m[1] + " to that album"
			}
		}
	}
	return ""
}

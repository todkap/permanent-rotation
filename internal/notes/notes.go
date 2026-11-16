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

// Note is one song's background note.
type Note struct {
	Note      string `json:"note"`
	SourceURL string `json:"source_url"`
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
	albumClaim = regexp.MustCompile(`(?i)\balbum[,:]?\s+["'\x{201c}]?((?:[A-Z0-9][\w&'.-]*|of|the|a|an|and|in|on|de|la)(?:\s+(?:[A-Z0-9][\w&'.-]*|of|the|a|an|and|in|on|de|la))*)`)
)

// ClaimsOK enforces the grounding contract: every specific the note states
// must come from the sourced facts the model was given. Any 4-digit year and
// any quoted title (album, film, single) must literally appear in facts.
// Notes failing this are retried once, then dropped.
func ClaimsOK(note, facts string) bool {
	low := strings.ToLower(facts)
	for _, y := range yearClaim.FindAllString(note, -1) {
		if !strings.Contains(facts, y) {
			return false
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
			return false
		}
	}
	return true
}

// Package notes is the opt-in background layer: short, sourced, model-written
// notes per song, kept strictly separate from the author's own words. Every
// note must cite a source URL or it is dropped. Output is reviewed before
// commit and rendered in a labeled "background" block on the site.
package notes

import (
	"encoding/json"
	"fmt"
	"os"
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

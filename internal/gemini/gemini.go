// Package gemini calls the Gemini API over plain HTTPS with an AI Studio
// key. Used only by the opt-in `songs notes` command. Free-tier keys are
// rate limited, so the caller paces requests.
package gemini

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultModel is the model notes are generated with when GEMINI_MODEL is
// not set. Google retires models, so the name is config, not code: set
// GEMINI_MODEL to override. The model used is recorded in context.json so
// readers can see what wrote the background layer.
const DefaultModel = "gemini-3.8-flash"

// apiBase is a var so tests can point at a local server
var apiBase = "https://generativelanguage.googleapis.com/v1beta/models/"

type Client struct {
	key, model string
	hc         *http.Client
}

// New builds a client. model falls back to DefaultModel when empty.
func New(key, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{key: key, model: model, hc: &http.Client{Timeout: 120 * time.Second}}
}

// Model returns the model name in use (for recording in context.json).
func (c *Client) Model() string { return c.model }

// Generate sends one prompt and returns the text of the first candidate.
// Newer models can be slow to first token, so timeouts and transient
// failures get up to two retries with backoff (5s, then 15s).
func (c *Client) Generate(prompt string) (string, error) {
	if c.key == "" {
		return "", fmt.Errorf("GEMINI_API_KEY is not set")
	}
	var err error
	var out string
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt*attempt) * 5 * time.Second
			var he httpError
			if errors.As(err, &he) && he.retryAfter > wait {
				wait = he.retryAfter // the API said how long; believe it
			}
			sleep(wait)
		}
		out, err = c.generateOnce(prompt)
		if err == nil || !retriable(err) {
			return out, err
		}
	}
	return "", err
}

// sleep is swappable in tests
var sleep = time.Sleep

type httpError struct {
	status     string
	retryAfter time.Duration
}

func (e httpError) Error() string { return "gemini: " + e.status }

func parseRetryAfter(h string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0
}

func retriable(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var he httpError
	if errors.As(err, &he) {
		return strings.HasPrefix(he.status, "429") || strings.HasPrefix(he.status, "5")
	}
	// timeouts surface as *url.Error wrapping a deadline
	var ue interface{ Timeout() bool }
	if errors.As(err, &ue) && ue.Timeout() {
		return true
	}
	return false
}

func (c *Client) generateOnce(prompt string) (string, error) {
	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"temperature":      0.3,
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", apiBase+c.model+":generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.key)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("%w: %s", httpError{resp.Status, parseRetryAfter(resp.Header.Get("Retry-After"))}, msg)
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}

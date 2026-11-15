// Package gemini calls the Gemini API over plain HTTPS with an AI Studio
// key. Used only by the opt-in `songs notes` command. Free-tier keys are
// rate limited, so the caller paces requests.
package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultModel is the model notes are generated with when GEMINI_MODEL is
// not set. Google retires models, so the name is config, not code: set
// GEMINI_MODEL to override. The model used is recorded in context.json so
// readers can see what wrote the background layer.
const DefaultModel = "gemini-3.8-flash"

const apiURL = "https://generativelanguage.googleapis.com/v1beta/models/"

type Client struct {
	key, model string
	hc         *http.Client
}

// New builds a client. model falls back to DefaultModel when empty.
func New(key, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{key: key, model: model, hc: &http.Client{Timeout: 30 * time.Second}}
}

// Model returns the model name in use (for recording in context.json).
func (c *Client) Model() string { return c.model }

// Generate sends one prompt and returns the text of the first candidate.
func (c *Client) Generate(prompt string) (string, error) {
	if c.key == "" {
		return "", fmt.Errorf("GEMINI_API_KEY is not set")
	}
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
	req, _ := http.NewRequest("POST", apiURL+c.model+":generateContent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.key)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("gemini: %s: %s", resp.Status, msg)
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

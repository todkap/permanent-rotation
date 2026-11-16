package gemini

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetriesTransientFailures(t *testing.T) {
	sleep = func(time.Duration) {} // no real backoff in tests
	defer func() { sleep = time.Sleep }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]string{{"text": `{"note":"ok","source_url":"https://x"}`}}}},
			},
		})
	}))
	defer srv.Close()

	c := New("k", "test-model")
	c.hc = srv.Client()
	// point the client at the test server
	orig := apiBase
	apiBase = srv.URL + "/"
	defer func() { apiBase = orig }()

	out, err := c.Generate("prompt")
	if err != nil {
		t.Fatalf("should have succeeded on third attempt: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("unexpected output: %q", out)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	sleep = func(time.Duration) {}
	defer func() { sleep = time.Sleep }()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(404)
	}))
	defer srv.Close()
	c := New("k", "test-model")
	c.hc = srv.Client()
	orig := apiBase
	apiBase = srv.URL + "/"
	defer func() { apiBase = orig }()
	if _, err := c.Generate("prompt"); err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("404 must not be retried, got %d calls", calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("17"); d != 17*time.Second {
		t.Fatalf("got %v", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Fatalf("empty should be 0, got %v", d)
	}
}

func TestRetriesOn429(t *testing.T) {
	sleep = func(time.Duration) {}
	defer func() { sleep = time.Sleep }()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(429)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]string{{"text": `{"note":"ok","source_url":"https://x"}`}}}},
			},
		})
	}))
	defer srv.Close()
	c := New("k", "test-model")
	c.hc = srv.Client()
	orig := apiBase
	apiBase = srv.URL + "/"
	defer func() { apiBase = orig }()
	if _, err := c.Generate("prompt"); err != nil {
		t.Fatalf("429 with Retry-After should be retried: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

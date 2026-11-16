package spotify

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(body string) *Client {
	return &Client{token: "test", tokenExp: time.Now().Add(time.Hour), hc: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
}
func TestSearchAlbumArt(t *testing.T) {
	c := fixtureClient(`{"tracks":{"items":[{"id":"pinned","name":"A","artists":[{"name":"B"}],"album":{"name":"Album","images":[{"url":"large"},{"url":"medium"},{"url":"small"}]}}]}}`)
	got, err := c.Search("A", "B")
	if err != nil || len(got) != 1 || got[0].AlbumArt != "medium" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestCoverArtByPinnedID(t *testing.T) {
	c := fixtureClient(`{"album":{"images":[{"url":"large"},{"url":"medium"}]}}`)
	art, err := c.CoverArt("pinned")
	if err != nil || art != "medium" {
		t.Fatalf("%q %v", art, err)
	}
	c = fixtureClient(`{"album":{"images":[]}}`)
	art, err = c.CoverArt("no-art")
	if err != nil || art != "" {
		t.Fatalf("%q %v", art, err)
	}
}

package notes

import "testing"

func TestStyleOK(t *testing.T) {
	bad := []string{
		"As you noted, its massive hooks made it stand out.",
		"During your time living in Boston, the song resonated.",
		"Introduced to you by your junior year college roommate.",
		"Released in 1983... wait, no. Released in 1993.",
		"It was actually, surprisingly, a hit.",
		"YOU might remember this one.",
	}
	for _, n := range bad {
		if StyleOK(n) {
			t.Fatalf("should reject: %q", n)
		}
	}
	good := []string{
		"For this listener, the track evokes dorm life.",
		"The song's youth-oriented marketing worked.", // "youth" is not "you"
		"Released in 1993, it bridged synth-pop and alternative rock.",
	}
	for _, n := range good {
		if !StyleOK(n) {
			t.Fatalf("should accept: %q", n)
		}
	}
}

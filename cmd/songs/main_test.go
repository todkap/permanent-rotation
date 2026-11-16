package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportAndFlushSummary(t *testing.T) {
	f := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", f)
	report("re-matched: A - B (Gold 2008 -> No Need To Argue 1994)")
	report("note: dropped (no source URL in generated note): C - D")
	flushSummary("Enrich: matching decisions")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "### Enrich: matching decisions") ||
		!strings.Contains(string(b), "- re-matched: A - B (Gold 2008 -> No Need To Argue 1994)") ||
		!strings.Contains(string(b), "- note: dropped (no source URL in generated note): C - D") {
		t.Fatalf("summary content: %q", b)
	}
	if len(reportLines) != 0 {
		t.Fatal("reportLines not reset after flush")
	}
}

func TestFlushSummaryNoEnvIsQuiet(t *testing.T) {
	report("matched: A - B -> X 1994 (exact)")
	flushSummary("Enrich: matching decisions") // no GITHUB_STEP_SUMMARY: must not fail
	if len(reportLines) != 1 {
		t.Fatal("lines should be kept when no summary file is set")
	}
	reportLines = nil
}

func TestSimilarTitles(t *testing.T) {
	yes := [][2]string{{"Would?", "Would"}, {"The Freshmen", "Freshman"}, {"There Is a Light That Never Goes Out", "There is a light that never goes out"}}
	for _, c := range yes {
		if !similarTitles(c[0], c[1]) {
			t.Fatalf("want similar: %q / %q", c[0], c[1])
		}
	}
	no := [][2]string{{"Zombie", "Linger"}, {"Would?", "Go"}, {"Basket Case", "Longview"}}
	for _, c := range no {
		if similarTitles(c[0], c[1]) {
			t.Fatalf("want distinct: %q / %q", c[0], c[1])
		}
	}
}

func TestFlushSummaryEscapesExternalText(t *testing.T) {
	f := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", f)
	report("card: A | B -> Violator [Deluxe] *1990* [kept]")
	flushSummary("Enrich: per-card final state")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "A | B") || strings.Contains(string(b), "*1990*") || strings.Contains(string(b), "[kept]") {
		t.Fatalf("unescaped external text in summary: %q", b)
	}
}

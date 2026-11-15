// Package site renders the static pages from enriched.json: a timeline by
// life chapter and a stats page. There is no ranking anywhere; songs are
// ordered by life chapter, then life year. Memory text is the author's own
// words and renders verbatim (HTML-escaped, never edited).
package site

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/todkap/permanent-rotation/internal/enrich"
)

const pageCSS = `
body{font-family:Georgia,serif;max-width:46em;margin:2em auto;padding:0 1em;line-height:1.55;color:#222}
h1,h2{font-family:-apple-system,Helvetica,Arial,sans-serif}
.song{border-left:3px solid #ddd;padding-left:1em;margin:1.5em 0}
.song h3{margin:0;font-family:-apple-system,Helvetica,Arial,sans-serif}
.meta{color:#666;font-size:.9em}
.memory{margin:.5em 0}
.tier{display:inline-block;font-size:.75em;padding:.1em .5em;border-radius:1em;background:#eee;color:#555}
.tier.canon{background:#fff3cd}.tier.deep-cut{background:#e2d9f3}
footer{margin-top:3em;color:#888;font-size:.85em;border-top:1px solid #eee;padding-top:1em}
a{color:#1DB954;text-decoration:none}a:hover{text-decoration:underline}
table{border-collapse:collapse}td,th{padding:.3em 1em .3em 0;text-align:left}
nav{font-family:-apple-system,Helvetica,Arial,sans-serif;font-size:.9em;margin-bottom:2em}
`

type chapter struct {
	Name  string
	Songs []enrich.Record
}

func groupByChapter(recs []enrich.Record) []chapter {
	var order []string
	byName := map[string][]enrich.Record{}
	yearOf := map[string]int{}
	for _, r := range recs {
		name := r.Chapter
		if name == "" {
			name = "Unknown era"
		}
		if _, ok := byName[name]; !ok {
			order = append(order, name)
			yearOf[name] = r.LifeYear
		}
		byName[name] = append(byName[name], r)
	}
	sort.SliceStable(order, func(i, j int) bool { return yearOf[order[i]] < yearOf[order[j]] })
	var out []chapter
	for _, name := range order {
		songs := byName[name]
		sort.SliceStable(songs, func(i, j int) bool { return songs[i].LifeYear < songs[j].LifeYear })
		out = append(out, chapter{Name: name, Songs: songs})
	}
	return out
}

var indexTpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Permanent Rotation</title><style>{{.CSS}}</style></head><body>
<nav><a href="index.html">Timeline</a> · <a href="stats.html">Stats</a></nav>
<h1>Permanent Rotation</h1>
<p class="meta">{{.Count}} songs, grouped by the chapter of life they arrived in. Not ranked.</p>
{{range .Chapters}}
<h2>{{.Name}}</h2>
{{range .Songs}}<div class="song">
<h3>{{.Song}} &middot; {{.Artist}}</h3>
<p class="meta">{{if .LifeYear}}entered my life ~{{.LifeYear}}{{end}}{{if .ReleaseYear}} &middot; released {{.ReleaseYear}}{{end}}{{if .Album}} &middot; <em>{{.Album}}</em>{{end}}
{{if .Tier}} &middot; <span class="tier {{.Tier}}">{{.Tier}}</span>{{end}}</p>
{{if .Memory}}<p class="memory">{{.Memory}}</p>{{end}}
{{if .SpotifyURL}}<p><a href="{{.SpotifyURL}}">Listen on Spotify</a></p>{{end}}
</div>{{end}}
{{end}}
<footer>Data as of {{if .GeneratedAt}}{{.GeneratedAt}}{{else}}not yet enriched{{end}} · Built with <a href="https://github.com/todkap/permanent-rotation">permanent-rotation</a></footer>
</body></html>`))

var statsTpl = template.Must(template.New("stats").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Permanent Rotation - Stats</title><style>{{.CSS}}</style></head><body>
<nav><a href="index.html">Timeline</a> · <a href="stats.html">Stats</a></nav>
<h1>Stats</h1>
<h2>Canon vs deep cuts</h2>
<table><tr><th>Tier</th><th>Songs</th></tr>
{{range .Tiers}}<tr><td>{{.Name}}</td><td>{{.Count}}</td></tr>{{end}}</table>
<h2>Songs by life decade</h2>
<table><tr><th>Decade</th><th>Songs</th></tr>
{{range .Decades}}<tr><td>{{.Name}}</td><td>{{.Count}}</td></tr>{{end}}</table>
{{if .AvgGap}}<h2>Release year vs life year</h2>
<p>On average a song entered this list {{printf "%.1f" .AvgGap}} years after its release.</p>{{end}}
<footer>Data as of {{if .GeneratedAt}}{{.GeneratedAt}}{{else}}not yet enriched{{end}} · Built with <a href="https://github.com/todkap/permanent-rotation">permanent-rotation</a></footer>
</body></html>`))

type kv struct {
	Name  string
	Count int
}

// Render writes index.html and stats.html into dir. An empty data file
// renders a valid placeholder site so forks can run this step with no keys.
func Render(f *enrich.File, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	chapters := groupByChapter(f.Songs)

	tierCount := map[string]int{"canon": 0, "middle": 0, "deep-cut": 0}
	decadeCount := map[int]int{}
	var gaps, gapSum int
	for _, r := range f.Songs {
		if r.Tier != "" {
			tierCount[r.Tier]++
		}
		if r.LifeYear > 0 {
			decadeCount[r.LifeYear/10*10]++
		}
		if r.LifeYear > 0 && r.ReleaseYear > 0 {
			gapSum += r.LifeYear - r.ReleaseYear
			gaps++
		}
	}
	var tiers []kv
	for _, t := range []string{"canon", "middle", "deep-cut"} {
		tiers = append(tiers, kv{strings.Title(t), tierCount[t]}) //nolint:staticcheck
	}
	var decades []kv
	var ds []int
	for d := range decadeCount {
		ds = append(ds, d)
	}
	sort.Ints(ds)
	for _, d := range ds {
		decades = append(decades, kv{fmt.Sprintf("%ds", d), decadeCount[d]})
	}
	var avgGap float64
	if gaps > 0 {
		avgGap = float64(gapSum) / float64(gaps)
	}

	idxData := map[string]any{"CSS": template.CSS(pageCSS), "Chapters": chapters, "Count": len(f.Songs), "GeneratedAt": f.GeneratedAt}
	statsData := map[string]any{"CSS": template.CSS(pageCSS), "Tiers": tiers, "Decades": decades, "AvgGap": avgGap, "GeneratedAt": f.GeneratedAt}

	pages := []struct {
		name string
		tpl  *template.Template
		data any
	}{{"index.html", indexTpl, idxData}, {"stats.html", statsTpl, statsData}}
	for _, p := range pages {
		w, err := os.Create(filepath.Join(dir, p.name))
		if err != nil {
			return err
		}
		if err := p.tpl.Execute(w, p.data); err != nil {
			w.Close()
			return err
		}
		w.Close()
	}
	return nil
}

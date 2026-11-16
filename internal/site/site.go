// Package site renders the static pages from enriched.json: a timeline by
// life chapter and a stats page. There is no ranking anywhere; songs are
// ordered by life chapter, then life year. Memory text is the author's own
// words and renders verbatim (HTML-escaped, never edited). Model-written
// background notes render in their own labeled block, never mixed in.
//
// The visual style matches https://todkap.github.io/ - same palette (Braves
// navy base, Carolina blue accent, sparing Braves red), system sans fonts,
// no frameworks.
package site

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"

	"github.com/todkap/permanent-rotation/internal/enrich"
	"github.com/todkap/permanent-rotation/internal/notes"
)

const pageCSS = `
:root{--navy:#0b1f3a;--navy-2:#10294b;--ink:#eaf1f8;--muted:#9db4cc;--carolina:#7bafd4;--carolina-bright:#a8d0ee;--red:#ce1141;--line:rgba(123,175,212,.22);--max:900px}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--navy);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;line-height:1.6;-webkit-font-smoothing:antialiased}
a{color:var(--carolina-bright);text-decoration:none}
a:hover{text-decoration:underline}
.wrap{max-width:var(--max);margin:0 auto;padding:0 24px}
nav{position:sticky;top:0;z-index:10;background:rgba(11,31,58,.92);backdrop-filter:blur(8px);border-bottom:1px solid var(--line)}
nav .wrap{display:flex;align-items:center;justify-content:space-between;height:64px}
.brand{font-weight:700;font-size:1.05rem;letter-spacing:.02em;color:var(--ink)}
.brand:hover{text-decoration:none;color:var(--carolina-bright)}
.brand .dot{color:var(--red)}
nav ul{display:flex;gap:28px;list-style:none}
nav ul a{color:var(--muted);font-size:.95rem}
nav ul a:hover,nav ul a.active{color:var(--carolina-bright);text-decoration:none}
.hero{padding:80px 0 64px;background:radial-gradient(900px 420px at 85% -10%,rgba(123,175,212,.16),transparent 60%),radial-gradient(700px 380px at 0% 110%,rgba(206,17,65,.07),transparent 55%),var(--navy);border-bottom:1px solid var(--line)}
.hero .kicker{color:var(--carolina);text-transform:uppercase;letter-spacing:.18em;font-size:.8rem;font-weight:600;margin-bottom:18px}
.hero h1{font-size:clamp(2.4rem,5vw,3.6rem);line-height:1.05;letter-spacing:-.02em;margin-bottom:20px}
.hero h1 .accent{color:var(--carolina-bright)}
.hero p.lede{color:var(--muted);font-size:1.1rem;max-width:56ch}
section{padding:56px 0;border-bottom:1px solid var(--line)}
section:last-of-type{border-bottom:none}
.chapter-head{margin:48px 0 8px}
.chapter-head:first-of-type{margin-top:0}
.chapter-head .eyebrow{color:var(--red);text-transform:uppercase;letter-spacing:.18em;font-size:.75rem;font-weight:700;margin-bottom:8px}
.chapter-head h2{font-size:clamp(1.5rem,3vw,2rem);letter-spacing:-.01em}
.song{display:block;padding:22px 4px;border-bottom:1px solid var(--line)}
.song .row{display:flex;gap:22px;align-items:baseline}
.song .when{flex:0 0 86px;color:var(--carolina);font-size:.82rem;font-weight:600;text-transform:uppercase;letter-spacing:.06em}
.song .body{flex:1}
.song h3{color:var(--ink);font-size:1.08rem;margin-bottom:4px}
.song .byline{color:var(--carolina);font-size:.9rem;margin-bottom:8px}
.song .memory{color:var(--muted);font-size:.95rem;margin:6px 0}
.song .links{margin-top:10px;font-size:.9rem;font-weight:600}
.tier{display:inline-block;font-size:.72rem;font-weight:700;letter-spacing:.08em;text-transform:uppercase;padding:.15em .6em;border-radius:1em;border:1px solid var(--carolina);color:var(--carolina);vertical-align:middle;margin-left:8px}
.tier.deep-cut{border-color:var(--muted);color:var(--muted)}
.tier.middle{border-color:var(--line);color:var(--muted)}
.background{margin-top:12px;background:var(--navy-2);border:1px solid var(--line);border-left:3px solid var(--carolina);border-radius:6px;padding:14px 16px;font-size:.9rem}
.background .label{color:var(--muted);text-transform:uppercase;letter-spacing:.14em;font-size:.68rem;font-weight:700;display:block;margin-bottom:6px}
.background p{color:var(--muted)}
.background .src{margin-top:8px;font-size:.85rem}
table{border-collapse:collapse;margin-top:16px}
th{color:var(--carolina);font-size:.78rem;font-weight:700;text-transform:uppercase;letter-spacing:.1em;text-align:left}
td,th{padding:.5em 2em .5em 0}
td{color:var(--ink);border-bottom:1px solid var(--line)}
.stat-block{margin-bottom:48px}
.stat-block p{color:var(--muted);margin-top:8px;max-width:60ch}
footer{padding:48px 0 56px;background:var(--navy-2);border-top:1px solid var(--line)}
footer .note{color:var(--muted);font-size:.85rem}
footer .note .dot{color:var(--red)}
@media (max-width:760px){
nav .wrap{height:auto;padding:12px 24px;flex-wrap:wrap;row-gap:8px}
nav ul{gap:16px;flex-wrap:wrap}
nav ul a{font-size:.88rem}
.hero{padding:56px 0 48px}
.song .row{flex-direction:column;gap:4px}
.song .when{flex:none}
}
`

const navHTML = `<nav><div class="wrap">
<a class="brand" href="index.html">Permanent Rotation<span class="dot">.</span></a>
<ul>
<li><a href="index.html"{{if .Timeline}} class="active"{{end}}>Timeline</a></li>
<li><a href="stats.html"{{if not .Timeline}} class="active"{{end}}>Stats</a></li>
<li><a href="https://github.com/todkap/permanent-rotation">Make your own</a></li>
</ul></div></nav>`

type chapter struct {
	Name  string
	Songs []songView
}

type songView struct {
	enrich.Record
	Note       *notes.Note
	DecadeYear string
}

func groupByChapter(recs []enrich.Record, ctx *notes.Context) []chapter {
	var order []string
	byName := map[string][]songView{}
	yearOf := map[string]int{}
	for _, r := range recs {
		name := r.Chapter
		if name == "" {
			name = "Unknown era"
		}
		sv := songView{Record: r, DecadeYear: fmt.Sprintf("~%d", r.LifeYear)}
		if r.LifeYear == 0 {
			sv.DecadeYear = ""
		}
		if ctx != nil {
			if n, ok := ctx.Notes[notes.Key(r.Song, r.Artist)]; ok && notes.Valid(n) {
				n := n
				sv.Note = &n
			}
		}
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		// chapters sort by their earliest known song year
		if r.LifeYear > 0 {
			if cur, ok := yearOf[name]; !ok || r.LifeYear < cur {
				yearOf[name] = r.LifeYear
			}
		}
		byName[name] = append(byName[name], sv)
	}
	sort.SliceStable(order, func(i, j int) bool {
		yi, iok := yearOf[order[i]]
		yj, jok := yearOf[order[j]]
		if !iok {
			return false // unknown year sinks to the end
		}
		if !jok {
			return true
		}
		if yi != yj {
			return yi < yj
		}
		return order[i] < order[j] // same year: alphabetical
	})
	var out []chapter
	for _, name := range order {
		songs := byName[name]
		sort.SliceStable(songs, func(i, j int) bool {
			a, b := songs[i].LifeYear, songs[j].LifeYear
			if a == 0 {
				return false // unknown year sinks within the chapter too
			}
			if b == 0 {
				return true
			}
			return a < b
		})
		out = append(out, chapter{Name: name, Songs: songs})
	}
	return out
}

var indexTpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Permanent Rotation</title>
<meta name="description" content="{{.OGDesc}}">
<meta property="og:type" content="website">
<meta property="og:site_name" content="Permanent Rotation">
<meta property="og:title" content="Permanent Rotation">
<meta property="og:description" content="{{.OGDesc}}">
{{if .Base}}<meta property="og:url" content="{{.Base}}/">
<meta property="og:image" content="{{.Base}}/og.png">{{end}}
<meta name="twitter:card" content="summary_large_image">
<link rel="icon" type="image/png" href="favicon.png">
<link rel="apple-touch-icon" href="apple-touch-icon.png">
<style>{{.CSS}}</style></head><body>
` + navHTML + `
<header class="hero"><div class="wrap">
<p class="kicker">A list, not a ranking</p>
<h1>Permanent <span class="accent">Rotation</span></h1>
<p class="lede">{{.Count}} songs grouped by the chapter of life they arrived in. The memories are mine; the background notes are AI-researched, with sources linked.</p>
</div></header>
<main><div class="wrap">
<section>
{{range .Chapters}}
<div class="chapter-head"><p class="eyebrow">Chapter</p><h2>{{.Name}}</h2></div>
{{range .Songs}}<div class="song"><div class="row">
<div class="when">{{.DecadeYear}}</div>
<div class="body">
<h3>{{.Song}}{{if .Tier}}<span class="tier {{.Tier}}">{{.Tier}}</span>{{end}}</h3>
<p class="byline">{{.Artist}}{{if .Album}} &middot; <em>{{.Album}}</em>{{end}}{{if .ReleaseYear}} &middot; {{.ReleaseYear}}{{end}}</p>
{{if .Memory}}<p class="memory">{{.Memory}}</p>{{end}}
{{if .Note}}<div class="background"><span class="label">Background &middot; AI-researched &middot; source linked</span><p>{{.Note.Note}}</p><p class="src"><a href="{{.Note.SourceURL}}">Source</a></p></div>{{end}}
{{if .SpotifyURL}}<p class="links"><a href="{{.SpotifyURL}}">Listen on Spotify</a></p>{{end}}
</div></div></div>{{end}}
{{end}}
</section>
</div></main>
<footer><div class="wrap"><p class="note">Data as of {{if .GeneratedAt}}{{.GeneratedAt}}{{else}}not yet enriched{{end}} &middot; Built with <a href="https://github.com/todkap/permanent-rotation">permanent-rotation</a> by <a href="https://todkap.github.io/">Todd Kaplinger</a><span class="dot">.</span></p></div></footer>
</body></html>`))

var statsTpl = template.Must(template.New("stats").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Stats - Permanent Rotation</title>
<meta name="description" content="{{.OGDesc}}">
<meta property="og:type" content="website">
<meta property="og:site_name" content="Permanent Rotation">
<meta property="og:title" content="Stats - Permanent Rotation">
<meta property="og:description" content="{{.OGDesc}}">
{{if .Base}}<meta property="og:url" content="{{.Base}}/stats.html">
<meta property="og:image" content="{{.Base}}/og.png">{{end}}
<meta name="twitter:card" content="summary_large_image">
<link rel="icon" type="image/png" href="favicon.png">
<link rel="apple-touch-icon" href="apple-touch-icon.png">
<style>{{.CSS}}</style></head><body>
` + navHTML + `
<header class="hero"><div class="wrap">
<p class="kicker">The numbers</p>
<h1>Stats<span class="accent">.</span></h1>
<p class="lede">Canon vs deep cuts from Last.fm all-time play counts, split into thirds within this list. Today's popularity, not history's verdict.</p>
</div></header>
<main><div class="wrap">
<section>
<div class="stat-block">
<div class="chapter-head"><p class="eyebrow">Tiers</p><h2>Canon vs deep cuts</h2></div>
<table><tr><th>Tier</th><th>Songs</th></tr>
{{range .Tiers}}<tr><td>{{.Name}}</td><td>{{.Count}}</td></tr>{{end}}</table>
</div>
<div class="stat-block">
<div class="chapter-head"><p class="eyebrow">Timeline</p><h2>Songs by life decade</h2></div>
<table><tr><th>Decade</th><th>Songs</th></tr>
{{range .Decades}}<tr><td>{{.Name}}</td><td>{{.Count}}</td></tr>{{end}}</table>
</div>
{{if .AvgGap}}<div class="stat-block">
<div class="chapter-head"><p class="eyebrow">Lag</p><h2>Release year vs life year</h2></div>
<p>On average a song entered this list {{printf "%.1f" .AvgGap}} years after its release.</p>
</div>{{end}}
</section>
</div></main>
<footer><div class="wrap"><p class="note">Data as of {{if .GeneratedAt}}{{.GeneratedAt}}{{else}}not yet enriched{{end}} &middot; Built with <a href="https://github.com/todkap/permanent-rotation">permanent-rotation</a> by <a href="https://todkap.github.io/">Todd Kaplinger</a><span class="dot">.</span></p></div></footer>
</body></html>`))

type kv struct {
	Name  string
	Count int
}

var tierLabel = map[string]string{"canon": "Canon", "middle": "Middle", "deep-cut": "Deep cut"}

// Render writes index.html and stats.html into dir. An empty data file
// renders a valid placeholder site so forks can run this step with no keys.
// base is the absolute published URL (for og:image and link previews);
// leave it empty for a relative-only build. Files in an assets/ directory
// next to the repo root (og.png, favicon.png, ...) are copied into dir.
func Render(f *enrich.File, ctx *notes.Context, dir, base string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	copyAssets("assets", dir)
	chapters := groupByChapter(f.Songs, ctx)

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
		tiers = append(tiers, kv{tierLabel[t], tierCount[t]})
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

	ogDesc := fmt.Sprintf("%d songs grouped by the chapter of life they arrived in. The memories are mine; the background notes are AI-researched, with sources linked.", len(f.Songs))
	baseData := map[string]any{"CSS": template.CSS(pageCSS), "OGDesc": ogDesc, "Base": base} //nolint:gosec
	pages := []struct {
		name string
		tpl  *template.Template
		data map[string]any
	}{
		{"index.html", indexTpl, merge(baseData, map[string]any{"Timeline": true, "Chapters": chapters, "Count": len(f.Songs), "GeneratedAt": f.GeneratedAt})},
		{"stats.html", statsTpl, merge(baseData, map[string]any{"Timeline": false, "Tiers": tiers, "Decades": decades, "AvgGap": avgGap, "GeneratedAt": f.GeneratedAt})},
	}
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

// copyAssets copies any committed assets (og image, favicons) into the
// output directory. Missing assets dir is fine - forks may not have any.
func copyAssets(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}
		os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644)
	}
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

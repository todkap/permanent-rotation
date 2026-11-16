# permanent-rotation

Make your own songs page and playlist from a spreadsheet of the songs that stayed with you.

A small Go tool plus a GitHub Action. It reads your songs CSV, enriches each song with public music data (Spotify links, Last.fm listeners and play counts), and builds a static site. An optional local macOS script creates a Spotify playlist after your browser sign-in and explicit confirmation. Your words are the source of truth; the APIs only add labeled context.

**Sample:** [Todd's list](examples/todd/) - an example, made with this tool. Live site: https://todkap.github.io/permanent-rotation

[Listen to Todd's public Spotify playlist](https://open.spotify.com/playlist/3Mx9a5o9ZQW5v6nqoKUllO) - 49 songs in displayed chapter order. Deep Cover is omitted because it has no Spotify match.

## Make your own

1. **Fork this repo.** Work in your fork, not the sample owner's repository. A public fork makes your CSV and repository files readable by anyone. Do not put private memories or credentials in files you plan to commit.
2. **Replace `songs.csv` with your list.** Use `songs.template.csv` for the six columns: `song, artist, era, tags, memory, angle`. Only `song` and `artist` are required. `era` accepts text such as `College Freshman Year (~1991)`; the year is when the song entered your life. `memory` is your own writing and is rendered verbatim. `angle` is a drafting aid not rendered on the site, but remains visible in a public CSV/repo.
3. **Pick at least 25 songs.** The site has no fixed maximum. The template's three rows can be tested locally with `--min 3`; the supplied Action uses the 25-song minimum. The optional one-shot playlist script supports at most 100 tracks.
4. **Point the Action at your files.** Edit the `env:` section of `.github/workflows/build.yml`:

   ```yaml
   CSV_PATH: songs.csv
   OUT_PATH: data/enriched.json
   CONTEXT_PATH: data/context.json
   SITE_DIR: site
   SITE_BASE: https://YOURNAME.github.io/YOURREPO
   ```

   Set all three data paths, including `CONTEXT_PATH`, so your site does not reuse Todd's background notes. Start your own `data/context.json` as `{"notes":{}}` if you do not have notes yet. Start your own enriched output rather than copying Todd's song records. Replace the artwork in `assets/` (`og.png`, `favicon.png`, `apple-touch-icon.png`) with your own; assets are copied into the rendered site.
5. **Set your chapter order.** Add `data/chapter_order.txt`, beside your configured `OUT_PATH`, using [the sample](examples/todd/chapter_order.txt) as a format guide. One chapter per line, matching the CSV era text before `(~year)`. `## Era name` starts a larger era heading. Unlisted chapters use the fallback order; omit the file if you want the fallback throughout. It is read relative to the enriched JSON, not from the repo root.
6. **Personalize the generated pages.** In `internal/site/site.go`, replace the two footer author links/names (Timeline and Stats), and review the hero copy and navigation. Keep project credit if you like, but do not present Todd as the author of your memories. Changing generated HTML directly will be undone on the next build. Todd's playlist navigation link is shown only for Todd's published `SITE_BASE`; add your own link after creating your playlist.
7. **Enable Actions and Pages in the fork.** Open the Actions tab and enable workflows if GitHub asks. Under Settings > Actions > General, allow the workflow/actions needed by this repo if they are restricted. Under Settings > Pages > Build and deployment, choose **GitHub Actions** as the source, not a branch. The supplied workflow requests contents, Pages and OIDC permissions; organization policy can still restrict them. See [GitHub's Pages instructions](https://docs.github.com/en/pages/getting-started-with-github-pages/configuring-a-publishing-source-for-your-github-pages-site).
8. **Add your own keys** as repository secrets (below). Secrets do not transfer with a fork. The local playlist script uses your Client ID only; do not give it your Client Secret.
9. **Run and inspect the build.** Actions > build > Run workflow, with notes unchecked for the first run. The Action enriches, commits changed data and deploys the site automatically. Open the Pages deployment URL, verify your songs/art/chapters, then optionally run again with notes enabled. There is no built-in pause for human note approval before publishing. To review before publication, generate notes locally or add a branch/PR review gate to your own workflow first.
10. **Optional: create your playlist locally.** Follow [scripts/README.md](scripts/README.md), using `--site` with your own live index URL and `--name` with your own title. The Mac script opens Spotify authorization, shows the intended account/name/visibility/tracks and requires typed `confirm`. It defaults to PUBLIC unless you use `--private`. It is not the unimplemented Go `songs playlist` command.

After editing only chapter order, notes JSON, artwork or documentation, run the workflow manually: those paths are not all included in its automatic push triggers. Inspect generated diffs and the published pages; a green workflow is not a fact-check.

## Getting your keys

The site integrations use the keys below. Spotify requires Premium for the developer app; other providers have their own usage limits and paid tiers. Never paste secrets into chat, code, the CSV or a commit. Store site-build secrets only in GitHub repository secrets. Site screens change, so if a label differs, follow the linked docs.

### A. Spotify Client ID and Client Secret (needs a Spotify Premium account)

1. Sign in at https://developer.spotify.com/dashboard with your Premium account and accept the developer terms if asked.
2. Click **Create app**. Enter an app name (for example `permanent-rotation`) and a short description.
3. In **Redirect URIs** enter `http://127.0.0.1:8888/callback` (used only by the optional local `scripts/make-playlist.sh` script). Use `127.0.0.1`, not `localhost`.
4. Tick the Web API option, accept the terms, and click **Save**.
5. Open the app, then **Settings**. Copy the **Client ID**, then click **View client secret** and copy it.
6. Know the limits: new apps run in Development Mode (one Client ID per developer, up to 5 authorized users, a reduced endpoint set, no `popularity` field). That is enough for a personal tool.

Docs: https://developer.spotify.com/documentation/web-api/concepts/apps and https://developer.spotify.com/documentation/web-api/tutorials/client-credentials-flow

### B. Last.fm API key

1. Sign in, or create a free account, at https://www.last.fm.
2. Go to https://www.last.fm/api/account/create (or open https://www.last.fm/api and choose **Get an API account**).
3. Fill in a contact email, application name (`permanent-rotation`) and a one-line description. Callback URL can stay blank. Submit.
4. Copy the **API key** shown on the confirmation page. The shared secret is not needed.
5. Use is for personal, non-commercial projects. Commercial or research use needs permission from partners@last.fm.

Docs: https://www.last.fm/api

### C. Gemini API key (only for the optional `songs notes` step)

1. Sign in with a Google account at https://aistudio.google.com/apikey.
2. Click **Create API key**, name it, and attach it to a new or existing Google Cloud project when prompted.
3. Copy the key (typically starts with `AIza`).
4. The free tier has rate limits and its own data-use terms. Read them before sending any of your own writing to the API.
5. The model name is config, not code: it defaults to `gemini-3.8-flash`. To override it for Action runs, add a `GEMINI_MODEL` repository variable under **Settings, Secrets and variables, Actions, Variables tab** (leave it unset to keep the default). For local runs, export `GEMINI_MODEL` in your shell.

Docs: https://aistudio.google.com/docs/api-key

### D. Put them in GitHub as secrets

1. In the repo, go to **Settings, Secrets and variables, Actions, New repository secret**.
2. Create `SPOTIFY_CLIENT_ID`, `SPOTIFY_CLIENT_SECRET`, `LASTFM_API_KEY` and optionally `GEMINI_API_KEY`, one per secret, pasting each value.
3. The local playlist script uses only your Spotify Client ID, entered at its prompt or via `SPOTIFY_CLIENT_ID`. It does not use your Client Secret, Last.fm key or Gemini key. Never put credentials in tracked files.
4. If a key leaks, rotate it at the provider (Spotify has a **Rotate** button on the app page) and update the secret.

## How it works

```
songs enrich   # match songs.csv against Spotify + Last.fm, write enriched.json
songs site     # render the static site from enriched.json
songs notes    # opt-in Gemini background notes (review the output yourself)
```

- **Existing matches are cached, then checked.** With Spotify credentials, every automatic pin is searched again each build for an exact-title/artist candidate that is earlier or the same year in plainer packaging. A better candidate can replace a pin; release changes are recorded for note refresh. Manual pins/overrides are retained, not automatically rematched. New or unmatched rows are searched too. Inspect logs and generated diffs: fuzzy matches are reported, not paused for interactive approval. A song without a match still renders without a Spotify link. `data/overrides.csv` is an explicit manual escape hatch, not a required editing step.
- **Metrics are fresh every build.** With a Last.fm key, listener/play counts are requested on each run, and the canon / middle / deep-cut tiers are recomputed from them (top third vs bottom third of your list, so the split scales with any song count). The Action commits `enriched.json` only when it changed; the diff is your audit trail, and the page footer shows "data as of".
- **Rendering needs no API keys.** `songs site` can render committed enriched JSON without credentials. The Action still runs enrichment, but API calls require their corresponding keys; it does not magically replace Todd's sample with your own matched data. Configure your paths and inspect the output before publishing.
- **Background notes are opt-in and labeled.** Notes are paced at one request every 5 seconds by default (the free tier allows 15/minute, and 429 responses with a `Retry-After` header are honored). On a paid tier, speed it up with `--pace 1`. Run the Action manually with "Regenerate Gemini background notes" checked to (re)write `context.json` via Gemini. The supplied Action commits and deploys the resulting notes in the same run; there is no human approval gate. Inspect the generated diff and facts yourself, or change your workflow to require review before deployment. Notes appear in their own labeled block, never mixed into your words. The validator requires a source URL, but a link does not by itself prove that every claim is correct. Your memory text is included in the prompt by default (`-include-memory=false` opts out).
- **Playlist creation is optional and local, not in the Action.** [scripts/make-playlist.sh](scripts/make-playlist.sh) is a self-contained macOS bash launcher with embedded Python 3. It reads your generated site in displayed chapter order, shows a preview, signs in with PKCE and requires `confirm` before creating. It keeps only non-secret recovery state, not tokens. An ambiguous create response stops rather than risking a second playlist. See [setup, privacy and retry instructions](scripts/README.md). The Go `songs playlist` subcommand remains an unimplemented placeholder; do not use it.

## Known limits

- Spotify Development Mode rules (Feb 2026): Premium required, one Client ID, 5 authorized users, search capped at 10 results, and no `popularity` field - which is why canon-vs-deep-cut uses Last.fm instead.
- Both measures are today's popularity. A track obscure in 1987 that everyone knows now reads as canon.
- Remasters can report a later release year; the MusicBrainz cross-check flags disagreements instead of resolving them silently.
- Secrets are not available to pull requests from forks (the right default). Only the owner triggers enrichment.

## Requirements

Go 1.22+ for local Go commands. No Go dependencies beyond the standard library. The optional local playlist script requires macOS and Python 3.8+ (not guaranteed to be preinstalled); it needs no pip packages.

## License

MIT. Your list, your words, your data.

### Local playlist: Python certificate help

If Python was installed from python.org and reports `CERTIFICATE_VERIFY_FAILED`, run **Applications > Python 3.x > Install Certificates.command** and retry. Homebrew Python normally needs no separate certificate-install step. Check the active install with `python3 -c "import sys; print(sys.executable)"`: Python.org paths usually contain `/Library/Frameworks/Python.framework/`; Homebrew paths usually start `/opt/homebrew/` or `/usr/local/Cellar/`. For other paths or persistent errors, see [the troubleshooting guide](scripts/README.md#python-https-troubleshooting-pythonorg-vs-homebrew). Never disable HTTPS checks.

The sample site shows Todd's Spotify link only when built with Todd's SITE_BASE. Forks do not inherit that playlist link on their own published URL; add your own once created.

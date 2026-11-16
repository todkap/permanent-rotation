# permanent-rotation

Make your own songs page and playlist from a spreadsheet of the songs that stayed with you.

A small Go tool plus a GitHub Action. It reads your songs CSV, enriches each song with public music data (Spotify links, Last.fm listeners and play counts), and builds a static site and a Spotify playlist. Your words are the source of truth; the APIs only add labeled context.

**Sample:** [Todd's list](examples/todd/) - an example, made with this tool. Live site: https://todkap.github.io/permanent-rotation

## Make your own

1. **Fork this repo.**
2. **Replace `songs.csv` with your list.** A template (`songs.template.csv`) shows the six columns: `song, artist, era, tags, memory, angle`. Only `song` and `artist` are required. `era` accepts anything like `College Freshman Year (~1991)`; the year in parentheses is when the song entered your life. `memory` is your own words and is published verbatim. `angle` is a private drafting aid and is never published.
3. **Pick at least 25 songs. No maximum.** The number is the constraint that makes you dig past the obvious ones. Go as high as you like. (The template ships with 3 example rows; try it with `--min 3`.)
4. **Point the Action at your list:** in `.github/workflows/build.yml`, set `CSV_PATH` to `songs.csv` and `OUT_PATH` to `data/enriched.json`.
5. **Get your keys** (below) and add them as GitHub secrets.
6. **Run the Action** (Actions tab, "build", Run workflow). It enriches your list, commits `enriched.json`, and deploys the site to GitHub Pages.
7. **Optional:** run `songs playlist` locally to create your own Spotify playlist.

## Getting your keys

Three keys, all free. Never paste them into chat, code, the CSV or a commit. They go only into GitHub repository secrets. Site screens change, so if a label differs, follow the linked docs.

### A. Spotify Client ID and Client Secret (needs a Spotify Premium account)

1. Sign in at https://developer.spotify.com/dashboard with your Premium account and accept the developer terms if asked.
2. Click **Create app**. Enter an app name (for example `permanent-rotation`) and a short description.
3. In **Redirect URIs** enter `http://127.0.0.1:8888/callback` (used only by the local `songs playlist` command). Use `127.0.0.1`, not `localhost`.
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
3. For local runs (`songs playlist`), export the same names as environment variables in your shell. Do not write them to a file that git tracks.
4. If a key leaks, rotate it at the provider (Spotify has a **Rotate** button on the app page) and update the secret.

## How it works

```
songs enrich   # match songs.csv against Spotify + Last.fm, write enriched.json
songs site     # render the static site from enriched.json
songs playlist # create/update your Spotify playlist (local run, your login)
songs notes    # opt-in Gemini background notes (reviewed before commit)
```

- **Matching is deterministic and cached.** Only new or changed rows search Spotify. Anything fuzzy is printed for you to confirm; pin a match by hand in `data/overrides.csv`. Overrides always win. A song that cannot be matched is listed and skipped - never invented.
- **Metrics are fresh every build.** Last.fm listeners and play counts are refetched on every run, and the canon / middle / deep-cut tiers are recomputed from them (top third vs bottom third of your list, so the split scales with any song count). The Action commits `enriched.json` only when it changed; the diff is your audit trail, and the page footer shows "data as of".
- **Forks without keys still work.** `songs site` renders from the committed `enriched.json`; a fork's Action simply skips enrichment until its owner adds keys.
- **Background notes are opt-in and labeled.** Notes are paced at one request every 5 seconds by default (the free tier allows 15/minute, and 429 responses with a `Retry-After` header are honored). On a paid tier, speed it up with `--pace 1`. Run the Action manually with "Regenerate Gemini background notes" checked to (re)write `context.json` via Gemini. The diff is your review gate - the site only renders what you commit. Notes appear in their own labeled block, never mixed into your words. Every note must carry a source URL or it is dropped. Your memory text is included in the prompt by default (`-include-memory=false` opts out).
- **Playlist runs locally, not in the Action.** Creating a playlist needs your Spotify user login (OAuth), and storing a refresh token in Actions is more risk than a one-time playlist is worth. Order follows your life chapters - no ranking.

## Known limits

- Spotify Development Mode rules (Feb 2026): Premium required, one Client ID, 5 authorized users, search capped at 10 results, and no `popularity` field - which is why canon-vs-deep-cut uses Last.fm instead.
- Both measures are today's popularity. A track obscure in 1987 that everyone knows now reads as canon.
- Remasters can report a later release year; the MusicBrainz cross-check flags disagreements instead of resolving them silently.
- Secrets are not available to pull requests from forks (the right default). Only the owner triggers enrichment.

## Requirements

Go 1.22+ for local runs. No dependencies beyond the standard library.

## License

MIT. Your list, your words, your data.

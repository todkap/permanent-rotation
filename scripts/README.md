# Make a Spotify playlist on your Mac

Create a playlist from your Permanent Rotation timeline, from the tracks linked on your site. Browser sign-in uses your **Client ID, not your Client Secret**. Nothing is created until you type `confirm`.

[Live sample site](https://todkap.github.io/permanent-rotation/index.html) · [Public Spotify playlist](https://open.spotify.com/playlist/3Mx9a5o9ZQW5v6nqoKUllO) · [The story behind it](https://www.linkedin.com/pulse/permanent-rotation-agentic-project-end-year-todd-kaplinger-nky5c/) · [Todd's sample data](examples/todd/) · [Local playlist guide](scripts/README.md)


## On this page

- [Requirements](#requirements)
- [1. Check Python](#1-check-python)
- [2. Add the callback once](#2-add-the-callback-once)
- [3. Preview without changing Spotify](#3-preview-without-changing-spotify)
- [4. Create once](#4-create-once)
- [Safe retries](#safe-retries)
- [Use your own fork](#use-your-own-fork)
- [Troubleshooting](#troubleshooting)
- [What has been checked](#what-has-been-checked)

## Requirements

| You need | Notes |
| --- | --- |
| macOS, internet and a browser | The script uses macOS's built-in `open` command. |
| Python 3.8+ | Not guaranteed to be installed on every Mac. Standard library only; no pip packages. |
| A Spotify developer app and its Client ID | The app owner needs Premium. Development Mode permits up to five authorized users. Forkers need their own app. |

No Homebrew, git or curl setup is required. Script version: **v4**.

## 1. Check Python

Press Command-Space, type Terminal, press Return. Copy this and press Return:

```bash
python3 --version
```

Continue if it prints Python 3.8 or newer. If macOS offers Command Line Tools, cancel unless you want that installation. If Python is missing, use [the Python.org Mac installer](https://www.python.org/downloads/macos/) or ask for help; do not keep running commands hoping they will fix it.

> [!IMPORTANT]
> If you install Python from python.org, run **Applications > Python 3.x > Install Certificates.command** before using HTTPS. Homebrew Python normally needs no separate certificate-install step. [Check your installation](#python-certificate-errors) if you are unsure. Never disable HTTPS verification.

## 2. Add the callback once

1. Open the [Spotify developer dashboard](https://developer.spotify.com/dashboard) and select your app.
2. Open Settings and edit Redirect URIs.
3. Add this exact value, then save:

   ```text
   http://127.0.0.1:8888/callback
   ```

4. Copy the app's Client ID. Do not copy its Client Secret.

The callback is your Mac's loopback address. Keep `127.0.0.1`, not `localhost`. Do not paste a token or secret into the callback setting.

## 3. Preview without changing Spotify

From a downloaded repository, open Terminal in the repository folder and run:

```bash
bash scripts/make-playlist.sh --dry-run
```

For the standalone download, unzip it in Downloads. Type `bash ` (including the space), drag `make-playlist.sh` from Finder into Terminal, type a space and `--dry-run`, then press Return. Dragging the file fills in its path safely.

The preview shows the exact name, visibility, description, artist/album labels and Spotify IDs. The default name is read from the current article title:

> Permanent Rotation: an agentic project for the end of the year

The default visibility is **PUBLIC**. Todd's sample has 49 tracks from 50 cards; Deep Cover is omitted because it has no Spotify match. No substitutions. The source is live, so review the list you see.

> [!NOTE]
> Dry-run does not ask for credentials, save recovery state or contact Spotify. A web-uploaded script may not have executable permission; using `bash` as above avoids that issue. Alternatively run `chmod +x scripts/make-playlist.sh` before using `./scripts/make-playlist.sh`.

## 4. Create once

From the repository folder:

```bash
bash scripts/make-playlist.sh
```

For the standalone download, repeat the drag-and-drop command without `--dry-run`.

1. Review the preview and enter your Client ID at the Terminal prompt.
2. Sign into the intended Spotify account in the browser and approve playlist permissions. Return to Terminal.
3. Check the account, name, PUBLIC/private setting, description, recovery label and track list.
4. Type the single word `confirm` only if all are right. Any other answer cancels.

The script prints the playlist URL after checking ownership and the track count and IDs. Open the link to check playback before sharing it. Availability can vary by region/account even when the IDs match.

### Credentials and local state

| Stays in memory only | Saved locally, without secrets |
| --- | --- |
| PKCE verifier and short-lived access token | Approved manifest, playlist ID/URL, owner ID and progress |
| No Client Secret needed; no refresh token kept | Non-secret recovery label appended to the playlist description |

The optional `SPOTIFY_CLIENT_ID` environment variable can supply the non-secret Client ID; the prompt is simpler. Never run with `bash -x`, a debugger or terminal session recording.

## Safe retries

Run the same script again. You will sign in again because no login is saved. Keep this non-secret recovery file, even after success:

```text
~/Library/Application Support/Permanent Rotation/playlist-state.json
```

| Situation | What the script does |
| --- | --- |
| Completed run | Verifies the same playlist, not a new one. Later Spotify edits are not overwritten. |
| Interrupted track write | Uses an exact-set operation on the recorded new playlist, so retry cannot append duplicates. Unexpected contents stop the run. |
| Lost creation response | Marks the attempt before sending and refuses another create. Check Spotify for the printed `[pr-...]` label and ask for help recovering the playlist ID. |
| Crash leaves `playlist-state.lock` | Confirm no copy is running, then remove only the empty `.lock` folder. Keep `playlist-state.json`. Ask for help if unsure. |

> [!WARNING]
> Do not delete the state file to retry. A remote create and a local receipt cannot be saved atomically; stopping on an ambiguous result is the safe fallback. This is a one-time creation tool, not ongoing sync.

## Use your own fork

Use your own Spotify app, generated site and playlist name:

```bash
bash scripts/make-playlist.sh \
  --site https://YOURNAME.github.io/YOURREPO/index.html \
  --name "Your playlist name" \
  --private
```

Omit `--private` only if you want PUBLIC visibility. Preview and confirmation still apply. The script reads generated `.body` cards, not arbitrary playlists. It supports up to 100 tracks with no duplicate IDs. A state file from another site is never overwritten; keep each completed receipt intact.

## Troubleshooting

### Python certificate errors

For `SSL: CERTIFICATE_VERIFY_FAILED`, check which Python is running:

```bash
python3 -c "import sys; print(sys.executable)"
```

| Installation/path | What to do |
| --- | --- |
| Python.org: usually `/Library/Frameworks/Python.framework/` | Run **Applications > Python 3.x > Install Certificates.command**, let it finish, then retry. |
| Homebrew: usually `/opt/homebrew/` on Apple Silicon or `/usr/local/Cellar/` on Intel | No separate certificate command is normally needed. Persistent errors need a CA/proxy/VPN check. |
| Apple/Xcode or another path | Do not assume either fix applies. Share the path and safe debug output, never credentials. |

`which python3` can show a symlink rather than the actual installation. A working Python.org installation is fine after its certificate step; there is no need to install Homebrew just for this script.

> [!CAUTION]
> Never disable certificate verification or use an unverified-SSL workaround.

### Safe debug output

```bash
bash scripts/make-playlist.sh --version
bash scripts/make-playlist.sh --dry-run --debug
```

The first command should print v4. Debug prints Python/SSL versions, the public-source step, HTTP status and exception class, not headers, tokens or credentials. For the real run, use `--debug` without `--dry-run`. v4 includes the fix for non-seekable Terminal prompt errors on some Mac Python versions.

### Spotify errors

| Error | Next step |
| --- | --- |
| 403 | Check the app's user allowlist, the owner's Premium status and granted permissions. |
| 429 | Stop and wait before retrying. Do not keep hammering Run. |
| Expired authorization | Rerun and sign in again, keeping the receipt. |
| Port 8888 busy | Close another copy/local server using that port, then rerun. |

## What has been checked

The launcher dry-run, mocked API/retry tests and Terminal prompt were tested without credentials during development. Todd then used the script to create the linked public 49-song playlist. Public checks confirm its title/count and the first 25 exposed track IDs; regional playback and the remaining IDs require owner-side read-back.

The script is separate from the Go `songs playlist` subcommand, which remains an unimplemented placeholder.

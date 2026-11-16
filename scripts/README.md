# Create the Permanent Rotation playlist on your Mac

This script uses a browser sign-in, not your Spotify Client Secret. It reads
the live timeline and presents every track in the same order. The default
name is read from the current article title, currently:

Permanent Rotation: an agentic project for the end of the year

The default visibility is PUBLIC, as requested. Both are shown before you
must type `confirm`. The description includes the site link and a small
recovery label. Nothing is created if you do not confirm.

## What you need

- Your Mac, an internet connection and a browser.
- Spotify Premium on the app owner's account, the existing Spotify app,
  and its Client ID. Do not enter the Client Secret anywhere in this script.
- Python 3.8 or later, using its standard library. The script also uses
  macOS's built-in `open` command. No pip packages, git, Homebrew or curl
  setup is needed. Python 3 is NOT guaranteed on every stock macOS install.

## 1. Check Python first

Press Command-Space, type Terminal, press Return. Copy this and press Return:

    python3 --version

If it prints Python 3.8 or newer, continue. If macOS offers to install
Command Line Tools, cancel for now unless you want that installation.
If Python is missing, stop and ask for help installing Python from
https://www.python.org/downloads/macos/ . Do not run more commands hoping
it will fix itself. A Python.org install includes Install Certificates.command;
if needed, run it as the installer explains. Never disable HTTPS checks.

## 2. Add the callback once

1. Open https://developer.spotify.com/dashboard and select the app you
   already use for this project.
2. Open Settings and edit its Redirect URIs.
3. Add this EXACT value, then save:

       http://127.0.0.1:8888/callback

   This is your own Mac's loopback address. Do not replace 127.0.0.1 with
   localhost. Do not paste an access token, refresh token or Client Secret.
4. Copy the app's Client ID. It is an identifier, not the Client Secret.
5. If Spotify rejects the account later with 403, check the app's User
   Management allowlist and that its owner still has Premium. Development
   Mode permits up to five authorized users. Forkers need their own app.

## 3. Run a preview with no Spotify changes

Unzip the delivered folder in Downloads. Find make-playlist.sh. In Terminal:

- Type `chmod +x ` with a space after the x, drag make-playlist.sh from
  Finder into Terminal, press Return.
- Drag the same file into Terminal again, type a space and `--dry-run`,
  then press Return.

It should list the songs in displayed chapter order. At the time of testing:
49 Spotify tracks from 50 cards, with Deep Cover omitted. No substitutions.
The source is live, so review the actual list you see. Dry run does not ask
for credentials, save recovery state or contact Spotify.

## 4. Create once

Drag make-playlist.sh into Terminal and press Return (without --dry-run).
It shows the preview, asks for your Client ID, and opens Spotify in your
browser. Sign into the account that should own the playlist and approve
playlist permissions. Come back to Terminal.

Check the account, exact name, PUBLIC setting, description, recovery label
and track list. Type the single word `confirm` and press Return only if
all are right. Any other answer cancels. On success it prints a Spotify URL
and verifies the exact track count/order and ownership. Open that URL to
check playback before putting it in the article.

OAuth credentials stay in process memory. No Client Secret is needed.
No refresh token is kept. The short-lived access token is not printed or
written to disk. An optional SPOTIFY_CLIENT_ID environment variable can
supply the non-secret Client ID; you can simply use the prompt instead.
Do not run with bash -x, a debugger or terminal session recording.

## Retry safely

Run the SAME script again. You will sign in again; no login is saved.
The script uses this local non-secret recovery file:

    ~/Library/Application Support/Permanent Rotation/playlist-state.json

It contains the approved manifest, playlist ID/URL, owner ID and progress,
not tokens. Keep it even after success. Completed reruns verify the same
playlist, not create another. Edits made in Spotify after completion are
not overwritten. This is not an ongoing sync tool.

Track writing uses an exact-set operation on ONLY the newly created,
recorded playlist. Repeating that operation does not append duplicates.
Unexpected contents stop rather than overwrite. If creation's response is
lost, the state is marked ambiguous BEFORE sending; the script refuses to
create another. Inspect Spotify for the printed recovery label and ask for
help recovering the playlist ID. Do not delete the state file to retry.
There is no safe claim that any network program can atomically create a
playlist and save a local receipt; stopping is the safe fallback.

A crash may leave playlist-state.lock in the same folder. First ensure no
copy of the script is still running. Remove only that empty .lock folder,
never playlist-state.json, then run again. If unsure, ask for help.

Spotify may return 429, 403, unavailable tracks or an expired authorization.
The script stops, keeps state and prints a short explanation without token
contents. Do not keep hammering Run. Wait if rate limited. Playback can
vary by region/account even when count/order verification succeeds.

## For a fork

Use your own Spotify app and callback configuration. Pass your own generated
site and name; do not create a copy of Todd's playlist accidentally:

    ./make-playlist.sh --site https://YOURNAME.github.io/YOURREPO/index.html --name "Your playlist name" --private

Omit --private only when you want PUBLIC visibility. The same preview and
confirmation apply. The script reads the generated .body card structure,
not arbitrary playlists. Up to 100 tracks; no duplicate IDs. A state file
from another site is not overwritten. Keep each completed state intact.

## Verified boundaries

Dry run and mocked API/retry tests were run without Spotify credentials.
Actual user authorization, endpoint permissions and live playlist creation
remain to be tested by the owner. The script is not the Go `songs playlist`
command; that command is still not implemented.

## Version 2: safe network diagnostics

If dry run stops, use:

    ./make-playlist.sh --dry-run --debug

It prints Python/SSL versions, the current public-source step, HTTP status
and exception class. It never prints request headers, tokens or credentials.
For Python.org certificate errors, run Applications > Python 3.x >
Install Certificates.command and retry. Never disable HTTPS verification.

## Version 3: Terminal prompt fix

Version 3 opens separate read/write handles for Terminal prompts, avoiding
non-seekable-stream errors on macOS Python. Verify your extracted copy with
`./make-playlist.sh --version`; it must print v3. Then use
`./make-playlist.sh --debug` for the real run. Debug never prints tokens.
Dry-run does not save state. Keep any existing recovery file intact.

## Python HTTPS troubleshooting (Python.org vs Homebrew)

If you see `SSL: CERTIFICATE_VERIFY_FAILED`, first check which Python
is running:

    python3 -c "import sys; print(sys.executable)"

- Python.org installer: the path usually contains
  `/Library/Frameworks/Python.framework/`. Open Applications, your Python
  3.x folder, then double-click **Install Certificates.command**. Let it
  finish, then rerun `./make-playlist.sh --dry-run --debug`.
- Homebrew: the resolved path usually begins `/opt/homebrew/` (Apple
  Silicon) or `/usr/local/Cellar/` (Intel). A separate Install
  Certificates.command step is normally not needed. If certificate errors
  persist, ask for help checking Homebrew's CA setup or your network's
  proxy/VPN, rather than switching off certificate checks.
- An Apple/Xcode or other path: do not assume either fix applies. Share
  the path and safe `--dry-run --debug` output, never credentials.

`which python3` alone can show a symlink rather than the actual install.
You do not need to install Homebrew just to run this script. A working
Python.org installation is fine after the certificate setup step.
Never use an unverified-SSL workaround.

Current script version: v4 (`./make-playlist.sh --version`).

### GitHub web-upload permission note

A web-uploaded script may not have its executable bit set. Either follow
the chmod step above or run `bash scripts/make-playlist.sh --dry-run` from
the repository root. This does not change the Python or OAuth requirements.

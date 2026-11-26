# Purple Disc for Deezer

**Unofficial open-source client for Deezer.** Log in once. The GUIs sign you in
with an embedded Deezer web view, so there's no ARL to go hunting for. Then
browse your liked songs, playlists, charts, artists, and search. Every track
plays **locally**: it's streamed in, Blowfish-decrypted, decoded, and played
from memory (MP3, or FLAC on HiFi). Your ARL stays on your machine and only goes
to Deezer in the requests Purple Disc makes for you.

One Go engine handles the whole streaming path (login, decrypt, decode,
playback). Eight native front-ends sit on top of it.

By **Scharkenberg**, based on OpenDeezer by Cycl0o0.

## Clients

| Client | Stack | Builds |
|--------|-------|--------|
| **Terminal (TUI)** | Go · Bubble Tea | linux · macOS · windows (amd64/arm64) |
| **macOS** | SwiftUI · Liquid Glass (macOS 26) | universal (Apple Silicon + Intel) `gui/macos` |
| **Linux (unified)** | auto-picks GTK4 or Qt6 by desktop | x86_64 · aarch64 `gui/linux` |
| **GNOME** | GTK4 · libadwaita | x86_64 · aarch64 `gui/gnome` |
| **KDE** | Qt6 Widgets · Breeze | x86_64 · aarch64 `gui/kde` |
| **Windows** | WinUI 3 · C# / .NET 11 (Native AOT) · Fluent | x64 `gui/windows` |
| **Android** | Kotlin · Jetpack Compose (phone + **Android TV** D-pad flavor) | arm64/arm/x86_64 (gomobile AAR) `gui/android` |
| **iOS** | SwiftUI · Liquid Glass (iOS 26) | iPhone (gomobile xcframework) `gui/ios` |

The **unified Linux** client is a single `purpledisc` command that picks the
native toolkit for your desktop (Qt/Breeze on KDE-family, GTK4/libadwaita
elsewhere), the way LibreOffice does. If you'd rather have one toolkit, the
standalone `gui/gnome` and `gui/kde` binaries are there too.

Prebuilt binaries for everything are on the [Releases](../../releases) page.

## Features

**Browse & discover**
- Liked songs, your playlists, and full **search** — tracks, artists, albums, playlists.
- **Charts** — global top tracks, albums, artists and playlists.
- **Artist pages** — top tracks, discography and related artists.
- **Synced lyrics** — karaoke-style, line-by-line, kept in step with the audio
  (the reported position is compensated for your output device's buffered
  latency, so lines stay in step with the sound); plain-text fallback.
- **Deezer Flow** — your personalized, endless track stream.
- **Podcasts** — search shows, browse episodes, play. A multi-hour episode
  streams through a bounded, disk-backed buffer instead of holding the whole
  thing in RAM, and your listening history keeps podcasts out of the music
  top-tracks/artists stats.

**Library editing**
- **Like / unlike** tracks; **add to playlist**; **create / rename / delete** playlists.
- **Accurate likes everywhere** — every client seeds your liked-track ids from
  the engine, so the heart on each track shows the real state instead of resetting.

**Playback**
- **Quality tiers** — Normal (MP3 128), High (MP3 320), **HiFi (FLAC lossless)**.
- **Free accounts welcome** — a Deezer Free login streams the **full library at
  128 kbps**, without the audio ads Deezer's own players play. A paid plan
  unlocks 320/FLAC and downloads. (If a specific track has no full-length source
  for your account, playback falls back to Deezer's 30-second preview, marked as
  such — but that's the exception, not the rule.)
- **Download tracks** — save the current or selected track to disk (decrypted
  MP3/FLAC), on every client. Downloads need a paid Deezer plan; the folder is
  configurable and shared across clients. Whole-album and playlist downloads run
  with bounded concurrency, **resume mid-file over HTTP Range** if a connection
  drops, refresh expired URLs, and verify the final byte count before finishing.
- **Offline playback** — **Download for offline** pulls a track into the
  encrypted on-disk cache; cached tracks then play with **zero network** — no
  token or media round-trip — and a genuine offline miss fails cleanly instead of
  hanging. A **downloaded badge** marks what's saved, on every client.
- **Up Next** — a real queue editor on every client: see what's queued, jump to a
  track, remove, drag to reorder, **Play next** / **Add to queue** from any track,
  and **Clear**. It surfaces as sheets on macOS/iOS, a dock on KDE, a sidebar on
  GNOME, a flyout on Windows, phone/TV screens on Android, and the TUI queue view.
- **Gapless** transitions, **crossfade**, **ReplayGain** loudness normalization.
- **Sleep timer** — pause after 5/10/20/30/45/60 min or at the end of the current
  track, with a smooth fade-out (on every client + the web remote).
- **Output-device selection** (powered by the malgo/miniaudio backend).
- **Perceptual volume** taper + anti-click micro-fades on skip/seek for clean,
  natural playback.
- Shuffle, repeat (off/all/one), seek, volume; **resume** the last track on launch.
- Shows the **actual output format** that's playing (e.g. "FLAC · lossless").
- **OS media controls + now-playing** — MPRIS on Linux (GNOME/KDE/TUI media keys
  + overlays), Now Playing + media keys on macOS, SMTC on Windows, and
  lock-screen / MediaSession controls on Android and iOS, with shuffle,
  repeat and skip (plus lock-screen artwork on Android and a buffering state on
  iOS).

**Cast & remote (Purple Disc Connect)**
- **Cast between your devices** — hand playback to another Purple Disc device on
  your LAN and drive it from any client. **Repeat and shuffle work in both
  directions**: each client mirrors the **host's real** playback modes while
  casting, and you can push your **whole queue** to the host in one go.
- A **"Playing on `<device>`"** chip (with one-click **Play here**) shows where the
  sound is coming from, and remote controllers advance the **instant** a track
  ends — the host emits an explicit "finished" event over SSE, so no polling lag.

**Accounts & UX**
- **One-click login** — sign in via the embedded Deezer web view; the ARL is
  captured automatically (manual ARL entry still available).
- Shows your **account tier** after login; a clear "ARL expired" re-login prompt.
- **Background playback** / close-to-tray in the GUIs.
- **Home screen** — a discovery landing (greeting, quick picks, top tracks, your
  playlists) instead of opening straight to Liked Songs.
- **Update check** — each client checks GitHub on launch and shows a dismissible
  "update available" notice (opt-in "Check for updates" too); never auto-installs.
- **Remote control in Settings** — the control API / phone remote is
  configurable in-app (enable, LAN, token), not only via env/config file.
- Album art (truecolor half-blocks in the TUI; native everywhere else).
- TUI extras: queue view, lyrics, help screen, themes, vim keys, resume.
- Settings persisted to `~/.config/purpledisc/`; ARL stays local.

## Languages

Purple Disc is fully localized into seven languages, across every client — the
TUI, the phone web remote, and all the native GUIs:

**English** · **简体中文** (`zh`) · **हिन्दी** (`hi`) · **Español** (`es`) ·
**Français** (`fr`) · **العربية** (`ar`) · **Русский** (`ru`)

The UI follows your **system language** and falls back to English for anything a
language is missing. The GUIs each have a **Language** setting to override it per
app (Arabic switches them to a right-to-left layout). The TUI picks the language
from your `LANG` environment variable, or from Settings > **Language** (key `,`).

Adding a new language — or fixing an existing one — is documented in
[`docs/TRANSLATIONS.md`](docs/TRANSLATIONS.md).

## Install

Download a binary from [Releases](../../releases).

**GUIs** — launch one and click **Log in with Deezer**. An embedded web view
opens the Deezer login; once you're in, your session (ARL) is saved locally for
next time. No token to paste.

**Terminal (TUI)** — build it and provide your ARL:

```sh
make build          # -> ./purpledisc + ./purpledisc-mcp   (or: go build -o purpledisc ./cmd/purpledisc)
./purpledisc -save-arl <your-arl>   # writes ~/.config/purpledisc/arl.txt (0600)
./purpledisc
```

Or pass it inline: `DEEZER_ARL=<your-arl> ./purpledisc`. For the GUIs, see each
`gui/<platform>/README.md` for build steps. A Homebrew formula is in
`packaging/homebrew/`. See the [distribution status](packaging/README.md) for
F-Droid, Obtainium, Snap, WinGet, AUR, Scoop, and other provider manifests.

Your ARL is the `arl` cookie from an authenticated `deezer.com` browser session
(the GUI web-login grabs it for you). Treat it like a password — it grants
access to your account.

## Requirements

- A Deezer account. **Free works** — full-library streaming at 128 kbps. A
  **paid plan** unlocks higher
  quality (Premium for MP3 320, HiFi for FLAC) and **downloads**.
- Building from source: **Go 1.27+**, a C compiler, and a working audio device.
  The audio backend is [malgo](https://github.com/gen2brain/malgo) (miniaudio),
  so **cgo is required on every platform** (Linux/macOS/Windows).
- **Linux**: ALSA dev headers (`libasound2-dev`); plus the toolkit dev packages
  for the GUIs — GTK4/libadwaita/json-glib **and `libwebkitgtk-6.0-dev`** (GNOME
  web-login), and/or Qt6 **and `qt6-webengine-dev`** (KDE web-login).
- **macOS GUI**: macOS 26 (Tahoe) + Xcode 26 for the Liquid Glass APIs (the login
  web view uses the system WebKit framework — no extra dependency).
- **Windows GUI**: Windows 10 1809+/11, the **.NET 11 SDK** (Native AOT) + Windows App SDK,
  Visual Studio's clang or MinGW-w64 (Go cgo builds the engine DLL), and the Edge **WebView2**
  runtime (preinstalled on Windows 11) for the login web view.
- **Android**: Android 7.0+ (API 24). Building needs JDK 21, the Android SDK
  (platform 37.2), NDK r30, and gomobile. Two flavors ship from one codebase — `assembleMobileDebug`
  for phones/tablets and `assembleTvDebug` for **Android TV** (a D-pad-driven,
  10-foot UI on the leanback launcher).
- **iOS**: iPhone on iOS 17+ (Liquid Glass on iOS 26). Building needs Xcode,
  gomobile and xcodegen (`gui/ios/build.sh`); Liquid Glass falls back to a
  material on iOS < 26. Releases ship an **unsigned** `.ipa` — sideload with
  AltStore/Sideloadly, or set your own Team in Xcode and run.
- TUI album art needs a 256-color or truecolor terminal.

## TUI controls

| Key | Action | | Key | Action |
|-----|--------|-|-----|--------|
| ↑/↓ or j/k | move | | z | toggle shuffle |
| g / G | top / bottom | | r | cycle repeat (off→all→one) |
| enter | open / play | | +/- | volume |
| esc / ⌫ | back | | ←/→ | seek ±10s |
| space | play / pause | | e / E | add to queue / play next |
| n / p | next / prev | | a | add to a playlist |
| f | like / unlike | | b | go to album |
| / | search | | ctrl+f | filter the list |
| l | lyrics (synced) | | m | start a radio |
| u | queue view | | c | now playing + cover |
| s | stop | | , | settings (quality, sound, theme, language) |
| T | sleep timer | | D | download track (paid plans) |
| i | about | | ? | help · q quit |
| w | position and state | | | |

**Screen reader mode** (Narrator, NVDA, braille displays) shows two plain lines
instead of the full screen: where you are and what is selected, then what just
happened or what plays. It turns on by itself on Windows when a screen reader
runs, with `-screen-reader`, or under Settings › *Screen reader mode*; `w` says
the position, state and modes.

To sign out, choose Settings › *Sign out device* and confirm with `y`: the saved ARL is
deleted and the app quits. On Windows the desktop app shares that sign-in and is signed
out too.

Home screen entries: Liked Songs · My Playlists · Flow · Flow moods · Charts ·
Podcasts · Search · Stats · Remote control · Web Remote · Settings (and Resume
when a saved position exists). From 100 columns a side panel shows the cover
and track, or your recent listening; a click on it opens Now Playing. Symbols
are limited to what every terminal font draws as text.

## Use it as a Go library (SDK)

The engine is also a public Go SDK, so you can build your own tools on top of it —
the Deezer API, track decode/download, Purple Disc Connect, and the remote control.

```sh
go get github.com/Scharkenberg/PurpleDisc/v4
```

```go
import dz "github.com/Scharkenberg/PurpleDisc/v4/sdk/deezer"

client := dz.New(os.Getenv("DEEZER_ARL"))
client.Login()
client.SetQuality(dz.QualityHigh)

plan, _ := client.PrepareStream("3135556")   // resolve + key the stream
f, _ := os.Create("track.mp3")
dz.DownloadTrack(plan, f)                     // fetch CDN, Blowfish-decrypt, write
```

Packages: `sdk/deezer` (API + decode/download), `sdk/connect` (LAN discovery +
drive or host a device — `RemoteClient` out, `Host` in), `sdk/control` (host/drive
the control API + phone web remote), `sdk/player` (in-process playback, cgo).
Runnable examples in [`examples/`](examples); full docs in
[`sdk/README.md`](sdk/README.md).

## Remote control & automation (Control API)

Purple Disc can expose a small HTTP/JSON API so another Purple Disc client (remote
control) or an AI agent (MCP) can drive playback. It is **off by default**.

This is engine-hosted, so it works in **every client** — the TUI *and* all native
GUIs (the same `PURPLEDISC_CONTROL` / Discord settings apply). From a GUI the
engine exposes play/pause, stop, seek, volume, restart, play-track/playlist and
status (next/prev/shuffle/repeat live in the GUI's own queue).

Enable it with an env var or a config file:

```sh
export PURPLEDISC_CONTROL=1                 # localhost only (127.0.0.1:7654)
export PURPLEDISC_CONTROL=:7654             # bind all interfaces (LAN remote)
# or: echo 1 > ~/.config/purpledisc/control.txt
```

Endpoints (reads are `GET`, mutations are `POST`):

| Method | Path | Action |
|--------|------|--------|
| GET  | `/whoami` | account name + auth mode (unauthenticated) |
| GET  | `/status` | playback snapshot (state, track, position, volume, queue…) |
| GET  | `/playlists`, `/search?q=` | browse |
| POST | `/playpause` `/next` `/prev` `/stop` `/restart` | transport |
| POST | `/repeat` `/shuffle` | cycle repeat / toggle shuffle |
| POST | `/seek?ms=` `/volume?v=` | position / volume (0..1) |
| POST | `/play/track?id=` `/play/playlist?id=` | play by id (a playlist/album plays through the whole list) |

**Auth.** Credentials are sent via request **headers only**:

- **Account-based (default on LAN).** When bound to a non-loopback address with no
  token, a controller must prove it is logged into the **same Deezer account** by
  sending its own user id in `X-PurpleDisc-Account`. Your own devices connect with
  no token to copy; other accounts are rejected. `/whoami` deliberately does *not*
  reveal the user id (it's the credential), only the account name. This is
  LAN-trust grade — a Deezer user id is only semi-private. Disable with
  `PURPLEDISC_CONTROL_SAMEACCOUNT=0`.
- **Token (strongest).** Set `PURPLEDISC_CONTROL_TOKEN` (or
  `~/.config/purpledisc/control-token.txt`); send it in `X-PurpleDisc-Token`.
- **None.** Localhost binds with no token are open (loopback only).

Mutations require `POST` and reject requests carrying a browser `Origin` header,
so a web page you happen to visit can't drive your playback. The server also caps
request/response sizes + sets timeouts (slowloris/DoS), and **refuses to start
unauthenticated on a non-loopback address** — a LAN bind always requires
same-account or token auth, failing closed on a misconfiguration.

> **Security note.** A Deezer user id is only *semi-private* (it appears in
> profile URLs), so same-account auth is LAN-trust grade — fine for a home
> network. On an untrusted network, set `PURPLEDISC_CONTROL_TOKEN` for a real
> secret. Purple Disc Connect authenticates to *discovered* devices with the
> account id only (never the token), since a discovery reply is unauthenticated.

### Remote control (one client drives another)

Enable the Control API on the target (`PURPLEDISC_CONTROL=:7654`), then on
another Purple Disc client open **Remote control** from the menu, enter the
target's `host:port`, and connect. Transport keys (space/n/p/s, ←/→ seek, +/-
volume, r/z) drive the remote; the screen shows its live now-playing. Same
Deezer account auto-authenticates (or share a token).

**Purple Disc Connect** (GUIs + TUI) auto-discovers devices on the same LAN via
UDP multicast/broadcast. That needs a network that carries multicast/broadcast —
**Tailscale/VPN meshes don't** (they're unicast-only), and some routers filter it
between Wi-Fi and Ethernet. For those, list peers explicitly so they always show
in the picker:

```sh
# one host[:port] per line (port defaults to 7654)
#   ~/.config/purpledisc/connect-peers.txt   (also read on macOS)
echo "100.78.213.67:7654" >> ~/.config/purpledisc/connect-peers.txt
# or: export PURPLEDISC_CONNECT_PEERS=100.78.213.67:7654,192.168.1.20
```

(You can always just type the address into "Enter address…" too.)

### MCP server (AI agent control)

`purpledisc-mcp` is a [Model Context Protocol](https://modelcontextprotocol.io)
server that lets an AI assistant control playback through the Control API. Build
it with `go build ./cmd/purpledisc-mcp` (or `make` builds it alongside the TUI),
enable the Control API (above), then register it with your MCP client:

```json
{
  "mcpServers": {
    "purpledisc": {
      "command": "/path/to/purpledisc-mcp",
      "env": {
        "PURPLEDISC_CONTROL_URL": "http://127.0.0.1:7654",
        "PURPLEDISC_CONTROL_TOKEN": "your-token-if-set"
      }
    }
  }
}
```

Tools: `get_status`, `play_pause`, `next`, `prev`, `stop`, `restart`,
`cycle_repeat`, `toggle_shuffle`, `set_volume`, `seek`, `search`,
`list_playlists`, `play_track`, `play_playlist`.

## Discord Rich Presence

Show what you're listening to on your Discord profile. **Off by default** — it
needs a Discord application id (create one at the [Discord Developer
Portal](https://discord.com/developers/applications); optionally upload an art
asset named `purpledisc`):

```sh
# Linux:   ~/.config/purpledisc/discord-app-id.txt
# macOS:   ~/Library/Application Support/purpledisc/discord-app-id.txt
# Windows: %AppData%\purpledisc\discord-app-id.txt
echo your-application-id > ~/.config/purpledisc/discord-app-id.txt
# (env var also works for the TUI: export PURPLEDISC_DISCORD_APP_ID=...)
```

With Discord running, your now-playing track appears as "Listening to …" with a
live progress bar. Works on macOS, Linux, and Windows. If Discord isn't running
it's silently skipped.

> **GUI users (esp. macOS):** apps launched from Finder/Activities do **not**
> inherit your shell environment, so set the id via the **file** above, not the
> env var. The config file is read from the platform config dir *and*
> `~/.config/purpledisc/` (so either path works). Check
> `~/Library/Application Support/purpledisc/purpledisc.log` (macOS) — it logs
> `rich presence enabled (app …)` / `connected` once it's working.

## How it works

```
ARL ─login (gw-light)→ browse (gw + public REST): search, charts, artists,
                       lyrics, Flow, podcasts, library writes
                     → resolve track → encrypted CDN URL (MP3 128/320 or FLAC)
                     → streaming download → Blowfish BF_CBC_STRIPE decrypt
                       (plain stream for podcast episodes)
                     → MP3 (minimp3; go-mp3 without cgo) / FLAC (dr_flac; mewkiz without cgo) decode → PCM ring
                     → malgo (miniaudio) output device → speakers
```

- `internal/deezer` — login, browse (search/charts/artists/lyrics/Flow/podcasts),
  library writes, track→URL resolve, the stripe decryptor.
- `internal/audio` — malgo backend: streaming buffer → decode → PCM ring with
  seek, ReplayGain, gapless, crossfade and output-device selection.
- `internal/queue` — the shared playback queue (shuffle/repeat/history).
- `internal/mpris` — Linux MPRIS media controls.
- `internal/log` — leveled file logging (`$PURPLEDISC_LOG`).
- `internal/ui` — the Bubble Tea TUI.
- `corelib` — the engine exposed as a C ABI (`-buildmode=c-archive` for
  macOS/Linux, `-buildmode=c-shared` DLL for Windows) so the native GUIs link it.
- `mobile` — the engine exposed for **gomobile** (`Pdmobile` AAR) so the Android
  app drives it from Kotlin.

## Build from source

Clone the repo and build whichever client you want. They all use the same Go
engine (`corelib`) underneath; each `build.sh` / `build.ps1` compiles the engine
first, then the native app.

**Terminal (any OS)** — Go 1.27+ and a C compiler (the malgo audio backend needs
cgo on every platform; Linux also needs `libasound2-dev`, Windows needs
Visual Studio's clang or MinGW-w64):
```sh
CGO_ENABLED=1 go build -o purpledisc ./cmd/purpledisc      # or: make build (also builds purpledisc-mcp)
```

**macOS app** — macOS 26 (Tahoe) + Xcode 26, Go:
```sh
cd gui/macos && make app        # -> PurpleDisc.app (universal: Apple Silicon + Intel)
```

**Linux — unified** (auto-picks GTK/Qt) — `libgtk-4-dev libadwaita-1-dev
libjson-glib-dev libwebkitgtk-6.0-dev libsoup-3.0-dev qt6-base-dev
qt6-webengine-dev libasound2-dev meson ninja-build cmake` + gcc, Go:
```sh
cd gui/linux && ./build.sh && ./dist/purpledisc
```

**Linux — single toolkit:**
```sh
cd gui/gnome && ./build.sh && ./purpledisc-gnome     # GTK4 / libadwaita
cd gui/kde   && ./build.sh && ./purpledisc-kde       # Qt6 / Breeze
```

**Windows app** — Windows 10/11, .NET 11 SDK (Native AOT) + Windows App SDK,
Visual Studio's clang or MinGW-w64 (Go cgo), Go:
```powershell
cd gui\windows; .\build.ps1     # cgo builds the engine DLL, then `dotnet publish`
```

**Android app** — Go, JDK 21, Android SDK (platform 37.2), NDK r30, and gomobile. `build.sh` binds
the engine to an `Pdmobile` AAR, then Gradle assembles the APK:
```sh
cd gui/android && ./build.sh    # -> app/build/outputs/apk/mobile/debug/app-mobile-debug.apk
                                #    app/build/outputs/apk/tv/debug/app-tv-debug.apk
```

## FAQ

**How do I log in?**
In the GUIs, click **Log in with Deezer** — an embedded web view opens the real
Deezer login, and once you sign in, Purple Disc reads the `arl` session cookie
automatically and saves it locally. Manual ARL entry is still there as a
fallback. The TUI uses `DEEZER_ARL` / `purpledisc -save-arl <arl>`.

**Does it have Flow / podcasts / charts / lyrics?**
Yes — Deezer Flow (personalized stream), podcast search + episode playback,
global charts, artist pages, and synced lyrics are all built in.

**Can I edit my library?**
Yes — like/unlike tracks, add tracks to playlists, and create/rename/delete
playlists.

**Can I choose the output device or use gapless/crossfade?**
Yes. The audio engine (malgo/miniaudio) supports output-device selection,
gapless transitions, crossfade and ReplayGain — all in settings (the TUI's
Settings screen, key `,`).

**Is there a sleep timer?**
Yes. Pause after 5/10/20/30/45/60 minutes or at the end of the current track, with
a smooth fade-out. It's in each app's settings, on the phone web remote, and on the
TUI key `T` (cycles off → 5 → 10 → 20 → 30 → 45 → 60 min → end of track).

**What's an ARL?**
Your Deezer session token — the `arl` cookie from a logged-in `deezer.com`
browser session. It authenticates you the same way the official app does. Treat
it like a password; it only ever lives on your own machine.

**Why does it need my Deezer login (ARL) instead of an API key?**
Deezer's public API doesn't allow full-track streaming. The only way to play
your music is the same authenticated path the official client uses, which needs
your session (the ARL).

**Do I need Deezer Premium?**
No — a **free** Deezer account works. Deezer Free streams the full library at
128 kbps. Purple Disc plays full tracks, as Deezer's own web player does, but none
of the audio ads Deezer plays on the free tier. It reports your plays to Deezer as
the web player does, which credits artists. Free settings include an
at-your-own-risk toggle to turn play reporting off. A
**paid plan** unlocks higher quality (Premium for MP3 320, HiFi for FLAC) and
downloads. (A track with no full-length source for your account falls back to
Deezer's 30-second preview, marked as such — the exception, not the rule.)
Purple Disc only plays what your own account can already reach.

**Can I download tracks?**
Yes, with a **paid plan** — the current or selected track saves to disk as a
decrypted MP3/FLAC (TUI key `D`, or the Download action in each GUI's track
menu), into a configurable folder shared across clients. Downloads are disabled
on free accounts (which have no full-length source to save — only the public
preview). Only content your own account is entitled to is ever fetched; keep
what you save for personal use.

**Does my ARL get uploaded anywhere?**
No. Login, decrypt and decode all run on your machine; the only requests that
leave are to Deezer itself.

**Is this legal? Will my account get banned?**
Grey zone. It reaches Deezer the unofficial way and plays/decrypts your own
entitled content locally, which almost certainly breaks Deezer's terms for
third-party apps. Personal/educational use, your own account, your own risk. Not
affiliated with Deezer.

**Does it support HiFi / FLAC?**
Yes — if your account is HiFi-entitled. Pick HiFi in settings (in the TUI,
Settings > Audio quality); it streams lossless FLAC.

**Why not just use the official app?**
Mostly because it's a reverse-engineering project and a learning exercise. You
also get lightweight native clients (including a terminal one) with no telemetry,
on platforms the official app doesn't always cover.

**Is it open source?**
Yes, AGPL-3.0. Read it, build it, audit exactly what it does.

## The fine print

Personal/educational use, your own account, your own risk. It reaches Deezer the
unofficial way and plays/decrypts your own entitled content locally, which almost
certainly breaks Deezer's terms for third-party apps. Not affiliated with Deezer.
AGPL-3.0. See the [privacy policy](PRIVACY.md) for what the clients store locally
and which third-party services they contact.

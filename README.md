# Purple Disc for Deezer

Purple Disc is an unofficial, open-source Deezer client. It signs in with your
own Deezer account and plays your music without Deezer's apps: liked songs,
playlists, search, Flow, charts, artists, podcasts, lyrics. The streaming,
decryption and decoding all happen on your device.

There is one engine, written in Go, and eight apps built on it: a terminal app,
macOS, iOS, Android (phone and TV), Windows, GNOME, KDE, and a Linux launcher
that picks GTK or Qt for your desktop.

By Scharkenberg, based on OpenDeezer by Cycl0o0. Licensed under AGPL-3.0.

## Status

Version 4.0.0. No release has been published from this repository yet, so for
now you build the apps yourself (see [Building](#building)). The release
workflow publishes the files listed below once a `v4.0.0` tag is pushed.

## The apps

| App | Built with | Runs on | Release file |
|-----|------------|---------|--------------|
| Terminal | Go, Bubble Tea | Linux (x86_64, arm64), macOS (Apple silicon, Intel), Windows (x64) | `purpledisc-tui-<os>-<arch>` |
| macOS | SwiftUI | macOS 26 or later, universal | `PurpleDisc-macos-universal.zip` |
| iOS | SwiftUI | iOS 17 or later, iPhone and iPad | `PurpleDisc-ios-unsigned.ipa` |
| Android | Kotlin, Jetpack Compose | Android 7.0 or later, phone and Android TV | `PurpleDisc-android*.apk`, `PurpleDisc-androidtv*.apk` |
| Windows | WinUI 3, C# (.NET 11, Native AOT) | Windows 10 1809 or later, x64 with AVX2 | `PurpleDisc-windows-x64.zip` |
| GNOME | GTK 4, libadwaita | Linux x86_64, aarch64 | `PurpleDisc-gnome-<arch>.tar.gz` |
| KDE | Qt 6 Widgets | Linux x86_64, aarch64 | `PurpleDisc-kde-<arch>` |
| Linux | GTK or Qt, chosen per desktop | Linux x86_64, aarch64 | `PurpleDisc-linux-<arch>.tar.gz` |

The Android release has one APK for all CPUs plus one per ABI (armeabi-v7a,
arm64-v8a, x86, x86_64), for phones and for TV. The iOS build is unsigned:
sideload it, or open the project in Xcode and sign it with your own team.

## What it does

- Browse liked songs, your playlists, albums and artists; search tracks,
  albums, artists and playlists.
- Flow, Deezer's endless personal mix, with one tap. The Windows, Android phone
  and terminal apps also offer Flow's moods.
- Charts, artist pages (top tracks, discography, related artists), track and
  artist mixes.
- Synced lyrics, timed against the audio device's output delay, with plain
  lyrics as the fallback. (Not on Android TV.)
- Podcasts: search shows, play episodes, resume where you stopped. (Not on
  Android TV.)
- Like and unlike, add to playlists, create, rename and delete playlists.
- A queue in every app: reorder, remove, play next, add to queue.
- Listening history on the device: recent plays, top tracks and top artists.
- Normal (MP3 128), High (MP3 320) and HiFi (FLAC), capped to what your plan
  allows.
- Gapless playback, crossfade, loudness normalization (ReplayGain), a 10-band
  equalizer and mono downmix.
- Sleep timer: 5, 10, 20, 30, 45 or 60 minutes, or the end of the track, with
  a fade-out.
- Output device selection (everywhere except macOS, which follows the system
  output).
- Media keys and system now-playing: MPRIS on Linux, Now Playing on macOS,
  media transport controls on Windows, the lock screen on Android and iOS.
- One player at a time, like Deezer's own apps: when another player of your
  account starts a track, Purple Disc pauses. This can be turned off.
- Downloads of tracks, albums and playlists as decrypted, tagged MP3 or FLAC
  files (paid plans only).
- Offline: pin a track, album or playlist and its audio is kept in the
  on-device cache; in offline mode the apps play only what is pinned or
  cached and send nothing to Deezer. (All apps except the terminal.)
- Purple Disc Connect: find your other Purple Disc devices on the network and
  play on them, and a phone remote served as a web page.
- An update check against this repository's releases. It never downloads or
  installs anything.

Some features are only in some apps so far: charts by genre and "Go to album"
are in the Windows, Android phone and terminal apps; playback speed for podcast
episodes is in the Windows and Android apps.

### Free and paid accounts

A free Deezer account works. It plays full tracks at 128 kbps, with none of the
audio ads Deezer's own apps play on the free plan. Purple Disc reports plays to
Deezer the way Deezer's web player does, so artists are credited; free
accounts get a setting to turn that off, at your own risk. A paid plan adds
320 kbps (Premium) or FLAC (HiFi) and downloads. When a track has no full
version available to your account, Purple Disc plays Deezer's 30-second
preview and marks it as such.

## Signing in

The desktop and mobile apps sign in through Deezer's own login page in an
embedded web view and keep the session cookie (the ARL) on the device. Each of
them also lets you paste an ARL by hand.

The terminal app reads the ARL from the `DEEZER_ARL` environment variable or
from `arl.txt` in its settings folder:

```sh
purpledisc -save-arl <your-arl>    # writes arl.txt, readable only by you
purpledisc
```

The ARL is the `arl` cookie of a signed-in deezer.com session. It gives full
access to your account, so treat it like a password. It is only ever sent to
Deezer.

## Languages

English, Chinese (Simplified), Hindi, Spanish, French, Arabic and Russian, in
every app and in the phone remote. The apps follow the system language and
fall back to English for anything missing. The Windows and terminal apps also
have a language setting; on Android 13 and later you can pick a language per
app in the system settings. Arabic switches the layout to right-to-left.

Adding or fixing a translation is described in
[docs/TRANSLATIONS.md](docs/TRANSLATIONS.md).

## The terminal app

It needs a terminal with 256 colors or true color for cover art; everything
else works anywhere.

| Key | Action | Key | Action |
|-----|--------|-----|--------|
| space | play / pause | e / E | add to queue / play next |
| n / p | next / previous track | f | like / unlike |
| ← / → | seek 10 s | a | add to a playlist |
| + / - | volume | b | go to album |
| z | shuffle | D | download (paid plans) |
| r | repeat: off, all, one | N R X | new, rename, delete playlist |
| s | stop | c | now playing |
| T | sleep timer | u | queue (x J K: remove, move) |
| m | start a radio | l | lyrics |
| w | say position and state | ? | help |
| ↑ ↓ j k | move | , | settings |
| g / G | top / bottom | i | about |
| enter | play or open | q | quit |
| / | search Deezer | esc | back |
| ctrl+f | filter the list | | |

The home screen lists Liked Songs, My Playlists, Flow, Flow moods, Charts,
Podcasts, Search, Stats, Remote control, Web Remote and Settings. From 100
columns wide, a side panel shows the cover and what plays next, or your recent
listening when nothing plays.

Settings holds audio quality, ReplayGain, the equalizer and its preset, mono,
crossfade, gapless, the output device, the stream cache, the color theme
(deezer, ocean, sunset, mono, matrix), language, screen reader mode, play
reporting (free accounts) and Sign out device. Signing out asks first, then
deletes the saved ARL and quits. On Windows the desktop app reads the same
file, so it is signed out too.

Screen reader mode is for Narrator, NVDA and braille displays. Instead of the
full screen it shows two short lines: where you are and what is selected, then
what just happened. Nothing on it changes by itself, so the screen reader only
speaks when something does. It turns on by itself when Windows reports a
screen reader, with `-screen-reader`, or in Settings. Press `w` to hear the
position, state and playback modes.

## Purple Disc Connect and remote control

Each app can serve a small HTTP API that other Purple Disc apps, the phone
remote and scripts use to control playback. It is off unless you turn it on,
in the app's settings or here:

```sh
PURPLEDISC_CONTROL=1        # this computer only (127.0.0.1:7654)
PURPLEDISC_CONTROL=:7654    # the local network
# or put the same value in control.txt in the settings folder
```

On the network, a controller has to be signed in to the same Deezer account
(it sends its user id in `X-PurpleDisc-Account`), or send the token from
`PURPLEDISC_CONTROL_TOKEN` / `control-token.txt` in `X-PurpleDisc-Token`. The
server refuses to start on a network address without one of the two. Changes
must be `POST` requests, and requests carrying a browser `Origin` header are
refused, so a web page can't drive your player. The account check is only as
private as a Deezer user id, which is fine at home; on a network you don't
trust, set a token.

| Method | Path |
|--------|------|
| GET | `/whoami` (no auth; account name only), `/status`, `/events` (server-sent events), `/playlists`, `/search?q=`, `/history/recent` |
| POST | `/playpause`, `/next`, `/prev`, `/stop`, `/restart`, `/repeat`, `/shuffle`, `/seek?ms=`, `/volume?v=`, `/sleep` |
| POST | `/play/track`, `/play/playlist`, `/play/album`, `/play/mix/track`, `/play/mix/artist` |
| POST | `/queue/add`, `/queue/set`, `/queue/jump`, `/queue/remove`, `/queue/move` |
| GET, POST | `/eq` |

**Finding devices.** Apps find each other by UDP multicast with a broadcast
fallback. Networks that don't carry multicast, like Tailscale or other VPN
meshes, need the peers listed by hand, one `host[:port]` per line in
`connect-peers.txt` in the settings folder, or in `PURPLEDISC_CONNECT_PEERS`
(comma-separated). The port defaults to 7654. When you play on another device,
the controlling app sends it one track at a time and checks its status every
second.

**Phone remote.** Turn on the web remote in an app's settings. It shows an
address and a QR code for `http://<computer>:7654/remote` and a six-digit code
to pair the phone. The page has play and pause, next and previous, seek,
volume, shuffle and repeat, the queue, the equalizer and the sleep timer.

**MCP server.** `purpledisc-mcp` exposes the same API to AI agents over the
[Model Context Protocol](https://modelcontextprotocol.io). Build it with
`go build ./cmd/purpledisc-mcp` (or `make build`), turn on the API, and add it
to your MCP client:

```json
{
  "mcpServers": {
    "purpledisc": {
      "command": "/path/to/purpledisc-mcp",
      "env": {
        "PURPLEDISC_CONTROL_URL": "http://127.0.0.1:7654",
        "PURPLEDISC_CONTROL_TOKEN": "your-token-if-you-set-one"
      }
    }
  }
}
```

`PURPLEDISC_CONTROL_ACCOUNT` can stand in for the token. The server has 31
tools: status and transport (`get_status`, `play_pause`, `next`, `prev`,
`stop`, `restart`, `cycle_repeat`, `set_repeat`, `toggle_shuffle`,
`set_shuffle`, `set_volume`, `seek`), playing (`play_track`, `play_playlist`,
`play_album`, `play_mix_track`, `play_mix_artist`), the queue (`queue_add`,
`queue_jump`, `queue_remove`, `queue_move`), library (`search`,
`list_playlists`, `history_recent`), the sleep timer (`set_sleep_timer`,
`cancel_sleep_timer`), devices (`whoami`, `list_devices`, `select_device`) and
the equalizer (`get_eq`, `set_eq`).

## Discord Rich Presence

Off unless you give it a Discord application id, from the
[Discord Developer Portal](https://discord.com/developers/applications). Put it
in `discord-app-id.txt` in the settings folder (or, for the terminal app, in
`PURPLEDISC_DISCORD_APP_ID`). An art asset named `purpledisc` is used if the
application has one. While Discord runs, your profile shows "Listening to"
with the track and its progress; without Discord nothing happens.

## Files and folders

| | Linux | macOS | Windows |
|---|---|---|---|
| Settings (`arl.txt`, `playback.json`, `control.txt`, the log) | `~/.config/purpledisc` | `~/Library/Application Support/purpledisc` | `%APPDATA%\purpledisc` |
| Cache (stream files, offline pins) | `~/.cache/purpledisc` | `~/Library/Caches/purpledisc` | `%LOCALAPPDATA%\purpledisc` |

Downloads go to `PURPLEDISC_DOWNLOAD_DIR`, else the folder in
`download-dir.txt`, else `~/Music/PurpleDisc` (or `~/Downloads/PurpleDisc`).
The log is `purpledisc.log` in the settings folder; `PURPLEDISC_LOG` sets its
level. Android and iOS keep everything inside the app.

## How playback works

```
ARL → sign-in (Deezer's web gateway) → browse: gateway + public REST API
    → track → encrypted CDN stream (MP3 128 / 320 or FLAC)
    → written to a file on disk as it arrives, still encrypted
    → read back through a Blowfish decryptor (64 KiB in memory at a time)
    → MP3 or FLAC decoder → audio output
```

The decoders are minimp3 and dr_flac (go-mp3 and mewkiz/flac in builds without
cgo). Audio goes out through miniaudio (malgo) on Linux and Windows and through
oto on macOS. Podcast episodes are not encrypted and are not kept in the cache.

The code:

- `internal/deezer`: sign-in, browsing, library edits, stream URLs, decryption,
  downloads.
- `internal/audio`: stream files, decoders, the player (seek, gapless,
  crossfade, ReplayGain, equalizer, output devices).
- `internal/engine`: everything the apps share (queue, offline, Connect,
  history, settings); `corelib` exports it as a C library for the desktop
  apps, and `mobile` as a gomobile library (`pdmobile`) for Android and iOS.
- `internal/control`, `internal/discovery`: the control API and device
  discovery.
- `internal/ui` and `cmd/purpledisc`: the terminal app.
- `sdk/`: the public Go packages (below).

## Use it as a Go library

```sh
go get github.com/Scharkenberg/PurpleDisc/v4
```

```go
import dz "github.com/Scharkenberg/PurpleDisc/v4/sdk/deezer"

client := dz.New(os.Getenv("DEEZER_ARL"))
if err := client.Login(); err != nil {
	log.Fatal(err)
}
client.SetQuality(dz.QualityHigh)

plan, err := client.PrepareStream("3135556") // resolve the track and its key
if err != nil {
	log.Fatal(err)
}
f, _ := os.Create("track.mp3")
defer f.Close()
if err := dz.DownloadTrack(plan, f); err != nil { // fetch, decrypt, write
	log.Fatal(err)
}
```

The packages are `sdk/deezer` (API, streams, downloads), `sdk/connect`
(discover and drive other devices, or host one), `sdk/control` (host or drive
the control API and phone remote) and `sdk/player` (playback in your own
program; needs cgo). There are runnable examples in [examples/](examples) and
more in [sdk/README.md](sdk/README.md).

## Building

Everything needs Go 1.27 or later. The engine uses cgo, so you also need a C
compiler: on Linux, gcc and `libasound2-dev`; on Windows, Visual Studio's
clang or MinGW-w64. Each app's README in `gui/<app>/` has the details.

**Terminal app**

```sh
make build                          # ./purpledisc and ./purpledisc-mcp
# or: go build -o purpledisc ./cmd/purpledisc
```

**macOS** (macOS 26, Xcode 26)

```sh
cd gui/macos && make app            # gui/macos/PurpleDisc.app
```

**Linux** (GTK 4, libadwaita, json-glib, WebKitGTK 6, libsoup 3, Qt 6 with
WebEngine, ALSA, meson, ninja, cmake)

```sh
cd gui/linux && ./build.sh          # ./dist/purpledisc, picks GTK or Qt
cd gui/gnome && ./build.sh          # ./purpledisc-gnome
cd gui/kde   && ./build.sh          # ./purpledisc-kde
```

**Windows** (.NET 11 SDK, Windows App SDK 2.7)

```powershell
cd gui\windows; .\build.ps1
# bin\Release\net11.0-windows10.0.26100.0\win-x64\publish\PurpleDisc.exe
```

**Android** (JDK 21, Android SDK platform 37.2 with build-tools 37.0.0, NDK r30)

```sh
cd gui/android && ./build.sh        # debug APKs for phone and TV in app/build/outputs/apk/
```

On Windows, `gui\android\release.ps1 -Abi x86_64,arm64-v8a` builds signed
release APKs into `gui/android/dist`.

**iOS** (Xcode 26, xcodegen; `gui/ios/build.sh` builds for the simulator)

The packaging recipes (Homebrew, AUR, Scoop, Snap, Flatpak, WinGet, F-Droid,
Obtainium) are in [packaging/](packaging); [packaging/README.md](packaging/README.md)
says where each one stands. Their checksums are filled in when a release is
made.

## Questions

**Why does it need my ARL instead of an API key?**
Deezer's public API only plays 30-second previews. Full tracks need a signed-in
session, which is what the ARL is.

**Is it legal? Can my account be banned?**
It reaches Deezer in a way Deezer doesn't offer to third-party apps, which
almost certainly breaks their terms. It only plays what your own account can
already play. Use it for yourself, with your own account, at your own risk.

**Does it collect anything?**
No. The apps talk to Deezer, to GitHub for the update check, and to Discord
if you turn that on. [PRIVACY.md](PRIVACY.md) lists what is stored on the
device.

**Is it affiliated with Deezer?**
No.

# Purple Disc (Android)

Native **Kotlin / Jetpack Compose / Material 3** front-end for Purple Disc — *an unofficial
open-source client for Deezer*. The whole engine (login, browse,
Blowfish decrypt, decode, playback, Purple Disc Connect) is the Go core bound to
an Android library (`app/libs/pdmobile.aar`) via **gomobile** and called
in-process through the `pdmobile.Pdmobile` static API; this layer is UI only.

## Look

- **Material 3, dark-first:** a Deezer-purple (`#A238FF`) accent on a deep
  purple-black surface, a `NavHost` shell, lazy track lists, album/artist art
  carousels and a persistent bottom now-playing bar that expands into a full
  player.
- **Compose-only:** every screen is a composable; the only XML is the manifest,
  the launcher icon and a thin host theme.

## Features

- **Login** via an in-app `WebView` (`deezer.com/login`) that reads the `arl`
  cookie with `CookieManager` and calls `Pdmobile.init(arl)`, plus a manual
  "paste ARL" fallback. The ARL is persisted (app-private `SharedPreferences`)
  for auto-login.
- **Free-account gate:** non-premium accounts get a "Premium required" screen.
- **Home:** Liked Songs · My Playlists · Flow · Charts · Podcasts · Search · Queue.
- **Browse:** tracks (tap to play), albums/artists/playlists carousels, artist
  top tracks, playlist & album tracks, podcast shows → episodes.
- **Player:** bottom bar + full now-playing screen (large art, seek, volume,
  like, lyrics, format), driven by a ~500 ms poll of the engine that auto-advances
  the in-app queue on `finishedCount`.
- **Synced lyrics** (current line highlighted by position), **Queue** view,
  **Search**, **Settings** (quality Normal/High/HiFi, ReplayGain, gapless,
  crossfade, plus the remote toggles below and an update check).
- **Purple Disc Connect:** a cast button opens a device picker from
  `discoverDevices(700)` (name · type · version, plus a "This device" entry);
  `connectDevice` / `disconnectDevice` route playback to another Purple Disc.
- **Remote settings** (in Settings): *Make this device reachable* advertises the
  phone as an Purple Disc Connect host (`connectHostSetEnabled`), and *Phone
  Remote* serves the browser remote page (QR + code). Both persist in
  `SharedPreferences` and are re-applied after login (`AppViewModel.applyRemoteHosts`).

## Android TV

A second Gradle flavor (`tv`) reuses the same engine, `AppViewModel` and
`PlayerController` behind a **D-pad-driven, 10-foot Compose UI** on the leanback
launcher:

- `src/tv/` holds `TvActivity` + a focusable browse UI (Flow / Made-for-you /
  Charts / Albums / Playlists shelves), search, album/playlist detail lists and
  a persistent now-playing bar with focusable transport buttons. Focus scales and
  outlines the selected card so it reads from across the room.
- No extra dependencies — plain Compose `foundation` + `material3` with
  `clickable`/`onFocusChanged` for D-pad focus (no `androidx.tv`).
- The `mobile` flavor keeps the touch app unchanged. Each flavor supplies its own
  launcher manifest (`src/mobile/`, `src/tv/`); shared code lives in `src/main/`.
- App id `dev.scharkenberg.purpledisc.tv`, so it installs alongside the phone app.

## Build & run

Windows (PowerShell 7, from the repository root):

```powershell
gui\android\build.ps1 [-Tv]
# -> gui\android\app\build\outputs\apk\mobile\debug\app-mobile-debug.apk  (phone/tablet)
# -> gui\android\app\build\outputs\apk\tv\debug\app-tv-debug.apk          (Android TV, with -Tv)
gui\android\release.ps1 [-Abi arm64-v8a[,x86_64]] [-Tv] [-Keystore <file>]
# -> gui\android\dist\PurpleDisc-<version>-<abi>.apk per CPU type, signed and verified; the build folders deleted
```

Linux and macOS:

```sh
gui/android/build.sh
```

The scripts (1) build the gomobile/gobind versions pinned by `go.mod`, (2) bind the Go engine
for 64-bit ARM and x86_64 with 16 KB aligned segments and packed relocations
(`CGO_LDFLAGS=-Wl,-z,max-page-size=16384 -Wl,--pack-dyn-relocs=android`) and the engine's CPU
profile (`GOFLAGS=-pgo=<repo>/corelib/default.pgo`), then
`gomobile bind -trimpath -ldflags="-s -w" -target=android/arm64,android/amd64 -androidapi 24`;
the Windows ones skip the bind while `app\libs\pdmobile.aar` is newer than the engine's sources
and `env.ps1`.
`build.ps1` and `build.sh` then run `gradlew --no-daemon assembleMobileDebug` (and
`assembleTvDebug`). CI (`.github/workflows/android.yml`) binds all four CPU types.

`release.ps1` runs `assembleMobileRelease -Pabi=<abi>` once per CPU type (R8 code and resource
optimization, the engine for that CPU type only) and signs each APK, v2 and v3 only (Android 7
reads v2), with `-Keystore` (password in `ANDROID_KEYSTORE_PASSWORD`), else
`ANDROID_KEYSTORE_FILE`, else the SDK's debug keystore.
Android refuses it over a copy signed with another key until that copy is uninstalled.
### Prerequisites

- **Go 1.27**, **JDK 21**, the **Android SDK** with platform **37.2** and build-tools
  **37.0.0**, and the **NDK r30** (`ndk;30.0.16248370`).
- `ANDROID_HOME`, `ANDROID_NDK_HOME` and `JAVA_HOME` point at them. `env.ps1` uses
  `C:\Program Files (x86)\Android\android-sdk`, its `ndk\30.0.16248370` and
  `C:\Program Files (x86)\Android\openjdk\jdk-21.0.8` when they are not set.
- First Gradle run downloads Gradle 9.9.2 through the committed wrapper.

## Toolchain

- Gradle **9.9.2** · Android Gradle Plugin **9.5.2** with its built-in Kotlin at
  **2.4.21** (`kotlin-gradle-plugin` on the buildscript classpath,
  `org.jetbrains.kotlin.plugin.compose`) · Compose BOM **2026.11.01**.
- `compileSdk` **37.2**, `targetSdk` **37**, `minSdk` **24**, JDK **21**.
- Release builds: R8 with resource shrinking (`optimization { enable = true }`); the
  engine AAR brings its own keep rules.
- Coil 3 (`io.coil-kt.coil3:coil-compose`, `coil-network-core`) loads `artworkUrl`s
  through one image loader built in `PurpleDiscApplication`, which downloads through the
  engine's `Fetch` (no OkHttp in the app); Navigation Compose
  drives routing.

## Architecture

```
app/src/main/java/dev/scharkenberg/purpledisc/
  MainActivity.kt        Compose host
  PurpleDiscApp.kt       auth gate + NavHost + bottom player bar + connect dialog
  AppViewModel.kt        login/account state, owns the PlayerController
  Routes.kt              navigation routes
  engine/Engine.kt       suspend facade over the gomobile Pdmobile static API
  engine/Models.kt       data models + org.json parsers (match the wire shapes)
  player/PlayerController.kt  in-app queue, 500 ms poll, finishedCount auto-advance
  data/Prefs.kt          ARL + remote-host toggle persistence
  ui/                    theme, components (Artwork/TrackRow/PlayerBar) and screens

app/src/mobile/          phone launcher manifest (.MainActivity)
app/src/tv/              Android TV: TvActivity + D-pad Compose UI, leanback manifest, banner
```

Package id `dev.scharkenberg.purpledisc` (`.tv` suffix for the TV flavor). Author: Scharkenberg, based on OpenDeezer by Cycl0o0.

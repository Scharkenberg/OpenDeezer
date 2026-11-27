# Purple Disc Win32

A small, native Win32 front end to the shared Go engine. It uses standard
Windows controls and needs no .NET, Windows App SDK or WebView2 runtime.

The app targets **Windows 10 or later**, x86 (32-bit). Go 1.27 is the lowest
supported Windows toolchain for this repository; 32-bit output does not restore
Windows XP support. The same executable also runs on 64-bit Windows.

## Build

Requires Go 1.27+, Visual Studio 2026 with the C++ Clang Compiler component,
the x86 MSVC libraries, and the Windows 10 SDK. From the repository root:

```powershell
gui\win32\build.ps1
```

The build uses the installed Visual Studio clang in its `i686-pc-windows-msvc`
target, cgo for the existing miniaudio backend, `GOARCH=386`, and the safe
`GO386=sse2` baseline. It does not set `GOAMD64` or use AVX2-specific compiler
flags. The manifest enables per-monitor DPI awareness and Windows common-controls
visual styles. Layout and font scale with each monitor; the font is Inter
(`assets\fonts\Inter-Regular.ttf`, embedded as a resource and loaded for this
process only) at 110 % of the larger of the system's message and menu fonts. The language is the
terminal app's (`language.txt` or `PURPLEDISC_LANG`), else the system's. The app
follows the Windows light/dark setting or the shared `theme` value in
`%APPDATA%\purpledisc\settings.json`; high contrast remains system controlled.
The output is
`gui\win32\dist\PurpleDisc-win32-x86.exe`.

## Use

Paste an ARL from a signed-in Deezer session into the top field and press Enter
(or **Search**, or **App › Sign in**). It is saved in the same
`%APPDATA%\purpledisc\arl.txt` file as the TUI. The app includes the main
library, discovery, playback, queue, download and offline actions. A search
lists Deezer's top result first, then tracks, albums, artists and playlists.
Rows show their cover art, fetched through the engine's shared picture cache.
Every button, the field and both sliders have a tooltip naming what they do.
The menus are drawn in the app's font. The Playback menu holds the transport,
the sleep timer, shuffle, repeat, crossfade, episode speed, quality, gapless,
ReplayGain, mono and the equalizer presets; Go the pages and Back; App a new
playlist, Connect, the phone remote, offline mode, sign-out and a newer release
when there is one. A row's actions
(play, play next, queue, add to a playlist, like, download, keep offline) are on
its context menu (right-click, the Menu key or Shift+F10). Keys: Enter opens or
plays, Space plays or pauses, Backspace or Alt+Left goes back, Ctrl+F searches,
Ctrl+Left and Ctrl+Right skip, and the media keys work while the window has focus.
The window keeps its size, place and last page between runs.

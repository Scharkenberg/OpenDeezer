# OpenDeezer (Windows)

Native **WinUI 3 / C# (.NET 11, Windows App SDK 2.6) / Fluent** front-end for OpenDeezer — *an open
source reimplementation of Deezer*. The whole engine (login, browse, Blowfish
decrypt, MP3/FLAC decode, WASAPI playback) is the Go core compiled to a C-ABI
shared library (`lib/libdeezercore.dll`) and called in-process via **P/Invoke**;
this layer is UI only.

## Look

- **Fluent + Mica:** a Fluent `TitleBar` (back, pane toggle, search, account), a
  `NavigationView` pane (Home; Library: Liked songs, Playlists, Recent; Discover:
  Flow, Charts; Settings), a view host with the queue pane beside it, and the
  player bar. Back with the title-bar button, Alt+Left or mouse Back.
- **Deezer purple (`#A238FF`):** `App.xaml` overrides `SystemAccentColor` (and its
  light/dark tints), so every themed control uses the one accent.
- **Real XAML markup compiler:** `App.xaml` is compiled and merges
  `XamlControlsResources`, so the WinUI theme dictionary resolves and the app
  launches. (The previous code-only C++/WinRT build never merged it, so the first
  theme-brush lookup threw `0x80004005` at startup.) The UI tree is built in C#
  from a small kit (`Ui/`); `App.xaml` holds resources only.

## Build & run

```powershell
gui\windows\build.ps1
```

`build.ps1` (1) builds the Go engine DLL (`go build -buildmode=c-shared ./corelib`
→ `gui/windows/lib/libdeezercore.dll`), (2) runs `dotnet publish OpenDeezer.csproj
-c Release -r win-x64 --self-contained -p:WindowsPackageType=None`, and (3) copies
the DLL next to `OpenDeezer.exe` in the publish dir
(`bin\Release\net11.0-windows10.0.26100.0\win-x64\publish\`).

### Prerequisites

- **.NET 11 SDK** on PATH (`dotnet`).
- **Go 1.27+** (`C:\Program Files\Go`; it need not be on PATH in an old shell).
- A C compiler for cgo (the engine exports a C ABI; the audio backend is
  miniaudio). Either:
  - **Visual Studio's clang** (recommended, no MinGW): the VS component
    *C++ Clang Compiler for Windows* (`Microsoft.VisualStudio.Component.VC.Llvm.Clang`),
    plus the MSVC build tools and a Windows SDK. Found automatically via `vswhere`.
  - or **MinGW-w64 gcc** on PATH (what the CI runners use, with Go 1.25).

`go-env.ps1` sets this up for the current shell only (no machine-wide `CC`):

```powershell
. gui\windows\go-env.ps1   # go on PATH, CC=clang (VS), CGO_ENABLED=1, static C runtime
go test ./...
go vet ./...
```

With VS clang the C runtime is linked statically, so `libdeezercore.dll` imports
only `KERNEL32.dll` (no Visual C++ redistributable, no UCRT forwarders).
`corelib/msvc_init_windows.c` (Windows-only by its file name) is required for that toolchain: Go starts a c-shared DLL's
runtime from a GNU `.ctors` entry, which the MSVC C runtime never runs; the file
registers the same entry in `.CRT$XCU` (MSVC targets only).

No MSBuild / import-lib step is required: P/Invoke loads `libdeezercore.dll` by
name at runtime. The app is **unpackaged** and **self-contained**
(`WindowsPackageType=None`, `WindowsAppSDKSelfContained=true`).

Debug builds of the app load `gui/windows/libdeezercore.dll` (copied to the
output by the csproj); copy `lib\libdeezercore.dll` there to run a fresh engine
build under the debugger.
## Login

On first launch the app shows the sign-in view with **"Continue with Deezer"**.
Picking it opens a **WebView2** view (`Microsoft.UI.Xaml.Controls.WebView2`,
profile in `%LOCALAPPDATA%\opendeezer\WebView2`, cookies wiped first) pointed at the
Deezer web login; once you sign in, OpenDeezer reads the `CoreWebView2`
cookie store (`CookieManager.GetCookiesAsync`) for the `arl` cookie — which is
`HttpOnly`, so it is only readable there, not via `document.cookie` — captures it
automatically, starts the session with `DZInit`, and writes it to
`%APPDATA%\opendeezer\arl.txt` so the next launch logs in automatically. No manual
ARL copy needed.

This needs the **Microsoft Edge WebView2 Runtime** (the Evergreen runtime ships
with Windows 11 / current Edge and is preinstalled on the CI runner). The WebView2
projection ships transitively with the Windows App SDK — no separate NuGet
package.

### Manual ARL (fallback)

You can still sign in by ARL directly: choose **"Use an ARL token"** on the
sign-in view, or pre-seed `%DEEZER_ARL%` / `%APPDATA%\opendeezer\arl.txt` before
launch (a failing `%DEEZER_ARL%` falls back to `arl.txt`). The account picture in
the title bar shows who is signed in, switches account, or signs out.
Your ARL is the `arl` cookie from an authenticated `deezer.com` session — treat it
like a password.

## Architecture

```
gui/windows/
  Program.cs                    custom Main: single instance + crash-safe UI SynchronizationContext
  App.xaml / App.xaml.cs        resources (accent, text, surface and button styles); services, restart, AppLog
  Shell/                        MainWindow (title bar, pane, view host), Navigator, PlayerBar, QueuePane,
                                SearchBox, Shortcuts, WindowBounds, AccountPanel, Tray, MediaControls (SMTC)
  Views/                        View base and one class per page (SignIn, WebSignIn, Home, Collection,
                                Playlists, Charts, Search, Recent, Artist, Lyrics, Settings, Equalizer);
                                Settings parts (SettingsParts, RemoteControl, AboutSettings)
  Ui/                           Tokens, Icons, Ui factories, Recycler, StateView, Toast, Section, Hero,
                                TrackRow/TrackList, MediaTile/TileGrid/Rail, Menus, Dialogs, Converters, Fmt
  Services/                     Player (+ .Queue, .Poll), Library, Session, Config, Collection
  Engine/                       wire DTOs + JsonSerializerContext, Engine.Post, LatestValuePump
  DeezerCore.cs                 [LibraryImport] bridge to libdeezercore.dll (DZ* exports)
  Interop.cs                    Win32 tray (Shell_NotifyIcon), DPI, single-instance wait
  Loc.cs                        MRT Core string lookup (cached), plurals, RTL
  OpenDeezer.csproj             unpackaged, self-contained C# WinUI 3 (.NET 11) project
  app.manifest                  per-monitor-v2 DPI awareness, asInvoker ($VERSION$ stamped from <Version> at build)
  EngineQueueMirror.cs          engine queue mirror over IEngineQueue (no XAML; unit-tested)
  EngineQueueNative.cs          IEngineQueue -> DeezerCore
  Track.cs                      track model (shared with the tests)
  ../windows.tests/             xunit tests for EngineQueueMirror (dotnet test)
  build.ps1                     Go DLL -> dotnet publish -> copy DLL
  go-env.ps1                    per-shell Go + cgo setup (VS clang or MinGW), used by build.ps1
  lib/                          generated libdeezercore.dll (git-ignored)
```

The C API is defined in `../../corelib/deezercore.go`
(`go build -buildmode=c-shared`). Every blocking `DZ*` call runs on the thread pool
via `await Task.Run(...)`; because the callers start on the UI thread (which carries
the `DispatcherQueueSynchronizationContext`), the code after each `await` resumes
back on the UI thread. Engine state is read on a background loop (250 ms while
playing with the window visible, 1 s otherwise, 5 s after repeated failures) into an
immutable snapshot; the UI thread applies only what changed. Every engine queue
mutation runs on one ordered chain, and remote edits are pulled back via
`DZQueueVersion`. Errors are logged to `%APPDATA%\opendeezer\windows.log`, which moves to
`windows.log.old` at 512 KB.

Char\* results from the engine are UTF-8 and freed with `DZFree` (see
`DeezerCore.TakeJson`). Artwork is loaded by XAML straight from the CDN URL,
decoded at display size.

## License

AGPL-3.0. By Cycl0o0.

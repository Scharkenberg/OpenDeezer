# Flathub submission — Purple Disc (unified Qt/KDE build)

This directory holds the files needed to publish Purple Disc on Flathub. The
submitted app is the **unified** Linux client (`dev.scharkenberg.PurpleDisc.unified`,
Qt6/KDE runtime, GTK desktops run it fine too).

## Files

| File | Purpose |
| --- | --- |
| `dev.scharkenberg.PurpleDisc.unified.yaml` | the Flathub manifest (offline build, pinned commit) |
| `dev.scharkenberg.PurpleDisc.unified.metainfo.xml` | required AppStream metadata |
| `gen-and-build.sh` | Linux helper: generate `go.mod.yml`, validate, build + install |
| `go.mod.yml` | **generated** offline Go module sources (run the helper) |

The desktop file (`packaging/flatpak/dev.scharkenberg.PurpleDisc.unified.desktop`)
and icon (`assets/icon.png`) already exist in the repo and are installed by the
manifest.

## Before you submit — three TODOs

1. **Screenshots.** Flathub requires ≥1 hosted screenshot. Capture the app, host
   the PNGs, list them under `<screenshots>` in the metainfo and make sure the URLs
   load over HTTPS. Aspect ratio 1:1 to 2:1.
2. **Generate + test the build on Linux** (flatpak-builder does not run on macOS —
   use your OrbStack Ubuntu box):
   ```sh
   packaging/flatpak/flathub/gen-and-build.sh
   flatpak run dev.scharkenberg.PurpleDisc.unified
   ```
   This writes `go.mod.yml`, validates the metainfo, and builds **offline**
   (`--disable-download`) to prove no build-time network — a hard Flathub rule.
   Commit the generated `go.mod.yml`.
3. **Bump the pinned `commit:`** in the manifest to whatever tag you submit.

## Submitting

1. Fork <https://github.com/flathub/flathub> and create a branch off the
   **`new-pr`** branch.
2. Add the manifest, metainfo and `go.mod.yml` (and any patches) to the branch.
3. Open a PR **against `new-pr`**. The Flathub build bot compiles it; a reviewer
   checks it. Address feedback.
4. On approval Flathub creates a dedicated repo `flathub/dev.scharkenberg.PurpleDisc.unified`
   you get push access to — pushes there trigger the official builds.

## ⚠️ Content-policy caveat

Flathub reviewers may reject apps that facilitate ToS/copyright violations. A
Deezer stream/download client is a plausible rejection on those grounds — the
packaging here is technically complete, but approval is not guaranteed.

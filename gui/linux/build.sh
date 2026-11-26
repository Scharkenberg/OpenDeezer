#!/usr/bin/env bash
# Build the unified Linux client: both toolkit backends as shared libraries plus
# the dlopen launcher, assembled into dist/. Run dist/purpledisc — it picks the
# Qt backend on KDE-family desktops and the GTK backend elsewhere.
#
# Deps: Go 1.27+, gcc, meson+ninja, GTK4/libadwaita/json-glib dev, Qt6 dev,
#       libasound2-dev. (Same as the two standalone GUIs combined.)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"   # repo root: module github.com/Scharkenberg/PurpleDisc/v4
DIST="$HERE/dist"
rm -rf "$DIST"; mkdir -p "$DIST"

echo "==> [1/5] Go engine c-archive"
mkdir -p "$ROOT/gui/gnome/lib" "$ROOT/gui/kde/lib"
( cd "$ROOT" && CGO_ENABLED=1 go build -buildmode=c-archive \
    -o gui/gnome/lib/libdeezercore.a ./corelib )
cp "$ROOT/gui/gnome/lib/libdeezercore.a" "$ROOT/gui/kde/lib/libdeezercore.a"
cp "$ROOT/gui/gnome/lib/libdeezercore.h" "$ROOT/gui/kde/lib/libdeezercore.h"

echo "==> [2/5] GTK backend (libpurpledisc-gtk.so)"
( cd "$ROOT/gui/gnome"
  if [ -d build ]; then meson setup --reconfigure build .; else meson setup build .; fi
  meson compile -C build )
cp "$ROOT/gui/gnome/build/libpurpledisc-gtk.so" "$DIST/"

echo "==> [3/5] Qt backend (libpurpledisc-qt.so)"
( cd "$ROOT/gui/kde"
  cmake -S . -B build -DCMAKE_BUILD_TYPE=Release >/dev/null
  cmake --build build -j "$(nproc 2>/dev/null || echo 2)" --target purpledisc-qt purpledisc-login )
cp "$ROOT/gui/kde/build/libpurpledisc-qt.so" "$DIST/"
# Standalone login webview helper — the launcher/app spawns it (QtWebEngine
# out-of-process so it works despite the backend being dlopen'd).
cp "$ROOT/gui/kde/build/purpledisc-login" "$DIST/"

echo "==> [4/5] launcher"
cc -O2 -o "$DIST/purpledisc" "$HERE/launcher.c" -ldl -Wl,-rpath,'$ORIGIN'

echo "==> [5/5] desktop entry + icon"
cp "$HERE/dev.scharkenberg.PurpleDisc.desktop" "$DIST/"
# Basename must match the desktop file's Icon= key for icon-theme lookup.
cp "$ROOT/assets/icon.png" "$DIST/dev.scharkenberg.PurpleDisc.png"
# Inter for the GTK backend, which reads fonts/ beside the executable (Qt embeds it).
mkdir -p "$DIST/fonts"
cp "$ROOT/assets/fonts/InterVariable.ttf" "$ROOT/assets/fonts/Inter-OFL.txt" "$DIST/fonts/"

echo "==> done -> $DIST  (run: $DIST/purpledisc)"

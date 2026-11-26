#!/usr/bin/env bash
# Build the GNOME front-end end-to-end:
#   1. compile the Go engine to a C static archive (lib/libdeezercore.{a,h})
#   2. configure + compile the GTK4/libadwaita app with meson + ninja
#   3. drop the binary next to this script as ./purpledisc-gnome
#
# Usage:  cd gui/gnome && ./build.sh && ./purpledisc-gnome
#
# Build deps (Debian/Ubuntu):
#   golang gcc pkg-config meson ninja-build gettext \
#   libgtk-4-dev libadwaita-1-dev libjson-glib-dev libasound2-dev \
#   libwebkitgtk-6.0-dev    # embedded Deezer login (pulls in libsoup-3.0-dev)
# (gettext provides msgfmt/xgettext, required by meson's i18n module for the
#  po/ message catalogs and the localized .desktop file.)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"   # repo root: module github.com/Scharkenberg/PurpleDisc/v4

echo "==> [1/3] building Go core archive (corelib -> lib/libdeezercore.a)"
mkdir -p "$HERE/lib"
( cd "$ROOT" && CGO_ENABLED=1 go build -buildmode=c-archive \
    -o gui/gnome/lib/libdeezercore.a ./corelib )

echo "==> [2/3] configuring meson"
if [ ! -d "$HERE/build" ]; then
  meson setup "$HERE/build" "$HERE"
else
  meson setup --reconfigure "$HERE/build" "$HERE"
fi

echo "==> [3/3] compiling"
meson compile -C "$HERE/build"

cp "$HERE/build/purpledisc-gnome" "$HERE/purpledisc-gnome"
# Inter beside the binary: the app reads fonts/ there when it was not installed by meson.
mkdir -p "$HERE/fonts"
cp "$ROOT/assets/fonts/InterVariable.ttf" "$ROOT/assets/fonts/Inter-OFL.txt" "$HERE/fonts/"
echo "==> done: $HERE/purpledisc-gnome"

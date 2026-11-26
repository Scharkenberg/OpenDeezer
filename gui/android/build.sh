#!/usr/bin/env bash
#
# Builds the Purple Disc Android app:
#   1. binds the Go engine (./mobile) into an Android AAR with gomobile, and
#   2. assembles the debug APK with Gradle.
#
# Requirements: Go 1.27, JDK 21, the Android SDK (platform 37.2, build-tools 37.0.0)
# and NDK r30 (ANDROID_HOME and ANDROID_NDK_HOME set), and an internet connection
# (the Gradle wrapper downloads Gradle 9.9.2 on first run). On Windows use build.ps1.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

TOOLS_DIR="$(mktemp -d "${TMPDIR:-/tmp}/purpledisc-mobile-tools.XXXXXX")"
trap 'rm -rf "$TOOLS_DIR"' EXIT
"$REPO_ROOT/scripts/build-mobile-tools.sh" "$TOOLS_DIR"
export PATH="$TOOLS_DIR:$PATH"

echo "==> Binding ./mobile -> app/libs/pdmobile.aar"
mkdir -p "$SCRIPT_DIR/app/libs"
# 64-bit ARM phones and x86_64 emulators (the workflows bind all four CPU types); 16 KB
# aligned ELF segments, as Android 15+ devices with 16 KB pages require; packed relocations
# (Android 6+) and the engine's CPU profile.
export CGO_LDFLAGS="-Wl,-z,max-page-size=16384 -Wl,--pack-dyn-relocs=android"
export GOFLAGS="-pgo=$REPO_ROOT/corelib/default.pgo"
( cd "$REPO_ROOT" && gomobile bind -trimpath -ldflags="-s -w" -target=android/arm64,android/amd64 -androidapi 24 \
    -o gui/android/app/libs/pdmobile.aar ./mobile )

echo "==> Assembling debug APKs (phone + Android TV flavors)"
( cd "$SCRIPT_DIR" && ./gradlew --no-daemon assembleMobileDebug assembleTvDebug )

echo "==> Done. APKs at:"
echo "    $SCRIPT_DIR/app/build/outputs/apk/mobile/debug/app-mobile-debug.apk  (phone/tablet)"
echo "    $SCRIPT_DIR/app/build/outputs/apk/tv/debug/app-tv-debug.apk          (Android TV)"

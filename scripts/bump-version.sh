#!/usr/bin/env bash
# bump-version.sh — bump the Purple Disc release number EVERYWHERE in one shot.
#
#   scripts/bump-version.sh            # minor bump: 1.7.0 -> 1.8.0
#   scripts/bump-version.sh patch      # 1.7.0 -> 1.7.1
#   scripts/bump-version.sh major      # 1.7.0 -> 2.0.0
#   scripts/bump-version.sh 2.1.3      # explicit target
#
# Single source of truth is internal/version/version.go (all Go surfaces —
# TUI, corelib, gomobile, MCP — read it). This script updates that plus every
# per-client build file and packaging manifest, and increments the platform
# build numbers (CFBundleVersion, versionCode, CURRENT_PROJECT_VERSION).
#
# It is idempotent: re-running with the same target only fixes stragglers.
# NOTE: winget's `ManifestVersion:` is the manifest SCHEMA version — never
# touched here (only PackageVersion / URLs are).
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION_GO=internal/version/version.go
CUR=$(sed -n 's/^const Number = "\(.*\)"$/\1/p' "$VERSION_GO")
[[ -n "$CUR" ]] || { echo "cannot read current version from $VERSION_GO" >&2; exit 1; }

IFS=. read -r MAJ MIN PAT <<<"$CUR"
case "${1:-minor}" in
  major) NEW="$((MAJ + 1)).0.0" ;;
  minor) NEW="$MAJ.$((MIN + 1)).0" ;;
  patch) NEW="$MAJ.$MIN.$((PAT + 1))" ;;
  *)     NEW="$1"
         [[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "bad version: $NEW" >&2; exit 1; } ;;
esac
echo "bump: $CUR -> $NEW"

# sub FILE SED_ARGS: apply in place (GNU and BSD sed), warn if the file
# doesn't exist.
sub() {
  local f=$1; shift
  [[ -f "$f" ]] || { echo "  ! missing: $f (skipped)" >&2; return 0; }
  sed -i.bak "$@" "$f" && rm -f "$f.bak"
  echo "  · $f"
}

# bumpnum FILE SED_CAPTURE — increment the first captured integer.
bumpnum() {
  local f=$1 pat=$2
  [[ -f "$f" ]] || { echo "  ! missing: $f (skipped)" >&2; return 0; }
  perl -0pi -e "s/$pat/\$1 . (\$2 + 1)/e" "$f"
  echo "  · $f (build number +1)"
}

# ---- single source of truth (all Go binaries) ----
sub "$VERSION_GO" "s/^const Number = \".*\"/const Number = \"$NEW\"/"

# ---- macOS ----
# CFBundleShortVersionString = NEW; CFBundleVersion += 1.
perl -0pi -e "s/(<key>CFBundleShortVersionString<\/key>\s*<string>)[^<]+/\${1}$NEW/" gui/macos/Info.plist
bumpnum gui/macos/Info.plist '(<key>CFBundleVersion<\/key>\s*<string>)(\d+)'
echo "  · gui/macos/Info.plist"

# ---- GNOME ----
sub gui/gnome/meson.build "s/version: '[0-9.]*'/version: '$NEW'/"
sub gui/gnome/src/main.c -E -e "s/(adw_about_dialog_set_version\([^,]*, )\"[0-9]+\.[0-9]+\.[0-9]+\"/\1\"$NEW\"/" \
  -e "s/\"version\", \"[0-9]+\.[0-9]+\.[0-9]+\"/\"version\", \"$NEW\"/"
for f in gui/gnome/po/*.po gui/gnome/po/*.pot; do
  sub "$f" "s/Project-Id-Version: purpledisc-gnome [0-9.]*/Project-Id-Version: purpledisc-gnome $NEW/"
done

# ---- KDE ----
sub gui/kde/src/mainwindow.cpp -E "s/Purple Disc for Deezer [0-9]+\.[0-9]+\.[0-9]+/Purple Disc for Deezer $NEW/g"

# ---- Windows ----
# <Version> in the csproj is the only source; the build stamps app.manifest from it.
sub gui/windows/PurpleDisc.csproj "s|<Version>[0-9.]*</Version>|<Version>$NEW</Version>|"

# ---- Android (phone + TV share the module) ----
sub gui/android/app/build.gradle.kts "s/versionName = \"[0-9.]*\"/versionName = \"$NEW\"/"
bumpnum gui/android/app/build.gradle.kts '(versionCode = )(\d+)'

# ---- iOS (project.yml; the Xcode project is generated from it) ----
sub gui/ios/project.yml "s/MARKETING_VERSION: \"[0-9.]*\"/MARKETING_VERSION: \"$NEW\"/"
bumpnum gui/ios/project.yml '(CURRENT_PROJECT_VERSION: ")(\d+)'

# ---- packaging manifests (checksums are filled at release time) ----
sub packaging/aur/PKGBUILD "s/^pkgver=.*/pkgver=$NEW/"
# The tag URL ends in "v<ver>.tar.gz" (a dot, not a trailing slash), so the old
# "/v$CUR/" pattern never matched and the source URL froze a release behind.
# Match the tag path value-agnostically instead.
sub packaging/aur/.SRCINFO -e "s/pkgver = .*/pkgver = $NEW/" -e "s|/refs/tags/v[0-9.]*\.tar\.gz|/refs/tags/v$NEW.tar.gz|g" -e "s/purpledisc-[0-9.]*\.tar\.gz/purpledisc-$NEW.tar.gz/g"
sub packaging/homebrew/purpledisc.rb -E "s/^(  version )\"[0-9]+\.[0-9]+\.[0-9]+\"/\1\"$NEW\"/"
sub packaging/scoop/purpledisc.json -E -e "s/^(    \"version\": )\"[0-9]+\.[0-9]+\.[0-9]+\"/\1\"$NEW\"/" -e "s|/v[0-9]+\.[0-9]+\.[0-9]+/|/v$NEW/|g"
sub packaging/snap/snapcraft.yaml -E "s/^version: \"[0-9]+\.[0-9]+\.[0-9]+\"/version: \"$NEW\"/"
for f in packaging/winget/*.yaml; do
  # PackageVersion + tag URLs only — ManifestVersion is the SCHEMA version.
  sub "$f" -e "s/^PackageVersion: .*/PackageVersion: $NEW/" -e "s|/v$CUR/|/v$NEW/|g" -e "s/ReleaseNotesUrl: \(.*\)v$CUR/ReleaseNotesUrl: \1v$NEW/"
done
for f in packaging/flatpak/*.yaml; do
  sub "$f" "s/tag: v[0-9.]*/tag: v$NEW/"
done

# ---- verify: nothing still carries the old version where it matters ----
echo
echo "leftover occurrences of $CUR (excluding CHANGELOG/history — review manually):"
grep -rn --exclude-dir=.git --exclude=CHANGELOG.md --exclude-dir=scripts -F "$CUR" \
  internal/version gui/macos/Info.plist gui/gnome/meson.build gui/gnome/po gui/kde/src/mainwindow.cpp \
  gui/windows/PurpleDisc.csproj gui/android/app/build.gradle.kts gui/ios/project.yml \
  packaging 2>/dev/null || echo "  (none)"
echo
echo "done: $NEW — remember to add a CHANGELOG.md entry and run: go build ./..."

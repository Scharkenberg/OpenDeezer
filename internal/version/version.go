// Package version holds the Purple Disc release number read by every Go surface (TUI,
// corelib, gomobile binding, MCP server). Bump it with scripts/bump-version.sh, which
// also updates the per-client build files.
package version

// Number is the current release, without a leading "v".
const Number = "4.0.0"

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	version_ "github.com/Scharkenberg/PurpleDisc/v4/internal/version"
)

// The Windows resource carries the release number: in purpledisc.rc and, after
// winres.ps1, in the compiled rsrc_windows_amd64.syso (UTF-16 there).
func TestWindowsResourceIsCurrent(t *testing.T) {
	rc, err := os.ReadFile("purpledisc.rc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rc), `"FileVersion", "`+version_.Number+`"`) {
		t.Fatalf("purpledisc.rc does not say version %s", version_.Number)
	}
	syso, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatal(err)
	}
	var wide []byte
	for _, r := range version_.Number {
		wide = append(wide, byte(r), 0)
	}
	if !bytes.Contains(syso, wide) {
		t.Fatalf("rsrc_windows_amd64.syso is not version %s: run winres.ps1", version_.Number)
	}
}

func TestReleaseVersion(t *testing.T) {
	for in, want := range map[string]string{
		"4.0.0":         "4.0.0",
		"v4.0.0-4-gabc": "v4.0.0-4-gabc",
		"8747c8e-dirty": version_.Number + " (8747c8e-dirty)",
		"abc1234":       version_.Number + " (abc1234)",
		"dev":           version_.Number + " (dev)",
	} {
		if got := releaseVersion(in); got != want {
			t.Errorf("releaseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

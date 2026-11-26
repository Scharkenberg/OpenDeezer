package main

import (
	"testing"

	version_ "github.com/Scharkenberg/PurpleDisc/v4/internal/version"
)

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

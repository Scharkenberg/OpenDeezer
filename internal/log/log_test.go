package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(LevelWarn)
	t.Cleanup(func() { SetOutput(discardReset()); SetLevel(LevelInfo) })

	Debug("d")
	Info("i")
	Warn("w-%d", 1)
	Error("e")

	out := buf.String()
	if strings.Contains(out, "d") && strings.Contains(out, "[DEBUG]") {
		t.Error("debug should be filtered at LevelWarn")
	}
	if strings.Contains(out, "[INFO]") {
		t.Error("info should be filtered at LevelWarn")
	}
	if !strings.Contains(out, "[WARN] w-1") {
		t.Errorf("warn missing: %q", out)
	}
	if !strings.Contains(out, "[ERROR] e") {
		t.Errorf("error missing: %q", out)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"debug": LevelDebug, "INFO": LevelInfo, "warn": LevelWarn,
		"error": LevelError, "off": LevelOff, "bogus": LevelInfo,
		"Debug": LevelDebug, "Warn": LevelWarn, "WARNING": LevelWarn,
		"Off": LevelOff,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
	// Surrounding whitespace is trimmed (kept out of the map above so the
	// linter doesn't flag a whitespace-only-differing key).
	if got := ParseLevel(" info "); got != LevelInfo {
		t.Errorf("ParseLevel with surrounding spaces = %v, want %v", got, LevelInfo)
	}
}

func TestLevelOffSilencesAll(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(LevelOff)
	t.Cleanup(func() { SetOutput(discardReset()); SetLevel(LevelInfo) })
	Error("should-not-appear")
	if buf.Len() != 0 {
		t.Errorf("LevelOff should silence everything, got %q", buf.String())
	}
}

// The log file moves to opendeezer.log.old once it passes 512 KB.
func TestLogFileRolls(t *testing.T) {
	dir := t.TempDir()
	if err := OpenFile(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mu.Lock()
		_ = out.(*rollingFile).f.Close()
		mu.Unlock()
		SetOutput(discardReset())
	})
	line := strings.Repeat("x", 1000)
	for range 600 { // ~600 KB
		Info("%s", line)
	}
	old, err := os.Stat(filepath.Join(dir, "opendeezer.log.old"))
	if err != nil {
		t.Fatalf("no rolled log: %v", err)
	}
	cur, err := os.Stat(filepath.Join(dir, "opendeezer.log"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Size() < 512*1024 || cur.Size() >= 512*1024 {
		t.Fatalf("old %d bytes, current %d bytes", old.Size(), cur.Size())
	}
}

// When the move fails (another process holds the file on Windows) the log
// tries again only after another 512 KB, not on every line.
func TestFailedRollRetriesAfterAnother512KB(t *testing.T) {
	dir := t.TempDir()
	tries := 0
	rename = func(string, string) error { tries++; return os.ErrPermission }
	if err := OpenFile(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rename = os.Rename
		mu.Lock()
		_ = out.(*rollingFile).f.Close()
		mu.Unlock()
		SetOutput(discardReset())
	})
	line := strings.Repeat("x", 1000)
	for range 1100 { // ~1.1 MB
		Info("%s", line)
	}
	if tries != 2 {
		t.Fatalf("%d moves tried over 1.1 MB, want 2 (past 512 KB and past 1 MB)", tries)
	}
}

// When the log cannot be opened again after a move, the next lines do not
// try the move again each.
func TestFailedReopenDoesNotRetryEachLine(t *testing.T) {
	dir := t.TempDir()
	tries := 0
	rename = func(from, to string) error {
		tries++
		err := os.Rename(from, to)
		_ = os.Mkdir(from, 0o700) // a folder where the log was: it cannot be opened
		return err
	}
	if err := OpenFile(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rename = os.Rename
		mu.Lock()
		_ = out.(*rollingFile).f.Close()
		mu.Unlock()
		SetOutput(discardReset())
	})
	line := strings.Repeat("x", 1000)
	for range 600 { // ~600 KB
		Info("%s", line)
	}
	if tries != 1 {
		t.Fatalf("%d moves tried after the log could not be opened, want 1", tries)
	}
}

// discardReset returns a throwaway writer to detach the test buffer.
func discardReset() *bytes.Buffer { return &bytes.Buffer{} }

// After a failed reopen the writes still count toward the next roll: another 512 KB brings
// a second try, and the log is written again.
func TestFailedReopenTriesAgainLater(t *testing.T) {
	dir := t.TempDir()
	tries := 0
	rename = func(from, to string) error {
		tries++
		err := os.Rename(from, to)
		if tries == 1 {
			_ = os.Mkdir(from, 0o700) // the first time a folder blocks the reopen
		}
		return err
	}
	if err := OpenFile(dir); err != nil {
		t.Fatal(err)
	}
	path := out.(*rollingFile).path
	t.Cleanup(func() {
		rename = os.Rename
		mu.Lock()
		_ = out.(*rollingFile).f.Close()
		mu.Unlock()
		SetOutput(discardReset())
	})
	line := strings.Repeat("x", 1000)
	for range 600 {
		Info("%s", line)
	}
	_ = os.Remove(path) // the folder goes; the next reopen works
	for range 600 {
		Info("%s", line)
	}
	if tries != 2 {
		t.Fatalf("%d moves tried, want 2", tries)
	}
	if fi, err := os.Stat(path); err != nil || fi.IsDir() || fi.Size() == 0 {
		t.Fatalf("no log written after the second try: %v", err)
	}
}

// Package log is a tiny leveled logger for OpenDeezer. It writes to an
// io.Writer (a file in normal use) rather than stdout/stderr so it never
// corrupts the Bubble Tea TUI. Levels are filtered by SetLevel.
package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Level is a severity threshold.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelOff
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "OFF"
	}
}

// ParseLevel maps a name (case-insensitive) to a Level; unknown => LevelInfo.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	case "off", "none":
		return LevelOff
	default:
		return LevelInfo
	}
}

var (
	mu    sync.Mutex
	out   io.Writer = io.Discard
	level           = LevelInfo
)

// SetOutput directs log lines to w.
func SetOutput(w io.Writer) {
	mu.Lock()
	out = w
	mu.Unlock()
}

// SetLevel sets the minimum level that is emitted.
func SetLevel(l Level) {
	mu.Lock()
	level = l
	mu.Unlock()
}

// OpenFile points logging at <dir>/opendeezer.log (appended, 0600), honoring the
// OPENDEEZER_LOG env var ("debug"/"info"/...) for the level. Logging is a no-op
// (discard) on failure.
func OpenFile(dir string) error {
	if v := os.Getenv("OPENDEEZER_LOG"); v != "" {
		SetLevel(ParseLevel(v))
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	r := &rollingFile{path: filepath.Join(dir, "opendeezer.log")}
	if err := r.open(); err != nil {
		return err
	}
	SetOutput(r)
	return nil
}

// maxLogBytes is the size at which the log moves to opendeezer.log.old, as
// the Windows app's own log does.
const maxLogBytes = 512 * 1024

// rename is os.Rename, indirected so a test can make the move fail.
var rename = os.Rename

// rollingFile appends to path and moves it to path.old once it passes
// maxLogBytes. Written under mu.
type rollingFile struct {
	path string
	f    *os.File
	size int64
	// rollAt is the size at which the next move is tried: maxLogBytes, or
	// another maxLogBytes further after a move or the reopening after it
	// failed.
	rollAt int64
}

func (r *rollingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	r.f, r.size = f, 0
	if fi, err := f.Stat(); err == nil {
		r.size = fi.Size()
	}
	r.rollAt = maxLogBytes
	return nil
}

func (r *rollingFile) Write(p []byte) (int, error) {
	if r.size > r.rollAt {
		_ = r.f.Close()
		// Another process (the TUI or the app) may hold the file open, which
		// makes the rename fail on Windows: the log then grows on, and the
		// move is tried again another maxLogBytes later.
		moved := rename(r.path, r.path+".old") == nil
		if err := r.open(); err != nil {
			r.rollAt = r.size + maxLogBytes
			return 0, err
		}
		if !moved {
			r.rollAt = r.size + maxLogBytes
		}
	}
	n, err := r.f.Write(p)
	if err != nil {
		r.size += int64(len(p)) // a closed file writes nothing: count it, so the reopen is tried again
	} else {
		r.size += int64(n)
	}
	return n, err
}

func logf(l Level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if l < level || level == LevelOff {
		return
	}
	fmt.Fprintf(out, "[%s] %s\n", l, fmt.Sprintf(format, args...))
}

// Debug / Info / Warn / Error emit at their level.
func Debug(format string, args ...any) { logf(LevelDebug, format, args...) }
func Info(format string, args ...any)  { logf(LevelInfo, format, args...) }
func Warn(format string, args ...any)  { logf(LevelWarn, format, args...) }
func Error(format string, args ...any) { logf(LevelError, format, args...) }

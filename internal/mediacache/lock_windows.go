package mediacache

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockExclusive takes an exclusive lock on f without waiting; it fails while
// another handle, in this process or another, holds one. Closing f releases
// it.
func lockExclusive(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}

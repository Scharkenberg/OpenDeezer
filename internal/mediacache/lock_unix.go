//go:build unix

package mediacache

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockExclusive takes an exclusive lock on f without waiting; it fails while
// another open file, in this process or another, holds one. Closing f
// releases it.
func lockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

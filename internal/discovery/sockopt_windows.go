//go:build windows

package discovery

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// setReusePort: Windows has no SO_REUSEPORT; SO_REUSEADDR allows several
// sockets to bind the same UDP port (and receive multicast).
func setReusePort(c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	}); err != nil {
		return err
	}
	return serr
}

// setBroadcast enables SO_BROADCAST for the limited-broadcast fallback.
func setBroadcast(c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return serr
}

// retryableRead reports whether a failed read leaves the socket usable: an
// oversized datagram or the reset an ICMP port-unreachable reports.
func retryableRead(err error) bool {
	return errors.Is(err, windows.WSAEMSGSIZE) || errors.Is(err, windows.WSAECONNRESET)
}

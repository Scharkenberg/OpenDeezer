package ui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// spiGetScreenReader is SPI_GETSCREENREADER: whether a screen reader is running.
const spiGetScreenReader = 0x0046

var procSystemParametersInfo = windows.NewLazySystemDLL("user32.dll").NewProc("SystemParametersInfoW")

// screenReaderRunning reports whether Windows says a screen reader is running
// (Narrator and NVDA set the flag).
func screenReaderRunning() bool {
	if procSystemParametersInfo.Find() != nil {
		return false
	}
	var on int32
	r, _, _ := procSystemParametersInfo.Call(spiGetScreenReader, 0, uintptr(unsafe.Pointer(&on)), 0)
	return r != 0 && on != 0
}

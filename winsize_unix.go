//go:build !windows
// +build !windows

package pty

import (
	"os"
	"syscall"
	"unsafe"
)

// Winsize describes the terminal size.
type Winsize struct {
	Rows uint16 // ws_row: Number of rows (in cells).
	Cols uint16 // ws_col: Number of columns (in cells).
	X    uint16 // ws_xpixel: Width in pixels.
	Y    uint16 // ws_ypixel: Height in pixels.
}

// Setsize resizes t to s.
func Setsize(t Pty, ws *Winsize) error {
	//nolint:gosec // Expected unsafe pointer for Syscall call.
	return ioctlPty(t, syscall.TIOCSWINSZ, unsafe.Pointer(ws))
}

// GetsizeFull returns the full terminal size description.
func GetsizeFull(t Pty) (size *Winsize, err error) {
	var ws Winsize

	//nolint:gosec // Expected unsafe pointer for Syscall call.
	if err := ioctlPty(t, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return nil, err
	}
	return &ws, nil
}

func ioctlPty(t Pty, cmd uintptr, ptr unsafe.Pointer) error {
	if f, ok := t.(*os.File); ok {
		return ioctl(f, cmd, ptr)
	}
	return ioctlInner(t.Fd(), cmd, ptr)
}

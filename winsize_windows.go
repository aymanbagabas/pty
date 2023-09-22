//go:build windows
// +build windows

package pty

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Setsize resizes t to ws.
func Setsize(t File, ws *Winsize) error {
	pty, ok := t.(*conPty)
	if !ok {
		return errors.New("not a pty")
	}

	pty.mtx.RLock()
	defer pty.mtx.RUnlock()

	if pty.handle == 0 {
		return ErrClosed
	}

	coord := windows.Coord{X: int16(ws.Cols), Y: int16(ws.Rows)}
	if err := windows.ResizePseudoConsole(pty.handle, coord); err != nil {
		return err
	}

	return nil
}

// GetsizeFull returns the full terminal size description.
func GetsizeFull(t File) (size *Winsize, err error) {
	pty, ok := t.(*conPty)
	if !ok {
		return nil, errors.New("not a pty")
	}

	pty.mtx.RLock()
	defer pty.mtx.RUnlock()

	if pty.handle == 0 {
		return nil, ErrClosed
	}

	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(t.Fd()), &info); err != nil {
		return nil, err
	}

	return &Winsize{
		Rows: uint16(info.Window.Bottom - info.Window.Top + 1),
		Cols: uint16(info.Window.Right - info.Window.Left + 1),
	}, nil
}

// GetsizeFull returns the full terminal size description.
// func GetsizeFull(t File) (size *Winsize, err error) {
// 	pty, ok := t.(*conPty)
// 	if !ok {
// 		return nil, errors.New("not a pty")
// 	}

// 	pty.mtx.RLock()
// 	defer pty.mtx.RUnlock()

// 	if pty.handle == 0 {
// 		return nil, ErrClosed
// 	}

// 	var info windows.ConsoleScreenBufferInfo
// 	if err := getConsoleScreenBufferInfo(windows.Handle(t.Fd()), &info); err != nil {
// 		return nil, err
// 	}

// 	return &Winsize{
// 		Rows: uint16(info.Window.Bottom - info.Window.Top + 1),
// 		Cols: uint16(info.Window.Right - info.Window.Left + 1),
// 	}, nil
// }

// var (
// 	kernel32DLL                    = windows.NewLazyDLL("kernel32.dll")
// 	procGetConsoleScreenBufferInfo = kernel32DLL.NewProc("GetConsoleScreenBufferInfo")
// )

// // GetsizeFull returns the full terminal size description.
// func GetsizeFull(t File) (size *Winsize, err error) {
// 	err = procGetConsoleScreenBufferInfo.Find()
// 	if err != nil {
// 		return nil, err
// 	}

// 	var info windows.ConsoleScreenBufferInfo
// 	var r0 uintptr

// 	r0, _, err = procGetConsoleScreenBufferInfo.Call(t.Fd(), uintptr(unsafe.Pointer(&info)))
// 	if int32(r0) < 0 {
// 		if r0&0x1fff0000 == 0x00070000 {
// 			r0 &= 0xffff
// 		}

// 		// S_OK: 0
// 		return nil, syscall.Errno(r0)
// 	}

// 	return &Winsize{
// 		Rows: uint16(info.Window.Bottom - info.Window.Top + 1),
// 		Cols: uint16(info.Window.Right - info.Window.Left + 1),
// 	}, nil
// }

//go:build windows
// +build windows

package pty

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Winsize is a dummy struct to enable compilation on unsupported platforms.
type Winsize struct {
	Rows, Cols, X, Y uint16
}

// Setsize resizes t to s.
func Setsize(f File, size *Winsize) error {
	p, ok := f.(*conFile)
	if !ok {
		return ErrNotPty
	}

	return setsize(p.conPty, size)
}

func setsize(p *conPty, size *Winsize) error {
	// hold the lock, so we don't race with anyone trying to close the console
	p.closeMutex.Lock()
	defer p.closeMutex.Unlock()
	if p.closed || p.console == windows.InvalidHandle {
		return ErrClosed
	}

	// Taken from: https://github.com/microsoft/hcsshim/blob/54a5ad86808d761e3e396aff3e2022840f39f9a8/internal/winapi/zsyscall_windows.go#L144
	ret, _, err := procResizePseudoConsole.Call(uintptr(p.console), uintptr(*((*uint32)(unsafe.Pointer(&windows.Coord{
		Y: int16(size.Rows),
		X: int16(size.Cols),
	})))))
	if windows.Handle(ret) != windows.S_OK {
		return err
	}
	return nil
}

// GetsizeFull returns the full terminal size description.
func GetsizeFull(f File) (size *Winsize, err error) {
	p, ok := f.(*conFile)
	if !ok {
		return nil, ErrUnsupported
	}

	return getsizeFull(p.conPty)
}

func getsizeFull(p *conPty) (size *Winsize, err error) {
	// hold the lock, so we don't race with anyone trying to close the console
	p.closeMutex.Lock()
	defer p.closeMutex.Unlock()
	if p.closed || p.console == windows.InvalidHandle {
		return nil, ErrClosed
	}

	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(p.console, &info); err != nil {
		return nil, err
	}

	w, h := int(info.Window.Right-info.Window.Left+1), int(info.Window.Bottom-info.Window.Top+1)

	return &Winsize{
		Rows: uint16(h),
		Cols: uint16(w),
	}, nil
}

//go:build windows
// +build windows

package pty

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	_PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x20016
)

type conPty struct {
	handle windows.Handle
	r, w   *os.File
	closer func(*conPty) error
	name   string
}

var (
	// NOTE(security): as noted by the comment of syscall.NewLazyDLL and syscall.LoadDLL
	// 	user need to call internal/syscall/windows/sysdll.Add("kernel32.dll") to make sure
	//  the kernel32.dll is loaded from windows system path
	//
	// ref: https://pkg.go.dev/syscall@go1.13?GOOS=windows#LoadDLL
	kernel32DLL = windows.NewLazyDLL("kernel32.dll")

	// https://docs.microsoft.com/en-us/windows/console/createpseudoconsole
	createPseudoConsole = kernel32DLL.NewProc("CreatePseudoConsole")
	closePseudoConsole  = kernel32DLL.NewProc("ClosePseudoConsole")

	resizePseudoConsole        = kernel32DLL.NewProc("ResizePseudoConsole")
	getConsoleScreenBufferInfo = kernel32DLL.NewProc("GetConsoleScreenBufferInfo")
)

func open() (_ *conPty, _ *conPty, err error) {
	pr, consoleW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}

	consoleR, pw, err := os.Pipe()
	if err != nil {
		_ = consoleW.Close()
		_ = pr.Close()
		return nil, nil, err
	}

	var consoleHandle windows.Handle

	err = procCreatePseudoConsole(windows.Handle(consoleR.Fd()), windows.Handle(consoleW.Fd()),
		0, &consoleHandle)
	if err != nil {
		_ = consoleW.Close()
		_ = pr.Close()
		_ = pw.Close()
		_ = consoleR.Close()
		return nil, nil, err
	}

	// These pipes can be closed here without any worry
	err = consoleW.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to close pseudo console handle: %w", err)
	}

	err = consoleR.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to close pseudo console handle: %w", err)
	}

	return &conPty{
			handle: consoleHandle,
			r:      pr,
			w:      pw,
			closer: winPtyCloser,
			name:   "",
		}, &conPty{
			handle: consoleHandle,
			r:      consoleR,
			w:      consoleW,
			closer: winTtyCloser,
			// See: https://github.com/PowerShell/openssh-portable/blob/latestw_all/contrib/win32/win32compat/win32_sshpty.c#L36
			name: "windows-pty",
		}, nil
}

func (p *conPty) Name() string {
	return p.name
}

func (p *conPty) Fd() uintptr {
	return uintptr(p.handle)
}

func (p *conPty) Read(data []byte) (int, error) {
	return p.r.Read(data)
}

func (p *conPty) Write(data []byte) (int, error) {
	return p.w.Write(data)
}

func (p *conPty) Close() error {
	return p.closer(p)
}

func winPtyConsoleCloser(p *conPty) error {
	if p.handle != windows.InvalidHandle {
		err := closePseudoConsole.Find()
		if err != nil {
			return err
		}

		_, _, err = closePseudoConsole.Call(uintptr(p.handle))

		p.handle = windows.InvalidHandle

		return err
	}

	return nil
}

func winPtyCloser(p *conPty) error {
	_ = p.r.Close()
	_ = p.w.Close()

	return winPtyConsoleCloser(p)
}

func winTtyCloser(t *conPty) error {
	_ = t.r.Close()
	return t.w.Close()
}

func procCreatePseudoConsole(hInput windows.Handle, hOutput windows.Handle, dwFlags uint32, consoleHandle *windows.Handle) error {
	var r0 uintptr
	var err error

	err = createPseudoConsole.Find()
	if err != nil {
		return err
	}

	r0, _, err = createPseudoConsole.Call(
		(windowsCoord{X: 80, Y: 30}).Pack(),    // size: default 80x30 window
		uintptr(hInput),                        // console input
		uintptr(hOutput),                       // console output
		uintptr(dwFlags),                       // console flags, currently only PSEUDOCONSOLE_INHERIT_CURSOR supported
		uintptr(unsafe.Pointer(consoleHandle)), // console handler value return
	)

	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}

		// S_OK: 0
		return syscall.Errno(r0)
	}

	return nil
}

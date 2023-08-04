//go:build windows
// +build windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procResizePseudoConsole = kernel32.NewProc("ResizePseudoConsole")
	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")
)

func open() (*conFile, *conFile, error) {
	pty, err := openPty()
	if err != nil {
		return nil, nil, err
	}

	return pty.ptyf(), pty.ttyf(), nil
}

// See: https://docs.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session
func openPty() (*conPty, error) {
	// We use the CreatePseudoConsole API which was introduced in build 17763
	vsn := windows.RtlGetVersion()
	if vsn.MajorVersion < 10 ||
		vsn.BuildNumber < 17763 {
		// If the CreatePseudoConsole API is not available, we fall back to a simpler
		// implementation that doesn't create an actual PTY - just uses os.Pipe
		return nil, errors.New("pty not supported")
	}

	var err error
	pty := &conPty{}
	pty.pr, pty.consolew, err = os.Pipe()
	if err != nil {
		return nil, err
	}
	pty.consoler, pty.pw, err = os.Pipe()
	if err != nil {
		_ = pty.consolew.Close()
		_ = pty.pr.Close()
		return nil, err
	}

	consoleSize := uintptr(80) + (uintptr(80) << 16)
	ret, _, err := procCreatePseudoConsole.Call(
		consoleSize,
		uintptr(windows.Handle(pty.consoler.Fd())),
		uintptr(windows.Handle(pty.consoler.Fd())),
		0,
		uintptr(unsafe.Pointer(&pty.console)),
	)
	// CreatePseudoConsole returns S_OK on success, as per:
	// https://learn.microsoft.com/en-us/windows/console/createpseudoconsole
	if windows.Handle(ret) != windows.S_OK {
		_ = pty.consolew.Close()
		_ = pty.pr.Close()
		_ = pty.pw.Close()
		_ = pty.consoler.Close()
		return nil, fmt.Errorf("create pseudo console (%d): %w", int32(ret), err)
	}

	// These pipes can be closed here without any worry
	// err = pty.consolew.Close()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to close pseudo console handle: %w", err)
	// }

	// err = pty.consoler.Close()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to close pseudo console handle: %w", err)
	// }

	return pty, nil
}

type conPty struct {
	console windows.Handle

	consoler *os.File
	consolew *os.File
	pr       *os.File
	pw       *os.File

	closeMutex sync.Mutex
	closed     bool
}

func (p *conPty) Fd() uintptr {
	return uintptr(p.console)
}

func (p *conPty) Name() string {
	return "windows-pty"
}

func (p *conPty) Close() error {
	p.closeMutex.Lock()
	defer p.closeMutex.Unlock()
	if p.closed {
		return nil
	}

	// Close the pseudo console, this will also terminate the process attached
	// to this pty. If it was created via Start(), this also unblocks close of
	// the readers below.
	err := p.closeConsoleNoLock()
	if err != nil {
		return err
	}

	// Only set closed after the console has been successfully closed.
	p.closed = true

	// Close the pipes ensuring that the writer is closed before the respective
	// reader, otherwise closing the reader may block indefinitely. Note that
	// outputWrite and inputRead are unset when we Start() a new process.
	if p.pw != nil {
		_ = p.pw.Close()
	}
	_ = p.consoler.Close()
	_ = p.consolew.Close()
	if p.pr != nil {
		_ = p.pr.Close()
	}
	return nil
}

func (p *conPty) ptyf() *conFile {
	return &conFile{
		conPty: p,
		master: true,
		r:      p.pr,
		w:      p.pw,
	}
}

func (p *conPty) ttyf() *conFile {
	return &conFile{
		r: p.consoler,
		w: p.consolew,
	}
}

type conFile struct {
	*conPty
	master bool
	r      *os.File
	w      *os.File
}

func (p *conFile) Close() error {
	err := errors.Join(p.r.Close(), p.w.Close())
	if p.master {
		err = errors.Join(err, p.conPty.closeConsoleNoLock())
	}

	return err
}

func (p *conFile) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

func (p *conFile) Write(b []byte) (int, error) {
	return p.w.Write(b)
}

func (p *conFile) Fd() uintptr {
	return uintptr(p.console)
}

type winProc struct {
	// cmdDone protects access to cmdErr: anything reading cmdErr should read from cmdDone first.
	cmdDone chan interface{}
	cmdErr  error
	proc    *os.Process
	pw      *conPty
}

func (p *winProc) waitInternal() {
	// put this on the bottom of the defer stack since the next defer can write to p.cmdErr
	defer close(p.cmdDone)
	defer func() {
		// close the pseudoconsole handle when the process exits, if it hasn't already been closed.
		// this is important because the PseudoConsole (conhost.exe) holds the write-end
		// of the output pipe.  If it is not closed, reads on that pipe will block, even though
		// the command has exited.
		// c.f. https://devblogs.microsoft.com/commandline/windows-command-line-introducing-the-windows-pseudo-console-conpty/
		p.pw.closeMutex.Lock()
		defer p.pw.closeMutex.Unlock()

		err := p.pw.closeConsoleNoLock()
		// if we already have an error from the command, prefer that error
		// but if the command succeeded and closing the PseudoConsole fails
		// then record that error so that we have a chance to see it
		if err != nil && p.cmdErr == nil {
			p.cmdErr = err
		}
	}()

	state, err := p.proc.Wait()
	if err != nil {
		p.cmdErr = err
		return
	}
	if !state.Success() {
		p.cmdErr = &exec.ExitError{ProcessState: state}
		return
	}
}

func (p *winProc) Wait() error {
	<-p.cmdDone
	return p.cmdErr
}

func (p *winProc) Kill() error {
	return p.proc.Kill()
}

// closeConsoleNoLock closes the console handle, and sets it to
// windows.InvalidHandle. It must be called with p.closeMutex held.
func (p *conPty) closeConsoleNoLock() error {
	// if we are running a command in the PTY, the corresponding *windowsProcess
	// may have already closed the PseudoConsole when the command exited, so that
	// output reads can get to EOF.  In that case, we don't need to close it
	// again here.
	if p.console != windows.InvalidHandle {
		// ClosePseudoConsole has no return value and typically the syscall
		// returns S_FALSE (a success value). We could ignore the return value
		// and error here but we handle anyway, it just in case.
		//
		// Note that ClosePseudoConsole is a blocking system call and may write
		// a final frame to the output buffer (p.outputWrite), so there must be
		// a consumer (p.outputRead) to ensure we don't block here indefinitely.
		//
		// https://docs.microsoft.com/en-us/windows/console/closepseudoconsole
		ret, _, err := procClosePseudoConsole.Call(uintptr(p.console))
		if winerrorFailed(ret) {
			return fmt.Errorf("close pseudo console (%d): %w", ret, err)
		}
		p.console = windows.InvalidHandle
	}

	return nil
}

// killOnContext waits for the context to be done and kills the process, unless it exits on its own first.
func (p *winProc) killOnContext(ctx context.Context) {
	select {
	case <-p.cmdDone:
		return
	case <-ctx.Done():
		p.Kill()
	}
}

// winerrorFailed returns true if the syscall failed, this function
// assumes the return value is a 32-bit integer, like HRESULT.
//
// https://learn.microsoft.com/en-us/windows/win32/api/winerror/nf-winerror-failed
func winerrorFailed(r1 uintptr) bool {
	return int32(r1) < 0
}

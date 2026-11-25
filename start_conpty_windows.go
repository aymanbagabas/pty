//go:build windows && go1.28
// +build windows,go1.28

package pty

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty/v2/conpty"
)

// The size of a new pseudo console if the caller does not give one.
const defaultCols, defaultRows = 80, 25

// After the pty closes, the process gets this much time to exit before it is
// terminated. Windows gives the same time to a process that gets
// CTRL_CLOSE_EVENT.
const closeTimeout = 5 * time.Second

// The exit status that Windows gives to processes that it ends when their
// console closes (STATUS_CONTROL_C_EXIT).
const controlCExit = 0xc000013a

var procGetConsoleScreenBufferInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")

// consolePty is the Pty that Start returns on Windows.
type consolePty struct {
	cpty *conpty.ConPty
	once sync.Once
	done chan struct{} // Closed by Close.
}

func (p *consolePty) Read(b []byte) (int, error)  { return p.cpty.Read(b) }
func (p *consolePty) Write(b []byte) (int, error) { return p.cpty.Write(b) }

// Name returns "conpty".
func (p *consolePty) Name() string { return "conpty" }

// Fd returns the pseudo console handle (HPCON), or 0 after the console closes.
func (p *consolePty) Fd() uintptr { return uintptr(p.cpty.Handle()) }

// SetDeadline sets the read and write deadlines.
func (p *consolePty) SetDeadline(t time.Time) error {
	if err := p.SetReadDeadline(t); err != nil {
		return err
	}
	return p.SetWriteDeadline(t)
}

// SetReadDeadline sets the deadline for Read.
func (p *consolePty) SetReadDeadline(t time.Time) error {
	return p.cpty.OutputPipe().SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for Write.
func (p *consolePty) SetWriteDeadline(t time.Time) error {
	return p.cpty.InputPipe().SetWriteDeadline(t)
}

// Close closes the console and its pipes. Windows ends the processes that are
// attached to the console. Close after the first call returns os.ErrClosed.
func (p *consolePty) Close() error {
	err := os.ErrClosed
	p.once.Do(func() {
		err = p.cpty.Close()
		close(p.done)
	})
	return err
}

// watch closes the console when the process exits. If the pty closes first,
// watch makes sure that the process exits.
func (p *consolePty) watch(process syscall.Handle) {
	defer syscall.CloseHandle(process)
	exited := make(chan struct{})
	go func() {
		_, _ = syscall.WaitForSingleObject(process, syscall.INFINITE)
		close(exited)
	}()

	select {
	case <-exited:
		p.cpty.CloseConsole()
	case <-p.done:
		// Usually Windows ends the process when the console closes. But if
		// the console closes while the process attaches to it, the process
		// can stay alive without a console.
		select {
		case <-exited:
		case <-time.After(closeTimeout):
			_ = syscall.TerminateProcess(process, controlCExit)
			<-exited
		}
	}
}

// Start starts c attached to a new Windows pseudo console (ConPTY).
//
// See StartWithAttrs for the details.
func Start(c *exec.Cmd) (Pty, error) {
	return StartWithSize(c, nil)
}

// StartWithSize starts c attached to a new Windows pseudo console (ConPTY)
// with the size ws. If ws is nil, the console is 80x25.
//
// See StartWithAttrs for the details.
func StartWithSize(c *exec.Cmd, ws *Winsize) (Pty, error) {
	return StartWithAttrs(c, ws, c.SysProcAttr)
}

// StartWithAttrs starts c attached to a new Windows pseudo console (ConPTY)
// with the size sz. If sz is nil, the console is 80x25. Read the returned Pty
// to get the console output, which contains virtual terminal sequences, and
// write it to send input.
//
// StartWithAttrs sets c.SysProcAttr to a copy of attrs with the Console
// field set. It does not change attrs. The process uses the console for its
// standard input, output, and error, so StartWithAttrs sets c.Stdin, c.Stdout,
// and c.Stderr to nil.
//
// When the process exits, the console closes. Read then returns the remaining
// output, then io.EOF. Windows also ends other processes that are attached to
// the console.
//
// When the caller closes the Pty, the console closes and Windows ends the
// processes that are attached to it. If the process is still alive 5 seconds
// later, it is terminated.
func StartWithAttrs(c *exec.Cmd, sz *Winsize, attrs *syscall.SysProcAttr) (Pty, error) {
	cols, rows := defaultCols, defaultRows
	if sz != nil && sz.Cols > 0 && sz.Rows > 0 {
		cols, rows = int(sz.Cols), int(sz.Rows)
	}
	cpty, err := conpty.New(cols, rows)
	if err != nil {
		return nil, err
	}

	sys := new(syscall.SysProcAttr)
	if attrs != nil {
		*sys = *attrs
	}
	sys.Console = cpty.Handle()
	c.SysProcAttr = sys
	// The process does not use these. If they stay set, os/exec still copies
	// them in goroutines, and a blocked copy makes Wait block.
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if err := c.Start(); err != nil {
		_ = cpty.Close()
		return nil, err
	}

	// Open a separate handle to the process. Then we can wait for it to exit
	// and the caller can still call c.Wait.
	process, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE, false, uint32(c.Process.Pid))
	if err != nil {
		_ = c.Process.Kill()
		_ = c.Wait()
		_ = cpty.Close()
		return nil, os.NewSyscallError("OpenProcess", err)
	}

	p := &consolePty{cpty: cpty, done: make(chan struct{})}
	go p.watch(process)
	return p, nil
}

// Setsize resizes t, a Pty from Start, to ws.
func Setsize(t Pty, ws *Winsize) error {
	p, ok := t.(*consolePty)
	if !ok {
		return ErrUnsupported
	}
	return p.cpty.Resize(int(ws.Cols), int(ws.Rows))
}

// GetsizeFull returns the size of t. t is a Pty from Start, or a console
// file such as os.Stdin or os.Stdout.
func GetsizeFull(t Pty) (*Winsize, error) {
	if p, ok := t.(*consolePty); ok {
		cols, rows := p.cpty.Size()
		return &Winsize{Rows: uint16(rows), Cols: uint16(cols)}, nil
	}
	return consoleSize(syscall.Handle(t.Fd()))
}

// consoleSize returns the size of the window of a console. h is a console
// input or output handle.
func consoleSize(h syscall.Handle) (*Winsize, error) {
	ws, err := screenBufferSize(h)
	if err == nil {
		return ws, nil
	}
	// An input handle, such as os.Stdin, does not have a screen buffer.
	// Use the active screen buffer of its console.
	var mode uint32
	if syscall.GetConsoleMode(h, &mode) != nil {
		return nil, err
	}
	out, oerr := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if oerr != nil {
		return nil, err
	}
	defer out.Close()
	return screenBufferSize(syscall.Handle(out.Fd()))
}

func screenBufferSize(h syscall.Handle) (*Winsize, error) {
	var info struct {
		size, cursor             [2]int16
		attributes               uint16
		left, top, right, bottom int16
		maxWindow                [2]int16
	}
	r, _, err := procGetConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return nil, os.NewSyscallError("GetConsoleScreenBufferInfo", err)
	}
	return &Winsize{
		Rows: uint16(info.bottom - info.top + 1),
		Cols: uint16(info.right - info.left + 1),
	}, nil
}

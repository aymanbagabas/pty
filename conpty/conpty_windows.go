//go:build windows && go1.25
// +build windows,go1.25

package conpty

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty/internal/winpipe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")
)

// ErrUnsupported is returned if Windows does not have the pseudo console API.
var ErrUnsupported = errors.New("conpty: pseudo console is not supported on this version of Windows")

// ErrClosed is returned if the pseudo console is closed.
var ErrClosed = errors.New("conpty: pseudo console is closed")

// ConPty is a Windows pseudo console and its input and output pipes.
// All methods are safe for concurrent use.
type ConPty struct {
	in  *os.File // The console reads what we write here.
	out *os.File // The console writes its output here.

	mu         sync.Mutex
	hpc        syscall.Handle
	cols, rows int
	closed     bool
}

// New creates a pseudo console with the given size in character cells.
func New(cols, rows int) (*ConPty, error) {
	if err := procCreatePseudoConsole.Find(); err != nil {
		return nil, ErrUnsupported
	}
	size, err := coord(cols, rows)
	if err != nil {
		return nil, err
	}

	inServer, inClient, err := winpipe.New(winpipe.Outbound, false)
	if err != nil {
		return nil, err
	}
	outServer, outClient, err := winpipe.New(winpipe.Inbound, false)
	if err != nil {
		_ = syscall.CloseHandle(inServer)
		_ = syscall.CloseHandle(inClient)
		return nil, err
	}

	var hpc syscall.Handle
	hr, _, _ := procCreatePseudoConsole.Call(size, uintptr(inClient), uintptr(outClient), 0, uintptr(unsafe.Pointer(&hpc)))
	// The console keeps its own copies of the client ends.
	_ = syscall.CloseHandle(inClient)
	_ = syscall.CloseHandle(outClient)
	if hr != 0 {
		_ = syscall.CloseHandle(inServer)
		_ = syscall.CloseHandle(outServer)
		return nil, fmt.Errorf("conpty: CreatePseudoConsole failed: HRESULT %#x", uint32(hr))
	}

	return &ConPty{
		in:   os.NewFile(uintptr(inServer), "conpty-input"),
		out:  os.NewFile(uintptr(outServer), "conpty-output"),
		hpc:  hpc,
		cols: cols,
		rows: rows,
	}, nil
}

// Handle returns the pseudo console handle, or 0 after CloseConsole or Close.
// Use it as syscall.SysProcAttr.Console to attach a new process to the console.
func (c *ConPty) Handle() syscall.Handle {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hpc
}

// Read reads output from the pseudo console.
func (c *ConPty) Read(p []byte) (int, error) {
	return c.out.Read(p)
}

// Write writes input to the pseudo console.
func (c *ConPty) Write(p []byte) (int, error) {
	return c.in.Write(p)
}

// InputPipe returns the write end of the console input pipe.
func (c *ConPty) InputPipe() *os.File {
	return c.in
}

// OutputPipe returns the read end of the console output pipe.
func (c *ConPty) OutputPipe() *os.File {
	return c.out
}

// Size returns the size of the pseudo console in character cells.
func (c *ConPty) Size() (cols, rows int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cols, c.rows
}

// Resize changes the size of the pseudo console in character cells.
func (c *ConPty) Resize(cols, rows int) error {
	size, err := coord(cols, rows)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hpc == 0 {
		return ErrClosed
	}
	if hr, _, _ := procResizePseudoConsole.Call(uintptr(c.hpc), size); hr != 0 {
		return fmt.Errorf("conpty: ResizePseudoConsole failed: HRESULT %#x", uint32(hr))
	}
	c.cols, c.rows = cols, rows
	return nil
}

// CloseConsole closes the pseudo console and keeps the pipes open.
// Windows ends the processes that are attached to the console. The console
// then writes its last output and closes its end of the output pipe, so Read
// returns io.EOF after the remaining output.
//
// Before Windows 11 24H2, CloseConsole waits until the remaining output is
// read. Read the output in a different goroutine, or call Close.
func (c *ConPty) CloseConsole() {
	c.mu.Lock()
	hpc := c.hpc
	c.hpc = 0
	c.mu.Unlock()

	if hpc != 0 {
		_, _, _ = procClosePseudoConsole.Call(uintptr(hpc))
	}
}

// Close closes the pipes and the pseudo console. Remaining output is lost.
// Windows ends the processes that are attached to the console.
// Close after the first call has no effect and returns nil.
func (c *ConPty) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Close the pipes first. Then ClosePseudoConsole cannot block on output
	// that nobody reads.
	err := errors.Join(c.in.Close(), c.out.Close())
	c.CloseConsole()
	return err
}

func coord(cols, rows int) (uintptr, error) {
	if cols <= 0 || rows <= 0 || cols > 0x7fff || rows > 0x7fff {
		return 0, fmt.Errorf("conpty: invalid size %dx%d", cols, rows)
	}
	// COORD{X: cols, Y: rows} is passed by value.
	return uintptr(cols) | uintptr(rows)<<16, nil
}

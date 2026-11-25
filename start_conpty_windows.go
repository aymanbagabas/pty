//go:build windows && go1.28
// +build windows,go1.28

package pty

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty/conpty"
	"github.com/creack/pty/internal/winpipe"
)

// The size of a new pseudo console if the caller does not give one.
const defaultCols, defaultRows = 80, 25

// sessions maps the *os.File that Start returns to its *session.
var sessions sync.Map

// A session bridges a pseudo console to one end of a duplex pipe. The caller
// gets the other end as one *os.File that reads and writes, as on Unix.
type session struct {
	cpty *conpty.ConPty
	pipe *os.File // Our end of the duplex pipe.
	file *os.File // The caller's end of the duplex pipe.
	once sync.Once
}

// close closes the pseudo console and our end of the pipe. The caller can
// read the output that is still in the pipe, then Read returns io.EOF.
func (s *session) close() {
	s.once.Do(func() {
		sessions.Delete(s.file)
		_ = s.cpty.Close()
		_ = s.pipe.Close()
	})
}

func lookupSession(f *os.File) (*session, bool) {
	s, ok := sessions.Load(f)
	if !ok {
		return nil, false
	}
	return s.(*session), true
}

// Start starts c attached to a new Windows pseudo console (ConPTY) and
// returns a file that reads the console output and writes the console input.
// Close the file to close the console.
//
// See StartWithAttrs for the details.
func Start(c *exec.Cmd) (*os.File, error) {
	return StartWithSize(c, nil)
}

// StartWithSize starts c attached to a new Windows pseudo console (ConPTY)
// with the size ws, and returns a file that reads the console output and
// writes the console input. If ws is nil, the console is 80x25.
//
// See StartWithAttrs for the details.
func StartWithSize(c *exec.Cmd, ws *Winsize) (*os.File, error) {
	return StartWithAttrs(c, ws, c.SysProcAttr)
}

// StartWithAttrs starts c attached to a new Windows pseudo console (ConPTY)
// with the size sz, and returns a file that reads the console output and
// writes the console input. If sz is nil, the console is 80x25.
//
// StartWithAttrs sets c.SysProcAttr to a copy of attrs with the Console
// field set. It does not change attrs. The process uses the console for its
// standard input, output, and error, so c.Stdin, c.Stdout, and c.Stderr have
// no effect.
//
// When the process exits, the console closes. Read then returns the remaining
// output, then io.EOF. Windows also ends other processes that are attached to
// the console.
//
// Use Setsize and GetsizeFull on the returned file to change and read the
// size of the console.
func StartWithAttrs(c *exec.Cmd, sz *Winsize, attrs *syscall.SysProcAttr) (*os.File, error) {
	cols, rows := defaultCols, defaultRows
	if sz != nil && sz.Cols > 0 && sz.Rows > 0 {
		cols, rows = int(sz.Cols), int(sz.Rows)
	}
	cpty, err := conpty.New(cols, rows)
	if err != nil {
		return nil, err
	}
	server, client, err := winpipe.New(winpipe.Duplex, true)
	if err != nil {
		_ = cpty.Close()
		return nil, err
	}
	s := &session{
		cpty: cpty,
		pipe: os.NewFile(uintptr(server), "conpty"),
		file: os.NewFile(uintptr(client), "conpty"),
	}

	sys := new(syscall.SysProcAttr)
	if attrs != nil {
		*sys = *attrs
	}
	sys.Console = cpty.Handle()
	c.SysProcAttr = sys
	if err := c.Start(); err != nil {
		s.close()
		_ = s.file.Close()
		return nil, err
	}

	// Open a separate handle to the process. Then we can wait for it to exit
	// and the caller can still call c.Wait.
	process, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(c.Process.Pid))
	if err != nil {
		_ = c.Process.Kill()
		s.close()
		_ = s.file.Close()
		return nil, os.NewSyscallError("OpenProcess", err)
	}

	sessions.Store(s.file, s)
	go func() {
		// This ends when the caller closes its file.
		_, _ = io.Copy(cpty, s.pipe)
		s.close()
	}()
	go func() {
		// This ends after the console closes and its output is copied.
		_, _ = io.Copy(s.pipe, cpty)
		s.close()
	}()
	go func() {
		_, _ = syscall.WaitForSingleObject(process, syscall.INFINITE)
		_ = syscall.CloseHandle(process)
		cpty.CloseConsole()
	}()
	return s.file, nil
}

// Setsize resizes the pseudo console of t, a file from Start.
func Setsize(t *os.File, ws *Winsize) error {
	s, ok := lookupSession(t)
	if !ok {
		return ErrUnsupported
	}
	return s.cpty.Resize(int(ws.Cols), int(ws.Rows))
}

// GetsizeFull returns the size of the pseudo console of t, a file from Start.
func GetsizeFull(t *os.File) (*Winsize, error) {
	s, ok := lookupSession(t)
	if !ok {
		return nil, ErrUnsupported
	}
	cols, rows := s.cpty.Size()
	return &Winsize{Rows: uint16(rows), Cols: uint16(cols)}, nil
}

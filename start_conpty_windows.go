//go:build windows && go1.28
// +build windows,go1.28

package pty

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty/conpty"
	"github.com/creack/pty/internal/winpipe"
)

// The size of a new pseudo console if the caller does not give one.
const defaultCols, defaultRows = 80, 25

// After the session closes, the process gets this much time to exit before
// it is terminated. Windows gives the same time to a process that gets
// CTRL_CLOSE_EVENT.
const closeTimeout = 5 * time.Second

// The exit status that Windows gives to processes that it ends when their
// console closes (STATUS_CONTROL_C_EXIT).
const controlCExit = 0xc000013a

// sessions maps the *os.File that Start returns to its *session.
var sessions sync.Map

// A session bridges a pseudo console to one end of a duplex pipe. The caller
// gets the other end as one *os.File that reads and writes, as on Unix.
type session struct {
	cpty *conpty.ConPty
	pipe *os.File // Our end of the duplex pipe.
	file *os.File // The caller's end of the duplex pipe.
	once sync.Once
	done chan struct{} // Closed by close.
}

// close closes the pseudo console and our end of the pipe. The caller can
// read the output that is still in the pipe, then Read returns io.EOF.
func (s *session) close() {
	s.once.Do(func() {
		sessions.Delete(s.file)
		_ = s.cpty.Close()
		_ = s.pipe.Close()
		close(s.done)
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
// standard input, output, and error, so StartWithAttrs sets c.Stdin, c.Stdout,
// and c.Stderr to nil.
//
// When the process exits, the console closes. Read then returns the remaining
// output, then io.EOF. Windows also ends other processes that are attached to
// the console.
//
// When the caller closes the file, the console closes and Windows ends the
// processes that are attached to it. If the process is still alive 5 seconds
// later, StartWithAttrs terminates it.
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
		done: make(chan struct{}),
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
		s.close()
		_ = s.file.Close()
		return nil, err
	}

	// Open a separate handle to the process. Then we can wait for it to exit
	// and the caller can still call c.Wait.
	process, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE, false, uint32(c.Process.Pid))
	if err != nil {
		_ = c.Process.Kill()
		_ = c.Wait()
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
	go s.watch(process)
	return s.file, nil
}

// watch closes the console when the process exits. If the session closes
// first, watch makes sure that the process exits.
func (s *session) watch(process syscall.Handle) {
	defer syscall.CloseHandle(process)
	exited := make(chan struct{})
	go func() {
		_, _ = syscall.WaitForSingleObject(process, syscall.INFINITE)
		close(exited)
	}()

	select {
	case <-exited:
		s.cpty.CloseConsole()
	case <-s.done:
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

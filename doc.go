// Package pty provides functions for working with Unix terminals and
// Windows pseudo consoles.
package pty

import (
	"errors"
	"io"
	"os"
)

// Pty is the controlling side of a pseudo-terminal. Read gets the output
// of the terminal, and Write sends input to it.
//
// On Unix, the value is an *os.File. On Windows, use a type assertion to
// get the SetDeadline, SetReadDeadline, and SetWriteDeadline methods.
// An *os.File also satisfies Pty, so a terminal such as os.Stdin can be
// used with Getsize, GetsizeFull, Setsize, and InheritSize.
type Pty interface {
	io.ReadWriteCloser

	// Name returns the name of the terminal.
	Name() string

	// Fd returns the file descriptor on Unix, and the pseudo console
	// handle (HPCON) on Windows.
	Fd() uintptr
}

// ErrUnsupported is returned if a function is not
// available on the current platform.
var ErrUnsupported = errors.New("unsupported")

// Open a pty and its corresponding tty. On Unix, pty is an *os.File.
//
// Windows does not have a tty side, so Open returns ErrUnsupported.
func Open() (pty Pty, tty *os.File, err error) {
	p, t, err := open()
	if err != nil {
		return nil, nil, err
	}
	return p, t, nil
}

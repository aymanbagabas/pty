// Package pty provides functions for working with Unix terminals.
package pty

import (
	"errors"
	"io"
)

var (
	// ErrUnsupported is returned if a function is not
	// available on the current platform.
	ErrUnsupported = errors.New("unsupported")

	// ErrNotPty is returned if a file is not a pty.
	ErrNotPty = errors.New("not a pty")

	// ErrClosed is returned if a file is closed.
	ErrClosed = errors.New("pty closed")
)

// File represents a pseudo-terminal file.
type File interface {
	io.ReadWriteCloser

	// Name returns the name of the TTY. For example, on Linux it would be
	// "/dev/pts/1".
	Name() string

	// Fd returns the integer file descriptor referencing the TTY.
	Fd() uintptr
}

// Open a pty and its corresponding tty.
func Open() (pty, tty File, err error) {
	return open()
}

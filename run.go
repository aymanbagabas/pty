package pty

import (
	"os/exec"
)

// StartOption is a function that configures the Pty.
type StartOption func(File) error

// WithSize resizes the Pty to the specified size before starting the command.
func WithSize(sz *Winsize) StartOption {
	return func(f File) error {
		return Setsize(f, sz)
	}
}

// Start assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding pty.
//
// Starts the process in a new session and sets the controlling terminal.
func Start(cmd *exec.Cmd, opts ...StartOption) (File, error) {
	return start(cmd, opts...)
}

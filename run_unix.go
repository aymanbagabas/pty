//go:build !windows
// +build !windows

package pty

import (
	"os/exec"
	"syscall"
)

// start assigns a pseudo-terminal Tty to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding Pty.
//
// This will resize the Pty to the specified size before starting the command if a size is provided.
// The `attrs` parameter overrides the one set in c.SysProcAttr.
//
// This should generally not be needed. Used in some edge cases where it is needed to create a pty
// without a controlling terminal.
func start(c *exec.Cmd, opts ...StartOption) (File, error) {
	pty, tty, err := open()
	if err != nil {
		return nil, err
	}

	for _, opt := range opts {
		if err := opt(pty); err != nil {
			return pty, err
		}
	}

	defer func() {
		// always close tty fds since it's being used in another process
		// but pty is kept to resize tty
		_ = tty.Close()
	}()

	if c.Stdout == nil {
		c.Stdout = tty
	}
	if c.Stderr == nil {
		c.Stderr = tty
	}
	if c.Stdin == nil {
		c.Stdin = tty
	}

	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
		c.SysProcAttr.Setsid = true
		c.SysProcAttr.Setctty = true
	}

	if err := c.Start(); err != nil {
		_ = pty.Close()
		return nil, err
	}
	return pty, err
}

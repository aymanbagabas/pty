package pty

import (
	"context"
	"os/exec"
	"syscall"
)

// Cmd is a drop-in replacement for exec.Cmd with most of the same API, but
// it exposes the context.Context to our PTY code so that we can still kill the
// process when the Context expires.  This is required because on Windows, we don't
// start the command using the `exec` library, so we have to manage the context
// ourselves.
type Cmd struct {
	Context     context.Context
	Path        string
	Args        []string
	Env         []string
	Dir         string
	Process     Process
	SysProcAttr *syscall.SysProcAttr
}

func CommandContext(ctx context.Context, name string, arg ...string) *Cmd {
	return &Cmd{
		Context: ctx,
		Path:    name,
		Args:    append([]string{name}, arg...),
		Env:     make([]string, 0),
	}
}

func Command(name string, arg ...string) *Cmd {
	return CommandContext(context.Background(), name, arg...)
}

func (c *Cmd) AsExec() *exec.Cmd {
	//nolint: gosec
	execCmd := exec.CommandContext(c.Context, c.Path, c.Args[1:]...)
	execCmd.Dir = c.Dir
	execCmd.Env = c.Env
	execCmd.SysProcAttr = c.SysProcAttr
	return execCmd
}

// Process represents a process running in a PTY. We need to trigger special processing on the PTY
// on process completion, meaning that we will have goroutines calling Wait() on the process.  Since
// the caller will also typically wait for the process, and it is not safe for multiple goroutines
// to Wait() on a process, this abstraction provides a goroutine-safe interface for interacting with
// the process.
// On Windows, this is implemented by wrapping the os.Process type.
// On Unix, this is just a type alias for os.Process.
type Process interface {
	// Wait for the command to complete.  Returned error is as for exec.Cmd.Wait()
	Wait() error

	// Kill the command process.  Returned error is as for os.Process.Kill()
	Kill() error
}

// Start assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding pty.
//
// Starts the process in a new session and sets the controlling terminal.
func Start(cmd *Cmd) (File, error) {
	return StartWithSize(cmd, nil)
}

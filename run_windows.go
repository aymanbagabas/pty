//go:build windows
// +build windows

package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartWithAttrs assigns a pseudo-terminal Tty to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding Pty.
//
// This will resize the Pty to the specified size before starting the command if a size is provided.
// The `attrs` parameter overrides the one set in c.SysProcAttr.
//
// This should generally not be needed. Used in some edge cases where it is needed to create a pty
// without a controlling terminal.
func start(c *exec.Cmd, opts ...StartOption) (File, error) {
	pty, _, err := open()
	if err != nil {
		return nil, err
	}

	for _, opt := range opts {
		if err := opt(pty); err != nil {
			return pty, err
		}
	}

	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}

	defer func() {
		// unlike unix command exec, do not close tty unless error happened
		if err != nil {
			_ = pty.Close()
		}
	}()

	// unlike unix command exec, do not set stdin/stdout/stderr

	// do not use os/exec.Start since we need to append console handler to startup info

	w := windowExecCmd{
		cmd:        c,
		waitCalled: false,
		conPty:     pty,
	}

	err = w.Start()
	if err != nil {
		return nil, err
	}

	return pty, err
}

// Start the specified command but does not wait for it to complete.
//
// If Start returns successfully, the c.Process field will be set.
//
// The Wait method will return the exit code and release associated resources
// once the command exits.
func (c *windowExecCmd) Start() error {
	if c.cmd.Process != nil {
		return errors.New("exec: already started")
	}

	var argv0 = c.cmd.Path
	var argv0p *uint16
	var argvp *uint16
	var dirp *uint16
	var err error

	sys := c.cmd.SysProcAttr
	if sys == nil {
		sys = &syscall.SysProcAttr{}
	}

	if c.cmd.Env == nil {
		c.cmd.Env, err = execEnvDefault(sys)
		if err != nil {
			return err
		}
	}

	var lp string

	lp, err = lookExtensions(c.cmd.Path, c.cmd.Dir)
	if err != nil {
		return err
	}

	c.cmd.Path = lp

	if len(c.cmd.Dir) != 0 {
		// Windows CreateProcess looks for argv0 relative to the current
		// directory, and, only once the new process is started, it does
		// Chdir(attr.Dir). We are adjusting for that difference here by
		// making argv0 absolute.

		argv0, err = joinExeDirAndFName(c.cmd.Dir, c.cmd.Path)
		if err != nil {
			return err
		}
	}

	argv0p, err = syscall.UTF16PtrFromString(argv0)
	if err != nil {
		return err
	}

	var cmdline string

	// Windows CreateProcess takes the command line as a single string:
	// use attr.CmdLine if set, else build the command line by escaping
	// and joining each argument with spaces
	if sys.CmdLine != "" {
		cmdline = sys.CmdLine
	} else {
		cmdline = makeCmdLine(c.argv())
	}

	if len(cmdline) != 0 {
		argvp, err = windows.UTF16PtrFromString(cmdline)
		if err != nil {
			return err
		}
	}

	if len(c.cmd.Dir) != 0 {
		dirp, err = windows.UTF16PtrFromString(c.cmd.Dir)
		if err != nil {
			return err
		}
	}

	// Acquire the fork lock so that no other threads
	// create new fds that are not yet close-on-exec
	// before we fork.
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()

	siEx := new(windows.StartupInfoEx)
	siEx.Flags = windows.STARTF_USESTDHANDLES

	if sys.HideWindow {
		siEx.Flags |= syscall.STARTF_USESHOWWINDOW
		siEx.ShowWindow = syscall.SW_HIDE
	}

	pi := new(windows.ProcessInformation)

	// Need EXTENDED_STARTUPINFO_PRESENT as we're making use of the attribute list field.
	flags := sys.CreationFlags | uint32(windows.CREATE_UNICODE_ENVIRONMENT) | windows.EXTENDED_STARTUPINFO_PRESENT

	c.attrList, err = windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("failed to initialize process thread attribute list: %w", err)
	}

	if c.conPty != nil {
		if err = updateProcThreadAttribute(c.conPty.handle, c.attrList); err != nil {
			return err
		}
	}

	siEx.ProcThreadAttributeList = c.attrList.List()
	siEx.Cb = uint32(unsafe.Sizeof(*siEx))

	if sys.Token != 0 {
		err = windows.CreateProcessAsUser(
			windows.Token(sys.Token),
			argv0p,
			argvp,
			nil,
			nil,
			false,
			flags,
			createEnvBlock(addCriticalEnv(dedupEnvCase(true, c.cmd.Env))),
			dirp,
			&siEx.StartupInfo,
			pi,
		)
	} else {
		err = windows.CreateProcess(
			argv0p,
			argvp,
			nil,
			nil,
			false,
			flags,
			createEnvBlock(addCriticalEnv(dedupEnvCase(true, c.cmd.Env))),
			dirp,
			&siEx.StartupInfo,
			pi,
		)
	}
	if err != nil {
		return err
	}

	defer func() {
		_ = windows.CloseHandle(pi.Thread)
		_ = windows.CloseHandle(pi.Process)
	}()

	process, err := os.FindProcess(int(pi.ProcessId))
	if err != nil {
		return err
	}

	c.cmd.Process = process

	wp := &windowsProcess{
		cmdDone: make(chan interface{}),
		proc:    process,
		pty:     c.conPty,
	}

	go wp.waitProcess()

	return nil
}

type windowsProcess struct {
	// cmdDone protects access to cmdErr: anything reading cmdErr should read from cmdDone first.
	cmdDone chan interface{}
	cmdErr  error
	proc    *os.Process
	pty     *conPty
}

func (p *windowsProcess) waitProcess() {
	defer close(p.cmdDone)
	defer func() {
		if err := winPtyConsoleCloser(p.pty); err != nil && p.cmdErr == nil {
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

func (p *windowsProcess) Wait() error {
	<-p.cmdDone
	return p.cmdErr
}

func (p *windowsProcess) Kill() error {
	return p.proc.Kill()
}

func (c *windowExecCmd) waitProcess(process *os.Process) {
	defer func() {
		if err := winPtyConsoleCloser(c.conPty); err != nil && c.cmdErr == nil {
			c.cmdErr = err
		}
	}()

	state, err := process.Wait()
	if err != nil {
		c.cmdErr = err
		return
	}

	if !state.Success() {
		c.cmdErr = &exec.ExitError{ProcessState: state}
		return
	}
}

func updateProcThreadAttribute(handle windows.Handle, attrList *windows.ProcThreadAttributeListContainer) error {
	var err error

	if err = attrList.Update(
		_PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(handle),
		unsafe.Sizeof(handle),
	); err != nil {
		return fmt.Errorf("failed to update proc thread attributes for pseudo console: %w", err)
	}

	return nil
}

//go:build windows
// +build windows

package pty

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartWithSize assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding pty.
//
// This will resize the pty to the specified size before starting the command.
// Starts the process in a new session and sets the controlling terminal.
func StartWithSize(cmd *Cmd, ws *Winsize) (File, error) {
	return StartWithAttrs(cmd, ws, cmd.SysProcAttr)
}

// StartWithAttrs assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding pty.
//
// This will resize the pty to the specified size before starting the command if a size is provided.
// The `attrs` parameter overrides the one set in c.SysProcAttr.
//
// This should generally not be needed. Used in some edge cases where it is needed to create a pty
// without a controlling terminal.
func StartWithAttrs(cmd *Cmd, sz *Winsize, attrs *syscall.SysProcAttr) (File, error) {
	winPty, err := openPty()
	if err != nil {
		return nil, err
	}

	ptyf, _ := winPty.ptyf(), winPty.ttyf()
	defer func() {
		if err != nil {
			// we hit some error finishing setup; close pty, so
			// we don't leak the kernel resources associated with it
			_ = ptyf.Close()
		}
	}()

	if sz != nil {
		if err := setsize(winPty, sz); err != nil {
			return nil, err
		}
	}

	if err := winPty.start(cmd); err != nil {
		return nil, err
	}

	return ptyf, nil
}

// Allocates a PTY and starts the specified command attached to it.
// See: https://docs.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session#creating-the-hosted-process
func (p *conPty) start(cmd *Cmd) (retErr error) {
	fullPath, err := exec.LookPath(cmd.Path)
	if err != nil {
		return err
	}
	pathPtr, err := windows.UTF16PtrFromString(fullPath)
	if err != nil {
		return err
	}
	argsPtr, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return err
	}
	if cmd.Dir == "" {
		cmd.Dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	dirPtr, err := windows.UTF16PtrFromString(cmd.Dir)
	if err != nil {
		return err
	}

	sys := cmd.SysProcAttr
	if sys == nil {
		sys = &syscall.SysProcAttr{}
	}

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}

	// Taken from: https://github.com/microsoft/hcsshim/blob/2314362e977aa03b3ed245a4beb12d00422af0e2/internal/winapi/process.go#L6
	const PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x20016
	err = attrs.Update(PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(p.console), unsafe.Sizeof(p.console))
	if err != nil {
		return err
	}

	// Acquire the fork lock so that no other threads
	// create new fds that are not yet close-on-exec
	// before we fork.
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()

	startupInfo := &windows.StartupInfoEx{}
	startupInfo.ProcThreadAttributeList = attrs.List()
	startupInfo.StartupInfo.Cb = uint32(unsafe.Sizeof(*startupInfo))
	startupInfo.StartupInfo.Flags = windows.STARTF_USESTDHANDLES
	if sys.HideWindow {
		startupInfo.StartupInfo.Flags |= syscall.STARTF_USESHOWWINDOW
		startupInfo.StartupInfo.ShowWindow = syscall.SW_HIDE
	}

	var processInfo windows.ProcessInformation
	// https://docs.microsoft.com/en-us/windows/win32/procthread/process-creation-flags#create_unicode_environment
	flags := sys.CreationFlags | uint32(windows.CREATE_UNICODE_ENVIRONMENT) | windows.EXTENDED_STARTUPINFO_PRESENT
	if sys.Token != 0 {
		err = windows.CreateProcessAsUser(
			windows.Token(sys.Token),
			pathPtr,
			argsPtr,
			nil,
			nil,
			false,
			flags,
			createEnvBlock(addCriticalEnv(dedupEnvCase(true, cmd.Env))),
			dirPtr,
			&startupInfo.StartupInfo,
			&processInfo,
		)
	} else {
		err = windows.CreateProcess(
			pathPtr,
			argsPtr,
			nil,
			nil,
			false,
			flags,
			createEnvBlock(addCriticalEnv(dedupEnvCase(true, cmd.Env))),
			dirPtr,
			&startupInfo.StartupInfo,
			&processInfo,
		)
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processInfo.Thread)
	defer windows.CloseHandle(processInfo.Process)

	process, err := os.FindProcess(int(processInfo.ProcessId))
	if err != nil {
		return fmt.Errorf("find process %d: %w", processInfo.ProcessId, err)
	}
	wp := &winProc{
		cmdDone: make(chan interface{}),
		proc:    process,
		pw:      p,
	}
	defer func() {
		if retErr != nil {
			// if we later error out, kill the process since
			// the caller will have no way to interact with it
			_ = process.Kill()
		}
	}()

	cmd.Process = wp

	// Now that we've started the command, and passed the pseudoconsole to it,
	// close the output write and input read files, so that the other process
	// has the only handles to them.  Once the process closes the console, there
	// will be no open references and the OS kernel returns an error when trying
	// to read or write to our end.  Without this, reading from the process
	// output will block until they are closed.
	// errO := p.pw.Close()
	// p.pw = nil
	// errI := p.pr.Close()
	// p.pr = nil
	// if errO != nil {
	// 	return errO
	// }
	// if errI != nil {
	// 	return errI
	// }
	go wp.waitInternal()
	if cmd.Context != nil {
		go wp.killOnContext(cmd.Context)
	}

	return nil
}

// Taken from: https://github.com/microsoft/hcsshim/blob/7fbdca16f91de8792371ba22b7305bf4ca84170a/internal/exec/exec.go#L476
func createEnvBlock(envv []string) *uint16 {
	if len(envv) == 0 {
		return &utf16.Encode([]rune("\x00\x00"))[0]
	}
	length := 0
	for _, s := range envv {
		length += len(s) + 1
	}
	length += 1

	b := make([]byte, length)
	i := 0
	for _, s := range envv {
		l := len(s)
		copy(b[i:i+l], []byte(s))
		copy(b[i+l:i+l+1], []byte{0})
		i = i + l + 1
	}
	copy(b[i:i+1], []byte{0})

	return &utf16.Encode([]rune(string(b)))[0]
}

// dedupEnvCase is dedupEnv with a case option for testing.
// If caseInsensitive is true, the case of keys is ignored.
func dedupEnvCase(caseInsensitive bool, env []string) []string {
	out := make([]string, 0, len(env))
	saw := make(map[string]int, len(env)) // key => index into out
	for _, kv := range env {
		eq := strings.Index(kv, "=")
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		k := kv[:eq]
		if caseInsensitive {
			k = strings.ToLower(k)
		}
		if dupIdx, isDup := saw[k]; isDup {
			out[dupIdx] = kv
			continue
		}
		saw[k] = len(out)
		out = append(out, kv)
	}
	return out
}

// addCriticalEnv adds any critical environment variables that are required
// (or at least almost always required) on the operating system.
// Currently this is only used for Windows.
func addCriticalEnv(env []string) []string {
	for _, kv := range env {
		eq := strings.Index(kv, "=")
		if eq < 0 {
			continue
		}
		k := kv[:eq]
		if strings.EqualFold(k, "SYSTEMROOT") {
			// We already have it.
			return env
		}
	}
	return append(env, "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
}

//go:build windows && go1.28
// +build windows,go1.28

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty/v2/conpty"
)

const windowsTimeout = 30 * time.Second

// readAllWindows reads p until io.EOF or the timeout.
func readAllWindows(t *testing.T, p Pty) string {
	t.Helper()
	d, ok := p.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		t.Fatalf("%T does not have SetReadDeadline", p)
	}
	noError(t, d.SetReadDeadline(time.Now().Add(windowsTimeout)), "SetReadDeadline")
	b, err := io.ReadAll(p)
	noError(t, err, "Read until EOF")
	return string(b)
}

func startWindows(t *testing.T, c *exec.Cmd, ws *Winsize) Pty {
	t.Helper()
	f, err := StartWithSize(c, ws)
	noError(t, err, "StartWithSize")
	t.Cleanup(func() {
		_ = f.Close()
		_ = c.Wait()
	})
	return f
}

func TestStartWindows(t *testing.T) {
	t.Parallel()

	const marker = "hello-conpty"
	c := exec.Command("cmd.exe", "/d", "/c", "echo", marker)
	f := startWindows(t, c, nil)

	out := readAllWindows(t, f)
	if !strings.Contains(out, marker) {
		t.Errorf("output = %q, want it to contain %q", out, marker)
	}
	noError(t, c.Wait(), "Wait")
}

// The output stays readable after the process exits and the console closes.
func TestStartWindowsReadAfterExit(t *testing.T) {
	t.Parallel()

	const marker = "after-exit"
	c := exec.Command("cmd.exe", "/d", "/c", "echo", marker)
	f := startWindows(t, c, nil)

	noError(t, c.Wait(), "Wait")
	time.Sleep(time.Second)

	out := readAllWindows(t, f)
	if !strings.Contains(out, marker) {
		t.Errorf("output = %q, want it to contain %q", out, marker)
	}
}

func TestStartWindowsInput(t *testing.T) {
	t.Parallel()

	c := exec.Command("cmd.exe", "/d", "/q")
	f := startWindows(t, c, nil)

	_, err := f.Write([]byte("set /a 6*7\r\nexit 3\r\n"))
	noError(t, err, "Write")

	out := readAllWindows(t, f)
	if !strings.Contains(out, "42") {
		t.Errorf("output = %q, want it to contain %q", out, "42")
	}
	var exitErr *exec.ExitError
	if err := c.Wait(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("Wait = %v, want exit status 3", err)
	}
}

func TestSetsizeWindows(t *testing.T) {
	t.Parallel()

	c := exec.Command("cmd.exe", "/d", "/q")
	f := startWindows(t, c, &Winsize{Rows: 30, Cols: 100})

	rows, cols, err := Getsize(f)
	noError(t, err, "Getsize")
	assert(t, 30, rows, "rows from Getsize")
	assert(t, 100, cols, "cols from Getsize")

	noError(t, Setsize(f, &Winsize{Rows: 40, Cols: 120}), "Setsize")
	rows, cols, err = Getsize(f)
	noError(t, err, "Getsize")
	assert(t, 40, rows, "rows from Getsize after Setsize")
	assert(t, 120, cols, "cols from Getsize after Setsize")

	_, err = f.Write([]byte("mode con\r\nexit\r\n"))
	noError(t, err, "Write")
	out := readAllWindows(t, f)
	for _, want := range []string{"40", "120"} {
		if !strings.Contains(out, want) {
			t.Errorf("mode con output = %q, want it to contain %q", out, want)
		}
	}
}

// Close closes the console, which ends the process. The test closes the
// console right after Start, so it also covers the race that leaves the
// process alive without a console.
func TestCloseWindows(t *testing.T) {
	t.Parallel()

	c := exec.Command("cmd.exe", "/d", "/q")
	p, err := Start(c)
	noError(t, err, "Start")

	noError(t, p.Close(), "Close")
	if err := p.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("second Close = %v, want %v", err, os.ErrClosed)
	}
	if err := Setsize(p, &Winsize{Rows: 10, Cols: 10}); !errors.Is(err, conpty.ErrClosed) {
		t.Errorf("Setsize after Close = %v, want %v", err, conpty.ErrClosed)
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		// The exit status depends on how far the process got. For example,
		// it is STATUS_DLL_INIT_FAILED if the console closes during startup.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("Wait = %v, want an exit error", err)
		}
	case <-time.After(windowsTimeout):
		_ = c.Process.Kill()
		t.Fatal("process did not exit after Close")
	}
}

// A process in the pseudo console gets the size of its console from
// GetsizeFull with os.Stdin or os.Stdout.
func TestGetsizeFullConsoleWindows(t *testing.T) {
	if os.Getenv("CREACK_PTY_SIZE_HELPER") == "1" {
		for _, f := range []*os.File{os.Stdin, os.Stdout} {
			ws, err := GetsizeFull(f)
			if err != nil {
				fmt.Printf("%s: %v\r\n", f.Name(), err)
				continue
			}
			fmt.Printf("%s=%dx%d\r\n", f.Name(), ws.Rows, ws.Cols)
		}
		return
	}
	t.Parallel()

	c := exec.Command(os.Args[0], "-test.run=^TestGetsizeFullConsoleWindows$")
	c.Env = append(os.Environ(), "CREACK_PTY_SIZE_HELPER=1")
	p := startWindows(t, c, &Winsize{Rows: 30, Cols: 100})
	out := readAllWindows(t, p)
	for _, want := range []string{os.Stdin.Name() + "=30x100", os.Stdout.Name() + "=30x100"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	noError(t, c.Wait(), "Wait")
}

// A file that is not a console has no size.
func TestGetsizeFullNotConsoleWindows(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "")
	noError(t, err, "CreateTemp")
	defer func() { _ = f.Close() }()
	if _, err := GetsizeFull(f); err == nil {
		t.Error("GetsizeFull succeeded for a regular file")
	}
	if err := Setsize(f, &Winsize{Rows: 10, Cols: 10}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Setsize = %v, want %v", err, ErrUnsupported)
	}
}

func TestStartWithAttrsWindows(t *testing.T) {
	t.Parallel()

	attrs := &syscall.SysProcAttr{HideWindow: true}
	c := exec.Command("cmd.exe", "/d", "/c", "exit")
	f, err := StartWithAttrs(c, nil, attrs)
	noError(t, err, "StartWithAttrs")
	defer func() { _ = f.Close() }()

	if attrs.Console != 0 {
		t.Error("StartWithAttrs changed attrs.Console")
	}
	if c.SysProcAttr == attrs || c.SysProcAttr.Console == 0 || !c.SysProcAttr.HideWindow {
		t.Errorf("c.SysProcAttr = %+v, want a copy of attrs with Console set", c.SysProcAttr)
	}
	readAllWindows(t, f)
	noError(t, c.Wait(), "Wait")
}

// A set c.Stdin that blocks must not make Wait block.
func TestStartWindowsIgnoresStdin(t *testing.T) {
	t.Parallel()

	r, w := io.Pipe()
	defer func() { _ = w.Close() }()
	c := exec.Command("cmd.exe", "/d", "/c", "exit")
	c.Stdin = r
	f := startWindows(t, c, nil)
	if c.Stdin != nil {
		t.Error("Start did not clear c.Stdin")
	}
	readAllWindows(t, f)

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		noError(t, err, "Wait")
	case <-time.After(windowsTimeout):
		t.Fatal("Wait blocked on c.Stdin")
	}
}

func TestStartWindowsBadCommand(t *testing.T) {
	t.Parallel()

	c := exec.Command("creack-pty-does-not-exist.exe")
	if f, err := Start(c); err == nil {
		_ = f.Close()
		t.Fatal("Start succeeded for a missing command")
	}
}

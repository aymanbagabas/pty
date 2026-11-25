//go:build windows && go1.28
// +build windows,go1.28

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

const windowsTimeout = 30 * time.Second

// readAllWindows reads f until io.EOF or the timeout.
func readAllWindows(t *testing.T, f *os.File) string {
	t.Helper()
	noError(t, f.SetReadDeadline(time.Now().Add(windowsTimeout)), "SetReadDeadline")
	b, err := io.ReadAll(f)
	noError(t, err, "Read until EOF")
	return string(b)
}

func startWindows(t *testing.T, c *exec.Cmd, ws *Winsize) *os.File {
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

// Close on the file closes the console, which ends the process.
func TestCloseWindows(t *testing.T) {
	t.Parallel()

	c := exec.Command("cmd.exe", "/d", "/q")
	f, err := Start(c)
	noError(t, err, "Start")

	noError(t, f.Close(), "Close")
	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("second Close = %v, want %v", err, os.ErrClosed)
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case <-done:
	case <-time.After(windowsTimeout):
		_ = c.Process.Kill()
		t.Fatal("process did not exit after Close")
	}

	if err := Setsize(f, &Winsize{Rows: 10, Cols: 10}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Setsize after Close = %v, want %v", err, ErrUnsupported)
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

func TestStartWindowsBadCommand(t *testing.T) {
	t.Parallel()

	c := exec.Command("creack-pty-does-not-exist.exe")
	if f, err := Start(c); err == nil {
		_ = f.Close()
		t.Fatal("Start succeeded for a missing command")
	}
}

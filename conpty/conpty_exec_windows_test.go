//go:build windows && go1.28
// +build windows,go1.28

package conpty

import (
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecCloseConsole(t *testing.T) {
	t.Parallel()

	c, err := New(80, 25)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	const marker = "conpty-exec"
	cmd := exec.Command("cmd.exe", "/d", "/c", "echo", marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Console: c.Handle()}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Read in a different goroutine, because CloseConsole can wait until
	// the output is read.
	out := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(c)
		out <- string(b)
	}()
	c.CloseConsole()
	if got := <-out; !strings.Contains(got, marker) {
		t.Errorf("output = %q, want it to contain %q", got, marker)
	}
}

func startAttached(t *testing.T, c *ConPty) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/d", "/q")
	cmd.SysProcAttr = &syscall.SysProcAttr{Console: c.Handle()}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return cmd
}

func waitExit(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("attached process did not exit")
	}
}

// Close ends the processes that are attached to the console.
func TestCloseEndsProcess(t *testing.T) {
	t.Parallel()

	c, err := New(80, 25)
	if err != nil {
		t.Fatal(err)
	}
	cmd := startAttached(t, c)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitExit(t, cmd)
}

// CloseConsole ends the processes that are attached to the console.
func TestCloseConsoleEndsProcess(t *testing.T) {
	t.Parallel()

	c, err := New(80, 25)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() { _, _ = io.Copy(io.Discard, c) }()
	cmd := startAttached(t, c)
	c.CloseConsole()
	waitExit(t, cmd)
}

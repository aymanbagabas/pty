//go:build windows && go1.28
// +build windows,go1.28

package conpty

import (
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
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

// Command runs a command in a pseudo-terminal and prints its output.
// It uses the same code on Unix and on Windows (ConPTY, Go 1.28 or later).
package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/creack/pty/v2"
)

func main() {
	// The command prints the terminal size, then a message.
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd.exe", "/d", "/c", "mode con & echo hello from a pty")
	} else {
		c = exec.Command("sh", "-c", "stty size; echo hello from a pty")
	}

	p, err := pty.StartWithSize(c, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	rows, cols, err := pty.Getsize(p)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("pty size: %d rows, %d cols", rows, cols)

	// The read stops when the command exits.
	_, _ = io.Copy(os.Stdout, p)
	if err := c.Wait(); err != nil {
		log.Fatal(err)
	}
}

# pty

Pty is a Go package for using unix pseudo-terminals.

On Windows, with Go 1.28 or later, `Start`, `StartWithSize`, and `StartWithAttrs` use the Windows pseudo console (ConPTY). See [Windows](#windows).

## Install

```sh
go get github.com/creack/pty
```

## Examples

Note that those examples are for demonstration purpose only, to showcase how to use the library. They are not meant to be used in any kind of production environment.

### Command

```go
package main

import (
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func main() {
	c := exec.Command("grep", "--color=auto", "bar")
	f, err := pty.Start(c)
	if err != nil {
		panic(err)
	}

	go func() {
		f.Write([]byte("foo\n"))
		f.Write([]byte("bar\n"))
		f.Write([]byte("baz\n"))
		f.Write([]byte{4}) // EOT
	}()
	io.Copy(os.Stdout, f)
}
```

### Shell

```go
package main

import (
        "io"
        "log"
        "os"
        "os/exec"
        "os/signal"
        "syscall"

        "github.com/creack/pty"
        "golang.org/x/term"
)

func test() error {
        // Create arbitrary command.
        c := exec.Command("bash")

        // Start the command with a pty.
        ptmx, err := pty.Start(c)
        if err != nil {
                return err
        }
        // Make sure to close the pty at the end.
        defer func() { _ = ptmx.Close() }() // Best effort.

        // Handle pty size.
        ch := make(chan os.Signal, 1)
        signal.Notify(ch, syscall.SIGWINCH)
        go func() {
                for range ch {
                        if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
                                log.Printf("error resizing pty: %s", err)
                        }
                }
        }()
        ch <- syscall.SIGWINCH // Initial resize.
        defer func() { signal.Stop(ch); close(ch) }() // Cleanup signals when done.

        // Set stdin in raw mode.
        oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
        if err != nil {
                panic(err)
        }
        defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.

        // Copy stdin to the pty and the pty to stdout.
        // NOTE: The goroutine will keep reading until the next keystroke before returning.
        go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
        _, _ = io.Copy(os.Stdout, ptmx)

        return nil
}

func main() {
        if err := test(); err != nil {
                log.Fatal(err)
        }
}
```

## Windows

Windows support uses the [pseudo console (ConPTY)](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session). It needs Windows 10 version 1809 or later and Go 1.28 or later, which adds `syscall.SysProcAttr.Console`. With older Go versions, the functions return `ErrUnsupported`.

`Start` returns one `*os.File` that reads the console output and writes the console input, as on Unix. These are the differences:

- The process uses the console for its standard input, output, and error. `c.Stdin`, `c.Stdout`, and `c.Stderr` have no effect.
- When the process exits, the console closes. `Read` returns the remaining output, then `io.EOF`. Windows also ends the other processes that are attached to the console.
- `Close` on the file closes the console.
- `Setsize`, `Getsize`, and `GetsizeFull` work only on a file from `Start`. Windows has no `SIGWINCH`, so poll for size changes or read them from the console input events.
- `Open` returns `ErrUnsupported`, because a Windows pseudo console has no tty file.

The output contains virtual terminal sequences. To use the console directly, for example with your own process creation, use the [`conpty`](https://pkg.go.dev/github.com/creack/pty/conpty) package.

See [examples/command](examples/command/main.go) for a program that works on Unix and Windows.

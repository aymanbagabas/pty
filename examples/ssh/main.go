package main

import (
	"fmt"
	"io"
	"log"
	"os/exec"

	"github.com/charmbracelet/ssh"
	"github.com/creack/pty"
)

func setWinsize(f pty.File, w, h int) {
	pty.Setsize(f, &pty.Winsize{
		Rows: uint16(h),
		Cols: uint16(w),
	})
}

func main() {
	ssh.Handle(func(s ssh.Session) {
		cmd := exec.Command("bash")
		ptyReq, winCh, isPty := s.Pty()
		if isPty {
			cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
			f, err := pty.Start(cmd, func(f pty.File) error {
				return applyTerminalModesToFd(f.Fd(), ptyReq.Window.Width, ptyReq.Window.Height, ptyReq.Modes, nil)
			})
			if err != nil {
				panic(err)
			}
			go func() {
				for win := range winCh {
					setWinsize(f, win.Width, win.Height)
				}
			}()
			go func() {
				io.Copy(f, s) // stdin
			}()
			io.Copy(s, f) // stdout
			// if err := cmd.Wait(); err != nil {
			// 	panic(err)
			// }
		} else {
			io.WriteString(s, "No PTY requested.\n")
			s.Exit(1)
		}
	})

	log.Println("starting ssh server on port 2222...")
	log.Fatal(ssh.ListenAndServe(":2222", nil))
}

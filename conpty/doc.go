// Package conpty provides access to the Windows pseudo console (ConPTY).
//
// A ConPty owns a pseudo console and the two pipes that connect it to the
// caller. Write sends input to the console, and Read receives the console
// output with its virtual terminal sequences.
//
// Since Go 1.28, os/exec can start a process attached to the console:
//
//	cpty, err := conpty.New(80, 25)
//	if err != nil {
//		return err
//	}
//	defer cpty.Close()
//
//	cmd := exec.Command("cmd.exe")
//	cmd.SysProcAttr = &syscall.SysProcAttr{Console: cpty.Handle()}
//	if err := cmd.Start(); err != nil {
//		return err
//	}
//
// The package needs Windows 10 version 1809 or later and Go 1.25 or later.
// On other platforms and older Go versions, the package is empty.
//
// See https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session.
package conpty

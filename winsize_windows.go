//go:build windows
// +build windows

package pty

// Winsize describes the terminal size.
// On Windows, only Rows and Cols are used.
type Winsize struct {
	Rows, Cols, X, Y uint16
}

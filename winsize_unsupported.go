//go:build windows && !go1.28
// +build windows,!go1.28

package pty

import (
	"os"
)

// Setsize resizes t to s.
func Setsize(*os.File, *Winsize) error {
	return ErrUnsupported
}

// GetsizeFull returns the full terminal size description.
func GetsizeFull(*os.File) (*Winsize, error) {
	return nil, ErrUnsupported
}

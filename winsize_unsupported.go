//go:build windows && !go1.28
// +build windows,!go1.28

package pty

// Setsize resizes t to s.
func Setsize(Pty, *Winsize) error {
	return ErrUnsupported
}

// GetsizeFull returns the full terminal size description.
func GetsizeFull(Pty) (*Winsize, error) {
	return nil, ErrUnsupported
}

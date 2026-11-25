//go:build windows && go1.25
// +build windows,go1.25

package conpty

import (
	"errors"
	"testing"
)

func TestNewResizeClose(t *testing.T) {
	t.Parallel()

	c, err := New(80, 25)
	if err != nil {
		t.Fatal(err)
	}
	if c.Handle() == 0 {
		t.Fatal("Handle is 0 for an open console")
	}
	if cols, rows := c.Size(); cols != 80 || rows != 25 {
		t.Errorf("Size = %dx%d, want 80x25", cols, rows)
	}

	if err := c.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if cols, rows := c.Size(); cols != 120 || rows != 40 {
		t.Errorf("Size after Resize = %dx%d, want 120x40", cols, rows)
	}
	if err := c.Resize(0, 40); err == nil {
		t.Error("Resize(0, 40) succeeded")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A second Close must not use the freed console handle.
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if c.Handle() != 0 {
		t.Error("Handle is not 0 after Close")
	}
	if err := c.Resize(80, 25); !errors.Is(err, ErrClosed) {
		t.Errorf("Resize after Close = %v, want %v", err, ErrClosed)
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Error("Read after Close succeeded")
	}
}

func TestNewInvalidSize(t *testing.T) {
	t.Parallel()

	for _, size := range [][2]int{{0, 25}, {80, 0}, {-1, 25}, {0x8000, 25}} {
		if c, err := New(size[0], size[1]); err == nil {
			_ = c.Close()
			t.Errorf("New(%d, %d) succeeded", size[0], size[1])
		}
	}
}

// Close must not block while another goroutine reads the output.
func TestCloseWhileReading(t *testing.T) {
	t.Parallel()

	c, err := New(80, 25)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := c.Read(buf); err != nil {
				done <- err
				return
			}
		}
	}()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-done; err == nil {
		t.Error("Read returned no error after Close")
	}
}

//go:build windows && go1.25
// +build windows,go1.25

// Package winpipe creates local named pipes for the Windows ConPTY support.
package winpipe

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Server access modes. The client end gets the opposite direction.
const (
	Inbound  = 0x1 // PIPE_ACCESS_INBOUND: the server reads, the client writes.
	Outbound = 0x2 // PIPE_ACCESS_OUTBOUND: the server writes, the client reads.
	Duplex   = 0x3 // PIPE_ACCESS_DUPLEX: both ends read and write.
)

const (
	fileFlagFirstPipeInstance = 0x00080000
	fileFlagOverlapped        = 0x40000000
	pipeRejectRemoteClients   = 0x00000008
	bufferSize                = 64 << 10
)

var (
	procCreateNamedPipeW = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateNamedPipeW")
	counter              uint64
)

// New creates a named pipe with a single instance and connects to it.
// The server end always uses overlapped I/O. The client end uses overlapped
// I/O only if clientOverlapped is true.
// Neither handle is inheritable.
func New(access uint32, clientOverlapped bool) (server, client syscall.Handle, err error) {
	var clientAccess uint32
	if access&Inbound != 0 {
		clientAccess |= syscall.GENERIC_WRITE
	}
	if access&Outbound != 0 {
		clientAccess |= syscall.GENERIC_READ
	}

	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, 0, err
	}
	name := fmt.Sprintf(`\\.\pipe\creack-pty-%d-%d-%s`, os.Getpid(), atomic.AddUint64(&counter, 1), hex.EncodeToString(nonce[:]))
	namep, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, 0, err
	}

	// FILE_FLAG_FIRST_PIPE_INSTANCE and a maximum of one instance make sure
	// that no other process owns or can open this pipe after we connect.
	r, _, e := procCreateNamedPipeW.Call(
		uintptr(unsafe.Pointer(namep)),
		uintptr(access|fileFlagFirstPipeInstance|fileFlagOverlapped),
		pipeRejectRemoteClients, // Byte stream, blocking mode.
		1,
		bufferSize,
		bufferSize,
		0,
		0,
	)
	server = syscall.Handle(r)
	if server == syscall.InvalidHandle {
		return 0, 0, os.NewSyscallError("CreateNamedPipe", e)
	}

	var flags uint32
	if clientOverlapped {
		flags = fileFlagOverlapped
	}
	client, err = syscall.CreateFile(namep, clientAccess, 0, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		_ = syscall.CloseHandle(server)
		return 0, 0, os.NewSyscallError("CreateFile", err)
	}
	return server, client, nil
}

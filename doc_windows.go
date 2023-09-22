//go:build windows
// +build windows

package pty

//go:generate go run golang.org/x/sys/windows/mkwinsyscall -output zsyscall_windows.go doc_windows.go

// const (
// 	_PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE = 0x20016
// )

// // createPseudoConsole creates a windows pseudo console.
// func createPseudoConsole(size windows.Coord, hInput windows.Handle, hOutput windows.Handle, dwFlags uint32, hpcon *windows.Handle) error {
// 	// We need this wrapper as the function takes a COORD struct and not a pointer to one, so we need to cast to something beforehand.
// 	return _createPseudoConsole(*((*uint32)(unsafe.Pointer(&size))), hInput, hOutput, 0, hpcon)
// }

// // resizePseudoConsole resizes the internal buffers of the pseudo console to the width and height specified in `size`.
// func resizePseudoConsole(hpcon windows.Handle, size windows.Coord) error {
// 	// We need this wrapper as the function takes a COORD struct and not a pointer to one, so we need to cast to something beforehand.
// 	return _resizePseudoConsole(hpcon, *((*uint32)(unsafe.Pointer(&size))))
// }

// HRESULT WINAPI CreatePseudoConsole(
//     _In_ COORD size,
//     _In_ HANDLE hInput,
//     _In_ HANDLE hOutput,
//     _In_ DWORD dwFlags,
//     _Out_ HPCON* phPC
// );
//
//sys _createPseudoConsole(size uint32, hInput windows.Handle, hOutput windows.Handle, dwFlags uint32, hpcon *windows.Handle) (hr error) = kernel32.CreatePseudoConsole

// void WINAPI ClosePseudoConsole(
//     _In_ HPCON hPC
// );
//
//sys closePseudoConsole(hpc windows.Handle) = kernel32.ClosePseudoConsole

// HRESULT WINAPI ResizePseudoConsole(
//     _In_ HPCON hPC ,
//     _In_ COORD size
// );
//
//sys _resizePseudoConsole(hPc windows.Handle, size uint32) (hr error) = kernel32.ResizePseudoConsole

// BOOL WINAPI GetConsoleScreenBufferInfo(
//     _In_  HANDLE                      hConsoleOutput,
//     _Out_ PCONSOLE_SCREEN_BUFFER_INFO lpConsoleScreenBufferInfo
// );
//
//sys getConsoleScreenBufferInfo(hConsoleOutput windows.Handle, lpConsoleScreenBufferInfo *windows.ConsoleScreenBufferInfo) (err error) = kernel32.GetConsoleScreenBufferInfo

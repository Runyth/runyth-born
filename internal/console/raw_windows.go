//go:build windows

package console

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	enableProcessedInput       = 0x0001
	enableLineInput            = 0x0002
	enableEchoInput            = 0x0004
	enableVirtualTerminalInput = 0x0200
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func enableRawMode(file *os.File) (func() error, bool) {
	handle := syscall.Handle(file.Fd())
	var previous uint32
	if err := consoleMode(getConsoleMode, handle, &previous); err != nil {
		return nil, false
	}

	raw := previous
	raw &^= enableLineInput | enableEchoInput
	raw |= enableProcessedInput | enableVirtualTerminalInput
	if err := setMode(handle, raw); err != nil {
		return nil, false
	}

	return func() error {
		return setMode(handle, previous)
	}, true
}

func consoleMode(procedure *syscall.LazyProc, handle syscall.Handle, mode *uint32) error {
	result, _, callErr := procedure.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(mode)),
	)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.EINVAL
	}
	return nil
}

func setMode(handle syscall.Handle, mode uint32) error {
	result, _, callErr := setConsoleMode.Call(uintptr(handle), uintptr(mode))
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.EINVAL
	}
	return nil
}

//go:build linux

package console

import (
	"os"
	"syscall"
	"unsafe"
)

func enableRawMode(file *os.File) (func() error, bool) {
	fileDescriptor := file.Fd()
	var previous syscall.Termios
	if err := ioctl(fileDescriptor, syscall.TCGETS, &previous); err != nil {
		return nil, false
	}

	raw := previous
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	raw.Cflag |= syscall.CS8
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if err := ioctl(fileDescriptor, syscall.TCSETS, &raw); err != nil {
		return nil, false
	}

	return func() error {
		return ioctl(fileDescriptor, syscall.TCSETS, &previous)
	}, true
}

func ioctl(fileDescriptor uintptr, request uintptr, state *syscall.Termios) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		fileDescriptor,
		request,
		uintptr(unsafe.Pointer(state)),
		0,
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

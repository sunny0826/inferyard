//go:build studio_dev && !release

package core

import (
	"syscall"
	"unsafe"
)

func DevelopmentProcessExecutable(pid int64) (string, error) {
	h, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(h)
	var buffer [32768]uint16
	size := uint32(len(buffer))
	query := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
	result, _, err := query.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if result == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buffer[:size]), nil
}

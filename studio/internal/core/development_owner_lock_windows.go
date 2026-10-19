//go:build studio_dev && !release

package core

import (
	"os"
	"syscall"
	"unsafe"
)

func tryDevelopmentOwnerLock(f *os.File) (bool, error) {
	var overlap syscall.Overlapped
	result, _, err := ownerLock.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlap)))
	if result != 0 {
		return true, nil
	}
	if err == syscall.Errno(33) {
		return false, nil
	}
	return false, err
}

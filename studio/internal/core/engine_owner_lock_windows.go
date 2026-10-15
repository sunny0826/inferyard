package core

import (
	"os"
	"syscall"
	"unsafe"
)

var ownerKernel = syscall.NewLazyDLL("kernel32.dll")
var ownerLock = ownerKernel.NewProc("LockFileEx")
var ownerUnlock = ownerKernel.NewProc("UnlockFileEx")

func lockOwnerFile(f *os.File) error {
	var overlap syscall.Overlapped
	result, _, err := ownerLock.Call(f.Fd(), 2, 0, 1, 0, uintptr(unsafe.Pointer(&overlap)))
	if result == 0 {
		return err
	}
	return nil
}
func unlockOwnerFile(f *os.File) {
	var overlap syscall.Overlapped
	ownerUnlock.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlap)))
}

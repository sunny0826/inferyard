//go:build darwin || linux

package core

import (
	"os"
	"syscall"
)

func lockOwnerFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			return err
		}
	}
}
func unlockOwnerFile(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

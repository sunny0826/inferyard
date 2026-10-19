//go:build studio_dev && !release && (darwin || linux)

package core

import (
	"os"
	"syscall"
)

func tryDevelopmentOwnerLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN || err == syscall.EINTR {
		return false, nil
	}
	return err == nil, err
}

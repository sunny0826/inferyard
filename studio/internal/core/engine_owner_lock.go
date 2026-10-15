package core

import (
	"os"
	"path/filepath"
)

// Keep the lock inode permanently: unlinking it would allow independent locks
// on an old open inode and its replacement. All owner mutations use this lock.
func lockEngineOwner(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockOwnerFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { unlockOwnerFile(f); f.Close() }, nil
}

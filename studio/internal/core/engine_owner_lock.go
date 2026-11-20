package core

import (
	"os"
	"path/filepath"
)

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

//go:build windows

package core

import "os"

type fileInodeKey struct {
	dev uint64
	ino uint64
}

func fileInodeLinks(os.FileInfo) (fileInodeKey, uint64, bool) {
	return fileInodeKey{}, 1, false
}

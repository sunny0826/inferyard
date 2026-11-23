//go:build !windows

package core

import (
	"os"
	"syscall"
)

type fileInodeKey struct {
	dev uint64
	ino uint64
}

func fileInodeLinks(info os.FileInfo) (fileInodeKey, uint64, bool) {
	if info == nil {
		return fileInodeKey{}, 1, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil || st.Nlink == 0 {
		return fileInodeKey{}, 1, false
	}
	return fileInodeKey{dev: uint64(st.Dev), ino: uint64(st.Ino)}, uint64(st.Nlink), true
}

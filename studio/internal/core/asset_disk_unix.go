//go:build !windows

package core

import (
	"math"
	"os"
	"strings"
	"syscall"
)

func DiskFreeBytesForPath(path string) *int64 {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	resolved, err := canonical(path)
	if err != nil {
		return nil
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(resolved, &stat); err != nil {
		return nil
	}
	if stat.Bsize <= 0 {
		return nil
	}
	bsize := uint64(stat.Bsize)
	bavail := uint64(stat.Bavail)
	if bavail > math.MaxInt64/bsize {
		return nil
	}
	free := int64(bavail * bsize)
	return &free
}

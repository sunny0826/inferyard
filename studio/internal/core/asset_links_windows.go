//go:build windows

package core

import (
	"os"
	"syscall"
)

type fileInodeKey struct {
	dev uint64
	ino uint64
}

func fileInodeLinks(path string, _ os.FileInfo) (fileInodeKey, uint64, bool) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fileInodeKey{}, 1, false
	}
	h, err := syscall.CreateFile(
		ptr,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return fileInodeKey{}, 1, false
	}
	defer func() { _ = syscall.CloseHandle(h) }()

	var bhfi syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &bhfi); err != nil || bhfi.NumberOfLinks == 0 {
		return fileInodeKey{}, 1, false
	}
	ino := (uint64(bhfi.FileIndexHigh) << 32) | uint64(bhfi.FileIndexLow)
	return fileInodeKey{dev: uint64(bhfi.VolumeSerialNumber), ino: ino}, uint64(bhfi.NumberOfLinks), true
}

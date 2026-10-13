package evidence

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

func HostStatePath() (string, error) {
	var buf [260]uint16
	dll := syscall.NewLazyDLL("shell32.dll")
	r, _, _ := dll.NewProc("SHGetFolderPathW").Call(0, 0x23, 0, 0, uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return "", fmt.Errorf("common data unavailable")
	}
	return filepath.Join(syscall.UTF16ToString(buf[:]), "inferyard-host.state.json"), nil
}

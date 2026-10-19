//go:build studio_dev && !release

package core

import (
	"github.com/ebitengine/purego"
	"os"
	"sync"
	"unsafe"
)

var loadDevExecutable = sync.OnceValues(func() (func(int32, unsafe.Pointer, uint32) int32, error) {
	handle, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	var query func(int32, unsafe.Pointer, uint32) int32
	purego.RegisterLibFunc(&query, handle, "proc_pidpath")
	return query, nil
})

func DevelopmentProcessExecutable(pid int64) (string, error) {
	query, err := loadDevExecutable()
	if err != nil {
		return "", err
	}
	var buffer [4096]byte
	n := query(int32(pid), unsafe.Pointer(&buffer[0]), uint32(len(buffer)))
	if n <= 0 || n >= int32(len(buffer)) {
		return "", os.ErrPermission
	}
	return string(buffer[:n]), nil
}

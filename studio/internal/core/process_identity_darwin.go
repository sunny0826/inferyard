package core

import (
	"math"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

var loadProcessInfo = sync.OnceValues(func() (func(int32, int32, uint64, unsafe.Pointer, int32) int32, error) {
	handle, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	var query func(int32, int32, uint64, unsafe.Pointer, int32) int32
	purego.RegisterLibFunc(&query, handle, "proc_pidinfo")
	return query, nil
})

func processStartTicks(pid int64) (int64, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return 0, os.ErrNotExist
	}
	query, err := loadProcessInfo()
	if err != nil {
		return 0, err
	}
	// Darwin proc_bsdinfo (PROC_PIDTBSDINFO=3), as declared by sys/proc_info.h.
	var info struct {
		Flags, Status, ExitStatus, PID, PPID, UID, GID, RUID, RGID, SVUID, SVGID, Reserved uint32
		Comm                                                                               [16]byte
		Name                                                                               [32]byte
		Files, PGID, JobCount, Device, TPGID                                               uint32
		Nice                                                                               int32
		Seconds, Microseconds                                                              uint64
	}
	size := int32(unsafe.Sizeof(info))
	if query(int32(pid), 3, 0, unsafe.Pointer(&info), size) != size {
		if syscall.Kill(int(pid), 0) == syscall.ESRCH {
			return 0, os.ErrNotExist
		}
		return 0, os.ErrPermission
	}
	if info.Status == 5 {
		return 0, os.ErrNotExist
	} // SZOMB
	// Match CLI's pinned psutil raw create_time and microseconds rounding.
	return int64(math.RoundToEven((float64(info.Seconds) + float64(info.Microseconds)/1e6) * 1e6)), nil
}

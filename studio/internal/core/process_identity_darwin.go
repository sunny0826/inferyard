package core

import (
	"context"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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

func classifyDarwinPSState(raw string) (deadOrZombie bool, exiting bool) {
	state := strings.TrimSpace(raw)
	if state == "" || strings.HasPrefix(state, "Z") {
		return true, false
	}
	if strings.Contains(state, "E") {
		return false, true
	}
	return false, false
}

func isZombieOrExitedDarwinPID(pid int64) bool {
	deadline := time.Now().Add(2 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.FormatInt(pid, 10), "-o", "state=").Output()
		cancel()
		if err != nil {
			return syscall.Kill(int(pid), 0) == syscall.ESRCH
		}
		dead, exiting := classifyDarwinPSState(string(out))
		if dead {
			return true
		}
		if !exiting {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func processStartTicks(pid int64) (int64, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return 0, os.ErrNotExist
	}
	query, err := loadProcessInfo()
	if err != nil {
		return 0, err
	}
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
		killErr := syscall.Kill(int(pid), 0)
		if killErr == syscall.ESRCH {
			return 0, os.ErrNotExist
		}
		if killErr == nil && isZombieOrExitedDarwinPID(pid) {
			return 0, os.ErrNotExist
		}
		return 0, os.ErrPermission
	}
	if info.Status == 5 {
		return 0, os.ErrNotExist
	}
	return int64(math.RoundToEven((float64(info.Seconds) + float64(info.Microseconds)/1e6) * 1e6)), nil
}

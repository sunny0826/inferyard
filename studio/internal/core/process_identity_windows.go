package core

import (
	"errors"
	"os"
	"syscall"
)

func processStartTicks(pid int64) (int64, error) {
	if pid <= 0 || pid > 1<<32-1 {
		return 0, os.ErrNotExist
	}
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		if errors.Is(err, syscall.Errno(87)) {
			return 0, os.ErrNotExist
		}
		return 0, err
	}
	defer syscall.CloseHandle(handle)
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	if exit.HighDateTime != 0 || exit.LowDateTime != 0 {
		return 0, os.ErrNotExist
	}
	return int64(uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)), nil
}

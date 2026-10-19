//go:build studio_dev && !release

package core

import (
	"errors"
	"os"
	"strconv"
	"syscall"
)

func developmentObserveProcess(pid int64) (DevelopmentProcessState, error) {
	return observeLinuxDevelopmentProcess(pid, os.ReadFile, func(pid int) error { return syscall.Kill(pid, 0) })
}

func observeLinuxDevelopmentProcess(pid int64, read func(string) ([]byte, error), probe func(int) error) (DevelopmentProcessState, error) {
	raw, err := read("/proc/" + strconv.FormatInt(pid, 10) + "/stat")
	if err == nil {
		_, err = parseProcessStat(raw)
		if errors.Is(err, os.ErrNotExist) {
			return DevelopmentProcessAbsent, nil
		}
		if err != nil {
			return DevelopmentProcessUnknown, err
		}
		return DevelopmentProcessPresent, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return DevelopmentProcessUnknown, err
	}
	self, selfErr := read("/proc/self/stat")
	if selfErr == nil {
		_, selfErr = parseProcessStat(self)
	}
	if selfErr != nil {
		return DevelopmentProcessUnknown, selfErr
	}
	if nativeErr := probe(int(pid)); nativeErr == syscall.ESRCH {
		return DevelopmentProcessAbsent, nil
	}
	return DevelopmentProcessUnknown, os.ErrPermission
}

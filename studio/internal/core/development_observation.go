//go:build studio_dev && !release

package core

import (
	"fmt"
	"runtime"
)

type DevelopmentProcessState uint8

const (
	DevelopmentProcessUnknown DevelopmentProcessState = iota
	DevelopmentProcessPresent
	DevelopmentProcessAbsent
)

func DevelopmentValidatePID(pid int64) error {
	maximum := int64(1<<31 - 1)
	if runtime.GOOS == "windows" {
		maximum = 1<<32 - 1
	}
	if pid <= 0 || pid > maximum {
		return fmt.Errorf("invalid development child PID")
	}
	return nil
}

func DevelopmentObserveProcess(pid int64) (DevelopmentProcessState, error) {
	if err := DevelopmentValidatePID(pid); err != nil {
		return DevelopmentProcessUnknown, err
	}
	return developmentObserveProcess(pid)
}

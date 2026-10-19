//go:build studio_dev && !release && (darwin || windows)

package core

import (
	"errors"
	"os"
)

func developmentObserveProcess(pid int64) (DevelopmentProcessState, error) {
	_, err := processStartTicks(pid)
	if errors.Is(err, os.ErrNotExist) {
		return DevelopmentProcessAbsent, nil
	}
	if err != nil {
		return DevelopmentProcessUnknown, err
	}
	return DevelopmentProcessPresent, nil
}

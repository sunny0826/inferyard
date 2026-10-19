//go:build studio_dev && !release

package core

import (
	"os"
	"strconv"
)

func DevelopmentProcessExecutable(pid int64) (string, error) {
	return os.Readlink("/proc/" + strconv.FormatInt(pid, 10) + "/exe")
}

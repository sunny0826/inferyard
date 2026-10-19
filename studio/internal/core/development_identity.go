//go:build studio_dev && !release

package core

func DevelopmentProcessStartTicks(pid int64) (int64, error) { return processStartTicks(pid) }

//go:build darwin || linux

package core

import (
	"os/exec"
	"syscall"
)

func isolateEngineProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return nil
}

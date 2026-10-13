//go:build !windows

package inferyard

import (
	"os/exec"
	"syscall"
)

func prepareProcess(cmd *exec.Cmd)   { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func interruptProcess(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
func killProcess(cmd *exec.Cmd)      { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

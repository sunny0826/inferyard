package inferyard

import "os/exec"

func prepareProcess(cmd *exec.Cmd)   {}
func interruptProcess(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
func killProcess(cmd *exec.Cmd)      { _ = cmd.Process.Kill() }

package core

import (
	"inferyard/studio/internal/evidence"
	"slices"
	"strconv"
)

const studioDirtyRecoveryNote = "studio_replaced_service_process"

func (s *Service) resolveHostStatePath() (string, error) {
	if s.hostStatePath != "" {
		return s.hostStatePath, nil
	}
	return evidence.HostStatePath()
}

func (s *Service) withDirtyRecoveryArgs(args []string, newPID, newStartTicks int64) []string {
	if newPID <= 0 || newStartTicks <= 0 || slices.Contains(args, "--recovery-confirm") {
		return args
	}
	host, err := s.resolveHostStatePath()
	if err != nil || host == "" {
		return args
	}
	dirty, err := evidence.ReadDirtyState(host)
	if err != nil || dirty == nil || dirty.DirtyToken == "" || dirty.ServerPID <= 0 || dirty.ProcessStartTicks <= 0 {
		return args
	}
	if newPID == dirty.ServerPID && newStartTicks == dirty.ProcessStartTicks {
		return args
	}
	alive, err := s.sameEngineProcess(engineProcessIdentity{PID: dirty.ServerPID, StartTicks: dirty.ProcessStartTicks})
	if err != nil || alive {
		return args
	}
	for _, proc := range dirty.LocalProcesses {
		if newPID == proc.PID && newStartTicks == proc.StartTicks {
			return args
		}
		procAlive, procErr := s.sameEngineProcess(engineProcessIdentity{PID: proc.PID, StartTicks: proc.StartTicks})
		if procErr != nil || procAlive {
			return args
		}
	}
	out := make([]string, len(args), len(args)+4)
	copy(out, args)
	return append(out, "--recovery-confirm", dirty.DirtyToken, "--recovery-note", studioDirtyRecoveryNote)
}

func (s *Service) executionArgsWithRecovery(t *runEntry) []string {
	if t == nil || len(t.args) == 0 {
		return nil
	}
	if t.local != nil && t.local.bound != nil {
		return s.withDirtyRecoveryArgs(t.args, t.local.bound.Pid, t.local.bound.StartTicks)
	}
	if t.Task.ConfigPath != nil && *t.Task.ConfigPath != "" {
		_, _, _, pid, ticks, err := readConfigWithEndpoint(*t.Task.ConfigPath)
		if err == nil {
			if alive, checkErr := s.sameEngineProcess(engineProcessIdentity{PID: pid, StartTicks: ticks}); checkErr == nil && alive {
				return s.withDirtyRecoveryArgs(t.args, pid, ticks)
			}
		}
		return t.args
	}
	if t.Task.Kind == "resume" {
		for i := 0; i+1 < len(t.args); i++ {
			if t.args[i] == "--server-pid" {
				if pid, err := strconv.ParseInt(t.args[i+1], 10, 64); err == nil && pid > 0 {
					if ticks, tickErr := s.processTicks(pid); tickErr == nil && ticks > 0 {
						return s.withDirtyRecoveryArgs(t.args, pid, ticks)
					}
				}
				break
			}
		}
	}
	return t.args
}

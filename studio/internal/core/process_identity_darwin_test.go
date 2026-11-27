//go:build darwin

package core

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessStartTicksTreatsZombieAndExitingStatesAsNotExistOnDarwin(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		wantDead bool
		wantExit bool
	}{
		{"", true, false},
		{"Z+", true, false},
		{"Z", true, false},
		{"UE", false, true},
		{"RE+", false, true},
		{"?E", false, true},
		{"S+", false, false},
		{"R", false, false},
	} {
		dead, exiting := classifyDarwinPSState(tc.raw)
		if dead != tc.wantDead || exiting != tc.wantExit {
			t.Fatalf("classifyDarwinPSState(%q) = (%v, %v), want (%v, %v)", tc.raw, dead, exiting, tc.wantDead, tc.wantExit)
		}
	}

	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if isZombieOrExitedDarwinPID(int64(cmd.Process.Pid)) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err := processStartTicks(int64(cmd.Process.Pid))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist for unreaped zombie PID %d, got %v", cmd.Process.Pid, err)
	}
}

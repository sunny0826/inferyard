package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProgressBlockedSurfacesLimitationCode(t *testing.T) {
	workspace := t.TempDir()
	cliPath := fakeCLIFile(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace, InferyardPath: &cliPath})
	if err := os.WriteFile(settingsPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				return inferyard.Execution{
					ExitCode: inferyard.ExitBlocked,
					Stdout:   []byte(`{"command":"progress","status":"blocked","limitations":["workspace_artifact_unreadable"]}`),
				}, nil
			}
			return inferyard.Execution{ExitCode: 0}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	resp, apiErr := s.Progress(context.Background())
	if resp.ExitCode != inferyard.ExitBlocked || resp.Progress != nil {
		t.Fatalf("unexpected progress response: %+v", resp)
	}
	if apiErr == nil || apiErr.Code != "workspace_artifact_unreadable" {
		t.Fatalf("expected workspace_artifact_unreadable error, got %+v", apiErr)
	}
}

func TestRefreshEngineTreePrunesExitedTransientWorkers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process group refresh is Unix-only")
	}
	rootPID := int64(os.Getpid())
	liveWorkerPID := int64(999991)
	ticksByPID := map[int64]int64{
		rootPID:       100,
		liveWorkerPID: 200,
	}
	s := &Service{
		identityReader: func(pid int64) (int64, error) {
			if ticks, ok := ticksByPID[pid]; ok {
				return ticks, nil
			}
			return 0, os.ErrNotExist
		},
	}
	owner := engineOwner{
		PID:          rootPID,
		StartTicks:   100,
		ProcessGroup: -1, // no real OS processes match -1; tests pruning of recorded workers
		Local:        true,
		Workers: []engineProcessIdentity{
			{PID: liveWorkerPID, StartTicks: 200},
			{PID: 999992, StartTicks: 201}, // exited transient probe (e.g. ioreg)
			{PID: 999993, StartTicks: 202}, // exited transient probe
		},
	}
	if err := s.refreshEngineTree(&owner, ""); err != nil {
		t.Fatalf("refreshEngineTree failed: %v", err)
	}
	if len(owner.Workers) != 1 || owner.Workers[0].PID != liveWorkerPID {
		t.Fatalf("expected only live worker %d to remain, got %+v", liveWorkerPID, owner.Workers)
	}
	delete(ticksByPID, liveWorkerPID)
	if err := s.refreshEngineTree(&owner, ""); err != nil {
		t.Fatalf("refreshEngineTree after all workers exited failed: %v", err)
	}
	if owner.Workers != nil {
		t.Fatalf("expected nil Workers slice after all workers exit (for omitempty/DeepEqual), got %#v", owner.Workers)
	}
}

func TestRefreshEngineTreeTracksSeparateProcessGroupChildren(t *testing.T) {
	origTable := engineProcessTableFunc
	origParent := engineProcessParentReader
	t.Cleanup(func() {
		engineProcessTableFunc = origTable
		engineProcessParentReader = origParent
	})

	ticksByPID := map[int64]int64{
		1000: 500, // server.py (pgid=1000)
		1001: 520, // strata C++ engine spawned via setpgid (ppid=1000, pgid=1001)
		1002: 530, // strata-vision helper spawned via setpgid (ppid=1000, pgid=1002)
		1003: 540, // grandchild worker (ppid=1001, pgid=1001)
		2000: 600, // unrelated process (ppid=1, pgid=2000)
	}
	parentsByPID := map[int64]int64{
		1000: 1,
		1001: 1000,
		1002: 1000,
		1003: 1001,
		2000: 1,
	}
	engineProcessTableFunc = func() ([]engineProcessRow, error) {
		return []engineProcessRow{
			{pid: 1000, parent: 1, group: 1000},
			{pid: 1003, parent: 1001, group: 1001}, // intentionally out-of-order before parent 1001
			{pid: 1001, parent: 1000, group: 1001},
			{pid: 1002, parent: 1000, group: 1002},
			{pid: 2000, parent: 1, group: 2000},
		}, nil
	}
	engineProcessParentReader = func(pid int64) (int64, error) {
		if p, ok := parentsByPID[pid]; ok {
			return p, nil
		}
		return 0, os.ErrNotExist
	}
	s := &Service{
		identityReader: func(pid int64) (int64, error) {
			if ticks, ok := ticksByPID[pid]; ok {
				return ticks, nil
			}
			return 0, os.ErrNotExist
		},
	}
	entry := &engineEntry{
		owner: engineOwner{
			PID:          1000,
			StartTicks:   500,
			ProcessGroup: 1000,
			Local:        true,
		},
		state: EngineState{Pid: 1000, Local: true, State: "running"},
	}
	if err := s.refreshEntryTree(entry); err != nil {
		t.Fatalf("refreshEntryTree failed: %v", err)
	}
	if len(entry.owner.Workers) != 3 {
		t.Fatalf("expected 3 descendant workers across separate pgids, got %+v", entry.owner.Workers)
	}
	if len(entry.pidSnapshot) != 4 {
		t.Fatalf("expected pidSnapshot to include root + 3 workers, got %v", entry.pidSnapshot)
	}
}

func TestReasoningEffortSettingsPersistenceAndBoundPaths(t *testing.T) {
	boundRoot, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boundCLI := filepath.Join(boundRoot, "cli")
	if err := os.WriteFile(boundCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		Paths: &BoundPaths{
			CLI:       boundCLI,
			Workspace: filepath.Join(boundRoot, "ws"),
			Results:   filepath.Join(boundRoot, "ws", "results"),
			Models:    filepath.Join(boundRoot, "shared", "models"),
			Engines:   filepath.Join(boundRoot, "shared", "engines"),
			Settings:  filepath.Join(boundRoot, "settings.json"),
		},
		HostStatePath: filepath.Join(boundRoot, "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	if got := s.ReasoningEffort(); got != ReasoningEffortNone {
		t.Fatalf("default ReasoningEffort = %q, want %q", got, ReasoningEffortNone)
	}
	resp, apiErr := s.UpdateSettings(map[string]json.RawMessage{"reasoning_effort": json.RawMessage(`"low"`)})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(reasoning_effort=low) failed: %+v", apiErr)
	}
	if resp.Effective.ReasoningEffort != ReasoningEffortLow || s.ReasoningEffort() != ReasoningEffortLow {
		t.Fatalf("expected effective reasoning_effort=low, got %+v", resp.Effective)
	}
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"reasoning_effort": json.RawMessage(`"ultra"`)}); apiErr == nil || apiErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for unsupported reasoning_effort, got %+v", apiErr)
	}
}

func TestFinishEngineTreeCleansLateSameGroupShutdownHelperAfterRootExits(t *testing.T) {
	origTable := engineProcessTableFunc
	origGroup := engineProcessGroupReader
	t.Cleanup(func() {
		engineProcessTableFunc = origTable
		engineProcessGroupReader = origGroup
	})

	// Simulate vLLM shutdown: root PID 2000 and EngineCore 2001 already exited,
	// while Python's multiprocessing.resource_tracker (PID 2002, pgid=2000) is still cleaning up.
	ticksByPID := map[int64]int64{2002: 610}
	engineProcessTableFunc = func() ([]engineProcessRow, error) {
		if _, alive := ticksByPID[2002]; alive {
			return []engineProcessRow{{pid: 2002, parent: 1, group: 2000}}, nil
		}
		return nil, nil
	}
	engineProcessGroupReader = func(pid int64) (int64, error) {
		if pid == 2002 {
			return 2000, nil
		}
		return 0, os.ErrNotExist
	}

	var signaled []int64
	s := &Service{
		identityReader: func(pid int64) (int64, error) {
			if ticks, ok := ticksByPID[pid]; ok {
				return ticks, nil
			}
			return 0, os.ErrNotExist
		},
		processSignal: func(pid int64, _ os.Signal) error {
			signaled = append(signaled, pid)
			delete(ticksByPID, pid)
			return nil
		},
	}
	entry := &engineEntry{
		owner: engineOwner{
			PID:          2000,
			StartTicks:   600,
			ProcessGroup: 2000,
			Local:        true,
			Workers:      []engineProcessIdentity{{PID: 2001, StartTicks: 605}},
		},
		state: EngineState{Pid: 2000, Local: true, State: "stopping"},
	}
	if apiErr := s.finishEngineTree(entry); apiErr != nil {
		t.Fatalf("expected finishEngineTree to terminate late same-group helper cleanly, got %+v", apiErr)
	}
	if len(signaled) != 1 || signaled[0] != 2002 {
		t.Fatalf("expected late helper PID 2002 to be signaled, got %v", signaled)
	}
}

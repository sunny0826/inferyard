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
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
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

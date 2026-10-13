package core

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceSwitchRetainsOwnershipUntilStopped(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	pollEngine(t, s, "running")
	next, _ := json.Marshal(t.TempDir())
	patch := map[string]json.RawMessage{"workspace_root": next}
	if _, e := s.UpdateSettings(patch); e == nil || e.Code != "workspace_busy" {
		t.Fatalf("%v", e)
	}
	if str(s.userSettings().WorkspaceRoot) != workspace {
		t.Fatal("workspace changed with live engine")
	}
	if _, e := s.StopEngine(); e != nil {
		t.Fatal(e)
	}
	pollEngine(t, s, "stopped")
	if _, e := s.UpdateSettings(patch); e != nil {
		t.Fatal(e)
	}
	if s.Engine().Pid != 0 {
		t.Fatal("old engine metadata leaked")
	}
}

package pages

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestRebindUsesFreshDirectoryAndCandidateFile(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidates", "zh-core-1")
	if err := os.MkdirAll(candidate, 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "bound", "zh-core-1")
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(old, "config.toml")
	os.WriteFile(marker, []byte("original evidence"), 0600)
	s := testState()
	s.candidate = candidate
	s.progress = &inferyard.WorkspaceProgress{Workspace: root, NextAction: "run", NextCommand: ptr("inferyard run --config old"), Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_missing", CandidatePath: &candidate, BoundPath: &old}}}
	s.engineState = core.EngineState{State: "running", Pid: 123, Endpoint: "http://127.0.0.1:48857"}
	command, e := s.managedBindCommand()
	if e != nil {
		t.Fatal(e)
	}
	args, err := inferyard.SplitCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	bound2, err := core.Resolve(root, filepath.Join("bound", "zh-core-2"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"config", "bind", "--candidate", filepath.Join(candidate, "candidate.toml"), "--pid", "123", "--endpoint", s.engineState.Endpoint, "--out", bound2}
	if !slices.Equal(args, want) {
		t.Fatalf("%v", args)
	}
	os.MkdirAll(filepath.Join(root, "bound", "zh-core-2"), 0700)
	command, e = s.bindCommand("456", "http://localhost:48857")
	if e != nil {
		t.Fatal(e)
	}
	args, _ = inferyard.SplitCommand(command)
	bound3, err := core.Resolve(root, filepath.Join("bound", "zh-core-3"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, bound3) || !slices.Contains(args, "456") {
		t.Fatal(args)
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != "original evidence" {
		t.Fatal("original changed")
	}
	if *s.progress.NextCommand != "inferyard run --config old" {
		t.Fatal("rewrote next_command")
	}
	s.progress.NextAction = "bind"
	s.progress.Bundles[0].Phase = "bound_missing"
	s.progress.Bundles[0].NextCommand = ptr("inferyard config bind --candidate '/frozen/candidate.toml' --pid 1 --endpoint http://localhost --out '/frozen/output'")
	command, e = s.managedBindCommand()
	if e != nil {
		t.Fatal(e)
	}
	args, _ = inferyard.SplitCommand(command)
	if !slices.Contains(args, "/frozen/output") {
		t.Fatal("rewrote CLI --out", args)
	}
}

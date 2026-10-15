package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCandidateEnginePlatformAndReceiptLayouts(t *testing.T) {
	for _, layout := range []string{"sibling", "parent"} {
		t.Run(layout, func(t *testing.T) {
			s := registryService(t)
			root := filepath.Join(*s.settings.WorkspaceRoot, "engines", "runtime")
			binary := writeEngineBinary(t, filepath.Join(root, "engine"), 0755, time.Now())
			dir := root
			if layout == "sibling" {
				dir = filepath.Dir(binary)
			}
			receipt := filepath.Join(dir, "runtime-receipt.json")
			if err := os.WriteFile(receipt, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			resolved, _ := canonical(receipt)
			if got := engineReceipt(binary); got != resolved {
				t.Fatalf("%s != %s", got, resolved)
			}
			name := s.Registry().Engines[0].Name
			for _, platform := range []string{"windows", "darwin", "linux"} {
				args, e := s.candidateEngineArgs([]string{"config", "create", "--engine", "old", "--runtime-receipt", "old"}, name, platform)
				flag, path := "--engine", binary
				if platform == "windows" {
					flag, path = "--runtime-receipt", receipt
				}
				path, _ = canonical(path)
				if e != nil || !reflect.DeepEqual(args, []string{"config", "create", flag, path}) {
					t.Fatalf("%s %v %v", platform, args, e)
				}
			}
		})
	}
}

func TestOrphanStoppingRejectsRunAdmission(t *testing.T) {
	s := registryService(t)
	root := t.TempDir()
	s.settings.ResultsRoot = &root
	s.mu.Lock()
	s.orphan = &orphanEngine{state: EngineState{State: "stopping"}, done: make(chan struct{})}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); close(s.orphan.done); s.orphan = nil; s.mu.Unlock() }()
	_, e := s.StartRuns(StartRunsRequest{ConfigPaths: []string{config(t, root, "zh-core")}, AllowSingleBundle: ptr(true)})
	if e == nil || e.Code != "engine_stopping" {
		t.Fatal(e)
	}
	s.mu.Lock()
	count := len(s.runs)
	s.mu.Unlock()
	if count != 0 {
		t.Fatal("run admitted")
	}
}

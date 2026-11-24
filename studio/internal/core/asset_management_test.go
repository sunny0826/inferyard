package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func assetTestExecFor(models *[]inferyard.ScannedModel) Executor {
	return func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			list := []inferyard.ScannedModel{}
			if models != nil {
				for _, m := range *models {
					if _, err := os.Stat(m.Path); err == nil {
						list = append(list, m)
					}
				}
			}
			payload, _ := json.Marshal(map[string]any{
				"command": "assets scan", "status": "scanned", "completeness": "complete", "limitations": []string{},
				"details": inferyard.AssetScan{Definition: inferyard.AssetScanDefinition, Models: list, Engines: []inferyard.ScannedEngine{}},
			})
			return inferyard.Execution{Stdout: payload}, nil
		}
		if len(args) > 0 && args[0] == "device-check" {
			return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
		}
		return inferyard.Execution{}, nil
	}
}

func assetTestExec(ctx context.Context, bin string, args ...string) (inferyard.Execution, error) {
	return assetTestExecFor(nil)(ctx, bin, args...)
}

func isolatedHomeAndBase(t *testing.T) (home, base string) {
	t.Helper()
	var err error
	if home, err = filepath.EvalSymlinks(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if base, err = filepath.EvalSymlinks(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return home, base
}

func mustMkdir(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func mustWriteFile(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteManagedAssetSingleGGUFFolderAndSiblings(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	modelsRoot := filepath.Join(base, "models")
	slugDir := filepath.Join(modelsRoot, "smol-slug")
	sharedDir := filepath.Join(modelsRoot, "shared-slug")

	soleGGUF := filepath.Join(slugDir, "smol.gguf")
	mustWriteFile(t, soleGGUF, []byte("gguf"), 0o644)
	mustWriteFile(t, filepath.Join(slugDir, "source.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(slugDir, "smol.gguf.part"), []byte("p"), 0o644)
	mustWriteFile(t, filepath.Join(slugDir, "model-assets.json"), []byte("{}"), 0o644)

	sharedGGUF := filepath.Join(sharedDir, "main.gguf")
	siblingFile := filepath.Join(sharedDir, "notes.txt")
	mustWriteFile(t, sharedGGUF, []byte("gguf"), 0o644)
	mustWriteFile(t, siblingFile, []byte("keep"), 0o644)

	models := []inferyard.ScannedModel{
		{Path: soleGGUF, Name: "smol", Format: "gguf", SizeBytes: ptr(int64(4)), Available: true},
		{Path: sharedGGUF, Name: "main", Format: "gguf", SizeBytes: ptr(int64(4)), Available: true},
	}
	s := testService(t, assetTestExecFor(&models))
	s.mu.Lock()
	s.settings.ModelsRoot = &modelsRoot
	s.assets.Parsed = &inferyard.AssetScan{Definition: inferyard.AssetScanDefinition, Models: models, Engines: []inferyard.ScannedEngine{}}
	s.mu.Unlock()

	if err := s.DeleteManagedAsset("model", "smol.gguf"); err != nil {
		t.Fatalf("DeleteManagedAsset(soleGGUF) failed: %+v", err)
	}
	if _, err := os.Stat(slugDir); !os.IsNotExist(err) {
		t.Fatalf("expected sole GGUF folder removed, stat err=%v", err)
	}
	wait(t, func() bool { return !s.Registry().Refreshing })

	if err := s.DeleteManagedAsset("model", "main.gguf"); err != nil {
		t.Fatalf("DeleteManagedAsset(sharedGGUF) failed: %+v", err)
	}
	if _, err := os.Stat(sharedGGUF); !os.IsNotExist(err) {
		t.Fatalf("expected sharedGGUF removed, stat err=%v", err)
	}
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("sibling file should remain preserved: %v", err)
	}
}

func TestDeleteManagedAssetRefusesBusyWorkspaceAndSafetyViolations(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	modelsRoot := filepath.Join(base, "models")
	resultsRoot := filepath.Join(base, "results")
	safeGGUF := filepath.Join(modelsRoot, "safe.gguf")
	evidenceDir := filepath.Join(modelsRoot, "run-evidence")
	mustMkdir(t, resultsRoot)
	mustWriteFile(t, safeGGUF, []byte("gguf"), 0o644)
	mustWriteFile(t, filepath.Join(evidenceDir, "manifest.json"), []byte("{}"), 0o644)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.ModelsRoot = &modelsRoot
	s.settings.ResultsRoot = &resultsRoot
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{
			{Path: safeGGUF, Name: "safe", Format: "gguf", Available: true},
			{Path: evidenceDir, Name: "run-evidence", Format: "directory", Available: true},
		},
	}
	s.downloads = []*downloadEntry{{Task: DownloadTask{TaskId: "dl-1", State: "downloading"}}}
	s.mu.Unlock()

	assertBusy := func(label string) {
		t.Helper()
		if err := s.DeleteManagedAsset("model", "safe.gguf"); err == nil || err.Code != "conflict" || err.Details["reason"] != "workspace_busy" {
			t.Fatalf("%s: expected workspace_busy conflict, got %+v", label, err)
		}
	}
	assertBusy("downloading")

	s.mu.Lock()
	s.downloads = nil
	s.engine = &engineEntry{state: EngineState{State: "running"}}
	s.mu.Unlock()
	assertBusy("running engine")

	s.mu.Lock()
	s.engine = nil
	s.runs = []*runEntry{{Task: RunTask{TaskId: "run-1", State: "running"}}}
	s.mu.Unlock()
	assertBusy("active run")

	s.mu.Lock()
	s.runs = nil
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("model", "run-evidence"); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable for sealed evidence marker, got %+v", err)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "manifest.json")); err != nil {
		t.Fatalf("evidence directory must remain untouched: %v", err)
	}

	overlapModel := filepath.Join(resultsRoot, "nested-model")
	mustMkdir(t, overlapModel)
	s.mu.Lock()
	s.settings.ModelsRoot = &resultsRoot
	raw, _ := json.Marshal(inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models:     []inferyard.ScannedModel{{Path: overlapModel, Name: "nested-model", Format: "directory", Available: true}},
		Engines:    []inferyard.ScannedEngine{},
	})
	s.assets.Parsed = nil
	s.assets.Snapshot = raw
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("model", "nested-model"); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable for ResultsRoot overlap, got %+v", err)
	}
}

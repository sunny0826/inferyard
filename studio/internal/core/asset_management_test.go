package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

func TestSuggestLocalModelDirsFiltersRealDirsAndExistingRoots(t *testing.T) {
	home, _ := isolatedHomeAndBase(t)
	lms := filepath.Join(home, ".lmstudio", "models")
	hf := filepath.Join(home, ".cache", "huggingface", "hub")
	ollama := filepath.Join(home, ".ollama", "models")
	plain := filepath.Join(home, "models")
	mustMkdir(t, lms, hf)
	mustWriteFile(t, ollama, []byte("not-dir"), 0o644)
	if runtime.GOOS != "windows" {
		linkTarget := filepath.Join(home, "elsewhere")
		mustMkdir(t, linkTarget)
		if err := os.Symlink(linkTarget, plain); err != nil {
			t.Fatal(err)
		}
	}

	got := SuggestLocalModelDirs([]string{hf})
	if len(got) != 1 || got[0].Path != lms || got[0].Label != "LM Studio" {
		t.Fatalf("unexpected suggested dirs: %+v", got)
	}
}

func TestRegistryPopulatesPathsDeletableTotalsAndDiskFree(t *testing.T) {
	home, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "workspace")
	modelsRoot := filepath.Join(base, "models")
	resultsRoot := filepath.Join(base, "results")
	outsideRoot := filepath.Join(base, "outside")
	mustMkdir(t, ws, modelsRoot, resultsRoot, outsideRoot)

	managedGGUF := filepath.Join(modelsRoot, "managed.gguf")
	outsideGGUF := filepath.Join(outsideRoot, "external.gguf")
	linkedTarget := filepath.Join(outsideRoot, "linked-target.gguf")
	symlinkGGUF := filepath.Join(modelsRoot, "linked.gguf")
	mustWriteFile(t, managedGGUF, make([]byte, 128), 0o644)
	mustWriteFile(t, outsideGGUF, make([]byte, 64), 0o644)
	mustWriteFile(t, linkedTarget, make([]byte, 64), 0o644)
	if runtime.GOOS != "windows" {
		if err := os.Symlink(linkedTarget, symlinkGGUF); err != nil {
			t.Fatal(err)
		}
	}

	wsEngine := filepath.Join(ws, "engines", "llama-b1", "bin", engineBinaryName())
	userEngine := filepath.Join(home, ".inferyard", "engine", "user-llama", engineBinaryName())
	instPath := filepath.Join(base, "install.json")
	mustWriteFile(t, wsEngine, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	mustWriteFile(t, userEngine, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	mustWriteFile(t, instPath, []byte(`{"definition":"local-engine-installation.v1","family":"llama-cpp"}`), 0o644)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.ResultsRoot = &resultsRoot
	s.settings.EngineInstallations = []string{instPath}
	scannedModels := []inferyard.ScannedModel{
		{Path: managedGGUF, Name: "managed", Format: "gguf", SizeBytes: ptr(int64(128)), Available: true},
		{Path: outsideGGUF, Name: "external", Format: "gguf", SizeBytes: ptr(int64(64)), Available: true},
	}
	if runtime.GOOS != "windows" {
		scannedModels = append(scannedModels, inferyard.ScannedModel{
			Path: symlinkGGUF, Name: "linked", Format: "gguf", SizeBytes: ptr(int64(64)), Available: true,
		})
	}
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition, Models: scannedModels, Engines: []inferyard.ScannedEngine{}, TotalModelBytes: ptr(int64(256)),
	}
	s.mu.Unlock()

	reg := s.Registry()
	if len(reg.ModelRoots) != 1 || reg.ModelRoots[0] != modelsRoot {
		t.Fatalf("unexpected ModelRoots: %+v", reg.ModelRoots)
	}
	expectedTotal := int64(192)
	if runtime.GOOS != "windows" {
		expectedTotal = 256
	}
	if reg.TotalModelBytes != expectedTotal {
		t.Fatalf("TotalModelBytes = %d, want %d", reg.TotalModelBytes, expectedTotal)
	}
	if runtime.GOOS != "windows" && reg.DiskFreeBytes == nil {
		t.Fatal("expected non-nil DiskFreeBytes on Unix")
	}

	byModel := map[string]ModelAsset{}
	for _, m := range reg.Models {
		byModel[m.Name] = m
	}
	if m := byModel["managed.gguf"]; !m.Deletable || m.Path != managedGGUF {
		t.Fatalf("managed model mismatch: %+v", m)
	}
	if m := byModel["external.gguf"]; m.Deletable || m.Path != outsideGGUF {
		t.Fatalf("outside model must not be deletable: %+v", m)
	}
	if runtime.GOOS != "windows" {
		if m, ok := byModel["linked.gguf"]; ok && m.Deletable {
			t.Fatalf("symlink model must not be deletable: %+v", m)
		}
	}

	var sawWS, sawUser, sawLocal bool
	for _, e := range reg.Engines {
		switch {
		case e.Path == wsEngine:
			sawWS = true
			if !e.Deletable {
				t.Fatalf("workspace engine should be deletable: %+v", e)
			}
		case e.Path == userEngine:
			sawUser = true
			if e.Deletable {
				t.Fatalf("user engine dir must not be deletable: %+v", e)
			}
		case e.Kind == "local_installation":
			sawLocal = true
			if e.Deletable || e.Path != instPath {
				t.Fatalf("local_installation mismatch: %+v", e)
			}
		}
	}
	if !sawWS || !sawUser || !sawLocal {
		t.Fatalf("missing engines: ws=%v user=%v local=%v engines=%+v", sawWS, sawUser, sawLocal, reg.Engines)
	}
}

func TestRevealAssetResolvesTargetsAndHandlesMissing(t *testing.T) {
	_, modelsRoot := isolatedHomeAndBase(t)
	ggufFile := filepath.Join(modelsRoot, "sub", "one.gguf")
	dirModel := filepath.Join(modelsRoot, "dir-model")
	mustWriteFile(t, ggufFile, []byte("gguf"), 0o644)
	mustMkdir(t, dirModel)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.ModelsRoot = &modelsRoot
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{
			{Path: ggufFile, Name: "one", Format: "gguf", Available: true},
			{Path: dirModel, Name: "dir-model", Format: "directory", Available: true},
		},
	}
	s.mu.Unlock()

	origOpen := openPathFunc
	t.Cleanup(func() { openPathFunc = origOpen })
	var opened []string
	openPathFunc = func(_ string, args ...string) error {
		if len(args) > 0 {
			opened = append(opened, args[0])
		}
		return nil
	}

	if err := s.RevealAsset("model", "one.gguf"); err != nil {
		t.Fatalf("RevealAsset(gguf) failed: %+v", err)
	}
	if err := s.RevealAsset("model", "dir-model"); err != nil {
		t.Fatalf("RevealAsset(dir) failed: %+v", err)
	}
	if len(opened) != 2 || opened[0] != filepath.Dir(ggufFile) || opened[1] != dirModel {
		t.Fatalf("unexpected revealed targets: %v", opened)
	}
	if err := s.RevealAsset("model", "missing.gguf"); err == nil || err.Code != "not_found" {
		t.Fatalf("expected not_found, got %+v", err)
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

func TestDeleteManagedAssetEngineAndDirectoryModel(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dirModel := filepath.Join(modelsRoot, "qwen-dir")
	engInstallDir := filepath.Join(ws, "engines", "llama-b1")
	engBin := filepath.Join(engInstallDir, "bin", engineBinaryName())
	mustWriteFile(t, filepath.Join(dirModel, "config.json"), []byte("{}"), 0o644)
	mustWriteFile(t, engBin, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	mustWriteFile(t, filepath.Join(engInstallDir, "runtime-receipt.json"), []byte("{}"), 0o644)

	models := []inferyard.ScannedModel{{Path: dirModel, Name: "qwen-dir", Format: "directory", SizeBytes: ptr(int64(2)), Available: true}}
	s := testService(t, assetTestExecFor(&models))
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.assets.Parsed = &inferyard.AssetScan{Definition: inferyard.AssetScanDefinition, Models: models, Engines: []inferyard.ScannedEngine{}}
	s.mu.Unlock()

	reg := s.Registry()
	if len(reg.Engines) != 1 || !reg.Engines[0].Deletable {
		t.Fatalf("expected 1 deletable engine, got %+v", reg.Engines)
	}
	if err := s.DeleteManagedAsset("engine", reg.Engines[0].Name); err != nil {
		t.Fatalf("DeleteManagedAsset(engine) failed: %+v", err)
	}
	if _, err := os.Stat(engInstallDir); !os.IsNotExist(err) {
		t.Fatalf("expected engine install dir removed, stat err=%v", err)
	}
	wait(t, func() bool { return !s.Registry().Refreshing })

	if err := s.DeleteManagedAsset("model", "qwen-dir"); err != nil {
		t.Fatalf("DeleteManagedAsset(dirModel) failed: %+v", err)
	}
	if _, err := os.Stat(dirModel); !os.IsNotExist(err) {
		t.Fatalf("expected dirModel removed, stat err=%v", err)
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

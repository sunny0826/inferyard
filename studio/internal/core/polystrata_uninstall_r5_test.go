package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestPolystrataUninstallConfigOnlyDeletePreservesOtherModelReferencedDir(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	sharedTokDir := filepath.Join(dataDir, "models", "shared-tok-dir")
	mustMkdir(t, ws, sharedTokDir)

	extWeightA := filepath.Join(base, "ext-a.gguf")
	extWeightB := filepath.Join(base, "ext-b.gguf")
	mustWriteFile(t, extWeightA, bytes.Repeat([]byte("a"), 128), 0o600)
	mustWriteFile(t, extWeightB, bytes.Repeat([]byte("b"), 128), 0o600)

	cfgA := writeStrataConfigFixture(t, filepath.Join(sharedTokDir, "strata-a.json"), "Model A", []string{"--model", extWeightA})
	cfgBPayload, _ := json.Marshal(map[string]any{
		"exe":           "/opt/ps/strata",
		"args":          []string{"--model", extWeightB},
		"tokenizer_dir": sharedTokDir,
		"model_name":    "Model B",
	})
	cfgB := filepath.Join(dataDir, "models", "b-holder", "strata-b.json")
	mustWriteFile(t, cfgB, cfgBPayload, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.mu.Unlock()

	infoA, err := s.InspectDeleteAsset("model", "Model A")
	if err != nil || infoA.Scope != "polystrata_model_config_only" {
		t.Fatalf("expected polystrata_model_config_only inspect for Model A, got %+v err=%+v", infoA, err)
	}
	if err := s.DeleteManagedAssetVerified("model", "Model A", infoA.TargetPath, infoA.Fingerprint); err != nil {
		t.Fatalf("delete Model A failed: %+v", err)
	}
	if _, err := os.Stat(cfgA); !os.IsNotExist(err) {
		t.Fatalf("expected strata-a.json removed, stat err=%v", err)
	}
	if _, err := os.Stat(sharedTokDir); err != nil {
		t.Fatalf("shared tokenizer_dir referenced by Model B must survive empty-dir cleanup: %v", err)
	}
	var modelB *ModelAsset
	for i := range s.Registry().Models {
		m := &s.Registry().Models[i]
		if m.Path == cfgB {
			modelB = m
			break
		}
	}
	if modelB == nil || !modelB.Available {
		t.Fatalf("Model B must remain Available=true after Model A deletion: %+v", modelB)
	}
}

func TestPolystrataUninstallHardlinkedExternalInstallationNotUnregistered(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-hl")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-hl", true)
	extDir := filepath.Join(base, "ext-mirror")
	mustMkdir(t, extDir)
	extInst := filepath.Join(extDir, "installation.json")

	// Clear working_directory so the external hardlink declares only shared data_dir (no dep on psDir).
	payload, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "executable": filepath.Join(psDir, "serve", "server.py"),
		"data_dir": dataDir,
	})
	_ = payload
	// First test independent hardlinked installation without executable inside psDir.
	extExe := filepath.Join(base, "ext-bin", "strata")
	mustWriteFile(t, extExe, []byte("#!/bin/sh\n"), 0o755)
	indepPayload, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "executable": extExe, "data_dir": dataDir,
	})
	mustWriteFile(t, psInst, indepPayload, 0o600)
	if err := os.Link(psInst, extInst); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst, extInst}
	s.mu.Unlock()

	var psName string
	for _, e := range s.Registry().Engines {
		if e.Path == psInst {
			psName = e.Name
			break
		}
	}
	if psName == "" {
		t.Fatalf("missing Registry engine for managed %s: %+v", psInst, s.Registry().Engines)
	}
	if err := s.DeleteManagedAsset("engine", psName); err != nil {
		t.Fatalf("uninstall managed installation failed: %+v", err)
	}
	gotInsts := s.Settings().Settings.EngineInstallations
	if len(gotInsts) != 1 || !sameDirectoryEntry(gotInsts[0], extInst) {
		t.Fatalf("external hardlinked installation must NOT be unregistered: got %+v", gotInsts)
	}
	if _, err := os.Stat(extInst); err != nil {
		t.Fatalf("external installation.json must survive on disk: %v", err)
	}
}

func TestPolystrataUninstallHardlinkedExternalDepBlocksUninstall(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-hl-dep")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-hldep", true)
	extDir := filepath.Join(base, "ext-dep-hl")
	mustMkdir(t, extDir)
	extInst := filepath.Join(extDir, "installation.json")
	if err := os.Link(psInst, extInst); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{extInst, psInst}
	s.mu.Unlock()

	var psEng EngineAsset
	for _, e := range s.Registry().Engines {
		if sameDirectoryEntry(e.Path, psInst) {
			psEng = e
			break
		}
	}
	if psEng.Name == "" || psEng.Deletable {
		t.Fatalf("managed install depended on by external hardlink must not be Deletable: %+v", psEng)
	}
	if _, err := s.InspectDeleteAsset("engine", psEng.Name); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable from InspectDeleteAsset, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", psEng.Name); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable from DeleteManagedAsset, got %+v", err)
	}
}

func TestPolystrataUninstallMigrationPreservesDanglingSymlinkAndNormalizesAbsAndDataRootPack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink preservation test uses POSIX symlinks")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	wsDataDir := filepath.Join(ws, "engines", "PolyStrata-data")
	mustMkdir(t, ws, dataDir, wsDataDir)

	psDir := filepath.Join(ws, "engines", "ps-edge-mig")
	psInst := setupPolystrataInstallFixture(t, psDir, wsDataDir, "v0.1.45-em", true)

	// 1. Dangling symlink in dataDir pointing to root config in psDir during staging/rollback.
	linkPack := filepath.Join(dataDir, "packs", "link-pack")
	mustWriteFile(t, filepath.Join(linkPack, "w.bin"), bytes.Repeat([]byte("l"), 100), 0o600)
	rootLinkCfg := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-link.json"), "Link Model", []string{"--pack", linkPack})
	destSymlink := filepath.Join(dataDir, "strata-link.json")
	if err := os.Symlink(rootLinkCfg, destSymlink); err != nil {
		t.Fatal(err)
	}

	// 2. Non-canonical abs path containing "..” through psDir into wsDataDir.
	wsWeight := filepath.Join(wsDataDir, "weights.gguf")
	mustWriteFile(t, wsWeight, bytes.Repeat([]byte("n"), 200), 0o600)
	dotDotPath := psDir + "/../PolyStrata-data/weights.gguf"
	_ = writeStrataConfigFixture(t, filepath.Join(psDir, "strata-dotdot.json"), "DotDot Model", []string{"--model", dotDotPath})

	// 3. --pack pointing to managed dataDir root itself.
	_ = writeStrataConfigFixture(t, filepath.Join(psDir, "strata-dd-pack.json"), "RootDD Pack", []string{"--pack", wsDataDir})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.paths = &BoundPaths{
		CLI: os.Args[0], Workspace: ws, Engines: filepath.Join(ws, "engines"),
		Models: modelsRoot, Results: filepath.Join(base, "other-results"),
	}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	if err := s.DeleteManagedAsset("engine", engName); err == nil || err.Details["reason"] != "paths_bound" {
		t.Fatalf("expected paths_bound failure on rollback test, got %+v", err)
	}
	if target, err := os.Readlink(destSymlink); err != nil || target != rootLinkCfg {
		t.Fatalf("existing symlink must remain untouched after uninstall rollback: target=%q err=%v", target, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "strata-link-retained-1.json")); !os.IsNotExist(err) {
		t.Fatalf("rolled-back retained config file must be removed on failure, stat err=%v", err)
	}

	s.mu.Lock()
	s.paths = nil
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("engine", engName); err != nil {
		t.Fatalf("uninstall failed: %+v", err)
	}
	if target, err := os.Readlink(destSymlink); err != nil || target != rootLinkCfg {
		t.Fatalf("user symlink in dataDir must stay un-overwritten after migration: target=%q err=%v", target, err)
	}

	findModel := func(name string) ModelAsset {
		t.Helper()
		for _, m := range s.Registry().Models {
			if m.Name == name {
				return m
			}
		}
		t.Fatalf("missing migrated model %q in %+v", name, s.Registry().Models)
		return ModelAsset{}
	}
	dotDot := findModel("DotDot Model")
	ddPack := findModel("RootDD Pack")
	if !dotDot.Available || !ddPack.Available || !ddPack.Deletable {
		t.Fatalf("expected DotDot & RootDD Pack Available after uninstall: dotDot=%+v ddPack=%+v", dotDot, ddPack)
	}
	if slices.ContainsFunc(s.Settings().Settings.EngineInstallations, func(p string) bool { return p != "" }) {
		t.Fatalf("expected empty installations after uninstall")
	}
	if err := s.DeleteManagedAsset("model", ddPack.Name); err != nil {
		t.Fatalf("deleting RootDD Pack config failed: %+v", err)
	}
	if _, err := os.Stat(wsWeight); err != nil {
		t.Fatalf("wsDataDir root and contents must stay intact when deleting --pack == wsDataDir config: %v", err)
	}
}

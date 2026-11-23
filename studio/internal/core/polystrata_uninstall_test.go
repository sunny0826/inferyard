package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func setupPolystrataInstallFixture(t *testing.T, dir, dataDir, version string, withReceipt bool) string {
	t.Helper()
	mustWriteFile(t, filepath.Join(dir, "setup.py"), []byte("print('ok')\n"), 0o644)
	mustWriteFile(t, filepath.Join(dir, "serve", "server.py"), []byte("print('srv')\n"), 0o644)
	mustWriteFile(t, filepath.Join(dir, "engine-poly", "BUILD.json"), []byte(`{"version":"`+version+`"}`), 0o644)
	mustWriteFile(t, filepath.Join(dir, "runtime-payload.bin"), bytes.Repeat([]byte("r"), 2048), 0o644)
	if withReceipt {
		receipt, _ := json.Marshal(map[string]any{
			"schema_version": 1,
			"definition":     "polystrata-runtime.v1",
			"source":         "https://example.com/polystrata.tar.gz",
		})
		mustWriteFile(t, filepath.Join(dir, ".inferyard-runtime.json"), receipt, 0o600)
	}
	instPath := filepath.Join(dir, "installation.json")
	instPayload, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "polystrata",
		"working_directory": dir,
		"data_dir":          dataDir,
	})
	mustWriteFile(t, instPath, instPayload, 0o600)
	return instPath
}

func writeStrataConfigFixture(t *testing.T, path, name string, args []string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       args,
		"model_name": name,
	})
	mustWriteFile(t, path, payload, 0o600)
	return path
}

func TestDeleteManagedPolystrataEngineVenvExternalAndSafetyGuards(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	resultsRoot := filepath.Join(base, "results")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, modelsRoot, resultsRoot, dataDir)
	mustWriteFile(t, filepath.Join(resultsRoot, "run-1", "manifest.json"), []byte("{}"), 0o644)

	managedDir := filepath.Join(ws, "engines", "polystrata-managed")
	managedInst := setupPolystrataInstallFixture(t, managedDir, dataDir, "v0.1.45-poly.10", true)

	hostPython := filepath.Join(base, "uv-python", "bin", "python3.12")
	mustWriteFile(t, hostPython, []byte("#!/bin/sh\n"), 0o755)
	mustWriteFile(t, filepath.Join(managedDir, ".venv", "pyvenv.cfg"), []byte("home = "+filepath.ToSlash(base)+"\n"), 0o644)
	if runtime.GOOS != "windows" {
		mustMkdir(t, filepath.Join(managedDir, ".venv", "bin"))
		if err := os.Symlink(hostPython, filepath.Join(managedDir, ".venv", "bin", "python3")); err != nil {
			t.Fatal(err)
		}
	}

	extDir := filepath.Join(base, "external-ps")
	extInst := setupPolystrataInstallFixture(t, extDir, dataDir, "v0.1.44-ext", true)

	noReceiptDir := filepath.Join(ws, "engines", "polystrata-noreceipt")
	noReceiptInst := setupPolystrataInstallFixture(t, noReceiptDir, dataDir, "v0.1.43-nr", false)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.ResultsRoot = &resultsRoot
	s.settings.EngineInstallations = []string{managedInst, extInst, noReceiptInst}
	s.mu.Unlock()
	s.checkMu.Lock()
	s.checks[managedInst] = engineCheckEntry{key: installationKey(managedInst), verified: true, result: EngineEligibility{State: EngineCheckEligible, Family: "polystrata"}}
	s.checkNames = []engineCheckTarget{{name: "PolyStrata · v0.1.45-poly.10", path: managedInst, key: installationKey(managedInst)}}
	s.checkMu.Unlock()

	reg := s.Registry()
	var managedName, extName, noReceiptName string
	for _, e := range reg.Engines {
		switch e.Path {
		case managedInst:
			managedName = e.Name
			if !e.Deletable {
				t.Fatalf("managed PolyStrata should be Deletable: %+v", e)
			}
		case extInst:
			extName = e.Name
			if e.Deletable {
				t.Fatalf("external PolyStrata must not be Deletable: %+v", e)
			}
		case noReceiptInst:
			noReceiptName = e.Name
			if e.Deletable {
				t.Fatalf("PolyStrata without .inferyard-runtime.json must not be Deletable: %+v", e)
			}
		}
	}
	if managedName == "" || extName == "" || noReceiptName == "" {
		t.Fatalf("missing engines in registry: %+v", reg.Engines)
	}

	info, err := s.InspectDeleteAsset("engine", managedName)
	if err != nil {
		t.Fatalf("InspectDeleteAsset(managed engine) failed: %+v", err)
	}
	if info.Scope != "polystrata_engine" || info.ReclaimableBytes == nil || *info.ReclaimableBytes < 2048 || !info.KeepsModelData {
		t.Fatalf("unexpected InspectDeleteAsset result: %+v", info)
	}
	if _, err := s.InspectDeleteAsset("engine", extName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable for external PolyStrata, got %+v", err)
	}

	if err := s.DeleteManagedAsset("engine", managedName); err != nil {
		t.Fatalf("DeleteManagedAsset(managed engine) failed: %+v", err)
	}
	wait(t, func() bool { return !s.Registry().Refreshing })

	if _, statErr := os.Stat(managedDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected managedDir removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(hostPython); statErr != nil {
		t.Fatalf("external python target must remain untouched: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(resultsRoot, "run-1", "manifest.json")); statErr != nil {
		t.Fatalf("resultsRoot must remain untouched: %v", statErr)
	}
	insts := s.Settings().Settings.EngineInstallations
	if slices.Contains(insts, managedInst) || !slices.Contains(insts, extInst) || !slices.Contains(insts, noReceiptInst) {
		t.Fatalf("unexpected EngineInstallations after uninstall: %+v", insts)
	}
	if _, exists := s.EngineChecks().Engines[managedName]; exists {
		t.Fatalf("uninstalled engine should be removed from EngineChecks: %+v", s.EngineChecks().Engines)
	}
	if err := s.DeleteManagedAsset("engine", extName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable deleting external PolyStrata, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", noReceiptName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable deleting no-receipt PolyStrata, got %+v", err)
	}
}

func TestDeleteManagedPolystrataEngineRejectsEscapingSymlinkAndEvidence(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	evidenceDir := filepath.Join(ws, "engines", "polystrata-ev")
	evInst := setupPolystrataInstallFixture(t, evidenceDir, dataDir, "v0.1.45-ev", true)
	mustWriteFile(t, filepath.Join(evidenceDir, "manifest.json"), []byte("{}"), 0o644)

	installs := []string{evInst}
	var escapeDir, escapeInst, outsideSecret, linkedDir, linkedInst string
	if runtime.GOOS != "windows" {
		outsideSecret = filepath.Join(base, "outside-secret.txt")
		mustWriteFile(t, outsideSecret, []byte("keep"), 0o644)

		escapeDir = filepath.Join(ws, "engines", "polystrata-esc")
		escapeInst = setupPolystrataInstallFixture(t, escapeDir, dataDir, "v0.1.45-esc", true)
		if err := os.Symlink(outsideSecret, filepath.Join(escapeDir, "bad-link.dylib")); err != nil {
			t.Fatal(err)
		}

		realTarget := filepath.Join(base, "real-linked-ps")
		setupPolystrataInstallFixture(t, realTarget, dataDir, "v0.1.45-lnk", true)
		linkedDir = filepath.Join(ws, "engines", "polystrata-link")
		if err := os.Symlink(realTarget, linkedDir); err != nil {
			t.Fatal(err)
		}
		linkedInst = filepath.Join(linkedDir, "installation.json")
		installs = append(installs, escapeInst, linkedInst)
	}

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = installs
	s.mu.Unlock()

	reg := s.Registry()
	for _, e := range reg.Engines {
		switch e.Path {
		case evInst:
			if err := s.DeleteManagedAsset("engine", e.Name); err == nil || err.Code != "asset_not_deletable" {
				t.Fatalf("expected asset_not_deletable for sealed evidence, got %+v", err)
			}
			if _, statErr := os.Stat(evInst); statErr != nil {
				t.Fatalf("evidence install must remain intact: %v", statErr)
			}
		case escapeInst:
			if runtime.GOOS != "windows" {
				if err := s.DeleteManagedAsset("engine", e.Name); err == nil || err.Code != "asset_not_deletable" {
					t.Fatalf("expected asset_not_deletable for escaping symlink, got %+v", err)
				}
				if _, statErr := os.Stat(outsideSecret); statErr != nil {
					t.Fatalf("outsideSecret must remain intact: %v", statErr)
				}
			}
		default:
			if runtime.GOOS != "windows" && strings.Contains(e.Name, "v0.1.45-lnk") && e.Deletable {
				t.Fatalf("symlinked install dir must not be Deletable: %+v", e)
			}
		}
	}
}

func TestDeleteManagedPolystrataModelsSharedExternalAndBusyGuards(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "polystrata-main")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-poly.10", true)

	// Exclusive pack dir + exclusive split-GGUF + broken config
	packExclDir := filepath.Join(dataDir, "packs", "excl")
	mustWriteFile(t, filepath.Join(packExclDir, "weights.bin"), bytes.Repeat([]byte("a"), 120), 0o600)
	cfgExclPack := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "excl", "strata-excl-pack.json"), "Excl Pack", []string{"--pack", packExclDir})

	shard1 := filepath.Join(dataDir, "models", "split-excl", "m-00001-of-00002.gguf")
	shard2 := filepath.Join(dataDir, "models", "split-excl", "m-00002-of-00002.gguf")
	mustWriteFile(t, shard1, bytes.Repeat([]byte("s"), 150), 0o600)
	mustWriteFile(t, shard2, bytes.Repeat([]byte("s"), 250), 0o600)
	cfgSplitExcl := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-split-excl.json"), "Split Excl", []string{"--model", shard1})

	cfgBroken := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "broken", "strata-broken.json"), "Broken Pack", []string{"--model", filepath.Join(dataDir, "models", "broken", "missing.gguf")})

	// Shared assets: pack vs file-inside-pack + cross-shard split-GGUF
	sharedPackDir := filepath.Join(dataDir, "packs", "shared-pack")
	sharedPackFile := filepath.Join(sharedPackDir, "weights.gguf")
	mustWriteFile(t, sharedPackFile, bytes.Repeat([]byte("p"), 512), 0o600)
	cfgSharedPackA := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "sa", "strata-shared-a.json"), "Shared Pack A", []string{"--pack", sharedPackDir})
	cfgSharedPackB := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "sb", "strata-shared-b.json"), "Shared File B", []string{"--model", sharedPackFile})

	shShard1 := filepath.Join(dataDir, "models", "split-shared", "sh-00001-of-00002.gguf")
	shShard2 := filepath.Join(dataDir, "models", "split-shared", "sh-00002-of-00002.gguf")
	mustWriteFile(t, shShard1, bytes.Repeat([]byte("1"), 300), 0o600)
	mustWriteFile(t, shShard2, bytes.Repeat([]byte("2"), 300), 0o600)
	cfgSplitShA := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-sh-a.json"), "Split Shared A", []string{"--model", shShard1})
	cfgSplitShB := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-sh-b.json"), "Split Shared B", []string{"--model", shShard2})

	// External GGUFs (one inside modelsRoot outside polystrata-data, one completely outside modelsRoot)
	extModelInRoot := filepath.Join(modelsRoot, "shared-in-root.gguf")
	extModelOutside := filepath.Join(base, "outside.gguf")
	mustWriteFile(t, extModelInRoot, bytes.Repeat([]byte("x"), 64), 0o600)
	mustWriteFile(t, extModelOutside, bytes.Repeat([]byte("y"), 64), 0o600)
	cfgExternal := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-ext.json"), "External Refs", []string{"--model", extModelInRoot, "--mmproj", extModelOutside})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	findModel := func(cfgPath string) ModelAsset {
		t.Helper()
		for _, m := range s.Registry().Models {
			if m.Path == cfgPath {
				return m
			}
		}
		t.Fatalf("missing strata_pack model for %s", cfgPath)
		return ModelAsset{}
	}

	// Busy guard blocks both model and engine deletion
	s.mu.Lock()
	s.downloads = []*downloadEntry{{Task: DownloadTask{TaskId: "dl-1", State: "downloading"}}}
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("model", findModel(cfgExclPack).Name); err == nil || err.Details["reason"] != "workspace_busy" {
		t.Fatalf("expected workspace_busy for active download, got %+v", err)
	}
	s.mu.Lock()
	s.downloads = nil
	s.engine = &engineEntry{state: EngineState{State: "running"}}
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("engine", s.Registry().Engines[0].Name); err == nil || err.Details["reason"] != "workspace_busy" {
		t.Fatalf("expected workspace_busy for running engine, got %+v", err)
	}
	s.mu.Lock()
	s.engine = nil
	s.runs = []*runEntry{{Task: RunTask{TaskId: "run-1", State: "running"}}}
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("model", findModel(cfgExclPack).Name); err == nil || err.Details["reason"] != "workspace_busy" {
		t.Fatalf("expected workspace_busy for active run, got %+v", err)
	}
	s.mu.Lock()
	s.runs = nil
	s.mu.Unlock()

	// 1. Delete exclusive pack & split-GGUF & broken config
	for _, cfg := range []string{cfgExclPack, cfgSplitExcl, cfgBroken} {
		m := findModel(cfg)
		if !m.Deletable {
			t.Fatalf("managed strata_pack %s should be Deletable", cfg)
		}
		if err := s.DeleteManagedAsset("model", m.Name); err != nil {
			t.Fatalf("DeleteManagedAsset(%s) failed: %+v", cfg, err)
		}
		wait(t, func() bool { return !s.Registry().Refreshing })
	}
	for _, removed := range []string{packExclDir, cfgExclPack, shard1, shard2, filepath.Dir(shard1), cfgSplitExcl, cfgBroken, filepath.Dir(cfgBroken)} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat err=%v", removed, err)
		}
	}
	for _, keptContainer := range []string{dataDir, filepath.Join(dataDir, "models"), filepath.Join(dataDir, "packs")} {
		if _, err := os.Stat(keptContainer); err != nil {
			t.Fatalf("managed container %s must remain: %v", keptContainer, err)
		}
	}

	// 2. Shared asset protection
	sharedInfoA, err := s.InspectDeleteAsset("model", findModel(cfgSharedPackA).Name)
	if err != nil || !sharedInfoA.PreservedShared || *sharedInfoA.ReclaimableBytes >= 512 {
		t.Fatalf("expected PreservedShared excluding 512B pack bytes, got %+v err=%+v", sharedInfoA, err)
	}
	if err := s.DeleteManagedAsset("model", findModel(cfgSharedPackA).Name); err != nil {
		t.Fatalf("delete shared pack A failed: %+v", err)
	}
	if _, statErr := os.Stat(sharedPackFile); statErr != nil {
		t.Fatalf("shared pack file must remain when Config B still references it: %v", statErr)
	}
	if err := s.DeleteManagedAsset("model", findModel(cfgSharedPackB).Name); err != nil {
		t.Fatalf("delete remaining shared pack B failed: %+v", err)
	}
	if _, statErr := os.Stat(sharedPackFile); !os.IsNotExist(statErr) {
		t.Fatalf("unreferenced sharedPackFile should now be deleted, stat err=%v", statErr)
	}

	splitInfoA, err := s.InspectDeleteAsset("model", findModel(cfgSplitShA).Name)
	if err != nil || !splitInfoA.PreservedShared || *splitInfoA.ReclaimableBytes >= 300 {
		t.Fatalf("expected cross-shard PreservedShared, got %+v err=%+v", splitInfoA, err)
	}
	if err := s.DeleteManagedAsset("model", findModel(cfgSplitShA).Name); err != nil {
		t.Fatalf("delete split shared A failed: %+v", err)
	}
	if _, statErr := os.Stat(shShard1); statErr != nil {
		t.Fatalf("shard 1 must be preserved while Config B references shard 2: %v", statErr)
	}
	if bModel := findModel(cfgSplitShB); !bModel.Available {
		t.Fatalf("Config B must remain available while sharing shard 2: %+v", bModel)
	}

	// 3. External GGUF protection (deletes config only)
	extModel := findModel(cfgExternal)
	extInfo, err := s.InspectDeleteAsset("model", extModel.Name)
	if err != nil || extInfo.Scope != "polystrata_model_config_only" || !extInfo.PreservedExternal {
		t.Fatalf("unexpected external-ref InspectDeleteAsset result: %+v err=%+v", extInfo, err)
	}
	if err := s.DeleteManagedAsset("model", extModel.Name); err != nil {
		t.Fatalf("delete external-ref config failed: %+v", err)
	}
	if _, statErr := os.Stat(cfgExternal); !os.IsNotExist(statErr) {
		t.Fatalf("expected cfgExternal removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(extModelInRoot); statErr != nil {
		t.Fatalf("extModelInRoot must remain untouched: %v", statErr)
	}
	if _, statErr := os.Stat(extModelOutside); statErr != nil {
		t.Fatalf("extModelOutside must remain untouched: %v", statErr)
	}
}

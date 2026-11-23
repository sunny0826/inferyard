package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestPolystrataUninstallPinnedArchiveSampleAndTamperGuard(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	if len(knownPolystrataSamples) == 0 || knownPolystrataSamples[0].size != 9150 || knownPolystrataSamples[0].archiveSHA != polystrataDefaultSourceSHA256 {
		t.Fatalf("unexpected pinned sample spec: %+v", knownPolystrataSamples)
	}

	sampleBody := []byte(`{"event":"community_sample","ok":true}` + "\n")
	sum := sha256.Sum256(sampleBody)
	origSamples := slices.Clone(knownPolystrataSamples)
	t.Cleanup(func() { knownPolystrataSamples = origSamples })
	knownPolystrataSamples = append(knownPolystrataSamples, polystrataSampleSpec{
		archiveSHA: polystrataDefaultSourceSHA256,
		relPath:    polystrataPinnedSamplePath,
		size:       int64(len(sampleBody)),
		sha256:     hex.EncodeToString(sum[:]),
	})

	psDir := filepath.Join(ws, "engines", "polystrata-pinned")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-poly.10", false)
	receipt, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"definition":     "polystrata-runtime.v1",
		"source":         polystrataDefaultSourceURL,
		"archive_sha256": polystrataDefaultSourceSHA256,
	})
	mustWriteFile(t, filepath.Join(psDir, ".inferyard-runtime.json"), receipt, 0o600)
	samplePath := filepath.Join(psDir, filepath.FromSlash(polystrataPinnedSamplePath))
	mustWriteFile(t, samplePath, sampleBody, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	if info, err := s.InspectDeleteAsset("engine", engName); err != nil {
		t.Fatalf("InspectDeleteAsset should allow pinned archive sample events.jsonl, got err=%+v", err)
	} else if info.Fingerprint == "" {
		t.Fatalf("expected non-empty delete fingerprint")
	}

	// Tampered sample bytes must still be blocked as sealed evidence.
	mustWriteFile(t, samplePath, append(slices.Clone(sampleBody), 'x'), 0o600)
	if _, err := s.InspectDeleteAsset("engine", engName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable for tampered events.jsonl, got %+v", err)
	}

	// Restore exact sample bytes and verify uninstall succeeds.
	mustWriteFile(t, samplePath, sampleBody, 0o600)
	if err := s.DeleteManagedAsset("engine", engName); err != nil {
		t.Fatalf("uninstall with verified pinned archive sample failed: %+v", err)
	}
	if _, err := os.Stat(psDir); !os.IsNotExist(err) {
		t.Fatalf("expected psDir removed, stat err=%v", err)
	}
}

func TestPolystrataUninstallHardlinksCaseDedupeAndRawGGUFProtection(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	// Case-variant GGUF inside polystrata-data must still be deduplicated & protected from raw GGUF deletion.
	weightsUpper := filepath.Join(dataDir, "models", "m1", "Weights.gguf")
	weightsLower := filepath.Join(dataDir, "models", "m1", "weights.gguf")
	mustWriteFile(t, weightsUpper, bytes.Repeat([]byte("w"), 512), 0o600)
	cfgCase := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "m1", "strata-case.json"), "Case Model", []string{"--model", weightsLower})

	// Hardlinked config files sharing a weight file must NOT erase weights when deleting one entry.
	hlPackDir := filepath.Join(dataDir, "packs", "hl")
	mustWriteFile(t, filepath.Join(hlPackDir, "weights.bin"), bytes.Repeat([]byte("h"), 512), 0o600)
	cfgHLA := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "hl", "strata-hl-a.json"), "HL A", []string{"--pack", hlPackDir})
	cfgHLB := filepath.Join(dataDir, "models", "hl", "strata-hl-b.json")
	if runtime.GOOS != "windows" {
		if err := os.Link(cfgHLA, cfgHLB); err != nil {
			t.Fatal(err)
		}
	}

	scanned := []inferyard.ScannedModel{
		{Path: weightsUpper, Name: "Weights", Format: "gguf", SizeBytes: ptr(int64(512)), Available: true},
	}
	s := testService(t, assetTestExecFor(&scanned))
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.assets.Parsed = &inferyard.AssetScan{Definition: inferyard.AssetScanDefinition, Models: scanned, Engines: []inferyard.ScannedEngine{}}
	s.mu.Unlock()

	reg := s.Registry()
	for _, m := range reg.Models {
		if m.Format == "gguf" && sameFileOrPath(m.Path, weightsUpper) {
			t.Fatalf("expected case-aliased raw GGUF covered by strata-case.json to be deduplicated: %+v", m)
		}
	}

	// Simulate raw GGUF exposed in Assets and ensure raw DeleteManagedAsset still blocks active ref deletion.
	otherGGUF := filepath.Join(dataDir, "models", "m1", "other.gguf")
	mustWriteFile(t, otherGGUF, bytes.Repeat([]byte("o"), 256), 0o600)
	s.mu.Lock()
	s.assets.Parsed.Models = []inferyard.ScannedModel{{Path: otherGGUF, Name: "other", Format: "gguf", Available: true}}
	s.mu.Unlock()
	_ = writeStrataConfigFixture(t, cfgCase, "Case Model", []string{"--model", weightsLower})
	if err := s.DeleteManagedAsset("model", "Weights.gguf"); err == nil || (err.Code != "not_found" && err.Code != "asset_not_deletable") {
		t.Fatalf("unexpected result deleting covered raw gguf: %+v", err)
	}

	if runtime.GOOS != "windows" {
		hlName := ""
		for _, m := range s.Registry().Models {
			if m.Path == cfgHLA {
				hlName = m.Name
				break
			}
		}
		if hlName == "" {
			t.Fatalf("missing Registry model entry for hardlinked %s", cfgHLA)
		}
		if err := s.DeleteManagedAsset("model", hlName); err != nil {
			t.Fatalf("delete first hardlinked strata_pack failed: %+v", err)
		}
		if _, err := os.Stat(filepath.Join(hlPackDir, "weights.bin")); err != nil {
			t.Fatalf("shared pack weights must survive while hardlinked sibling config remains: %v", err)
		}
		if _, err := os.Stat(cfgHLB); err != nil {
			t.Fatalf("hardlinked sibling config must survive: %v", err)
		}
		packWithSymMarker := filepath.Join(dataDir, "packs", "sym-marker")
		mustWriteFile(t, filepath.Join(packWithSymMarker, "real.bin"), []byte("x"), 0o600)
		if err := os.Symlink("real.bin", filepath.Join(packWithSymMarker, "manifest.json")); err != nil {
			t.Fatal(err)
		}
		if err := verifyAssetTreeSafe(packWithSymMarker, true); err == nil {
			t.Fatalf("expected verifyAssetTreeSafe to reject symlink named manifest.json")
		}
	}
}

func TestPolystrataUninstallExternalDepsSaveRollbackDualCaseRootMigrateAndVerified(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	cfgDir := filepath.Join(base, "cfg")
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, cfgDir, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-main")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-main", true)
	// Root strata config referencing pack in polystrata-data must migrate on uninstall (P2-A).
	rootPack := filepath.Join(dataDir, "packs", "root-owned")
	mustWriteFile(t, filepath.Join(rootPack, "w.bin"), bytes.Repeat([]byte("r"), 300), 0o600)
	_ = writeStrataConfigFixture(t, filepath.Join(psDir, "strata-root-pack.json"), "Root Pack", []string{"--pack", rootPack})

	// Secondary external installation referencing working_directory inside psDir blocks uninstall (P1-D).
	depInst := filepath.Join(base, "ext-dep", "installation.json")
	depRaw, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": psDir, "data_dir": dataDir,
	})
	mustWriteFile(t, depInst, depRaw, 0o600)

	settingsPath := filepath.Join(cfgDir, "settings.json")
	rawSettings, _ := json.Marshal(UserSettings{
		InferyardPath:       ptr(os.Args[0]),
		WorkspaceRoot:       &ws,
		ModelsRoot:          &modelsRoot,
		EngineInstallations: []string{psInst, depInst},
	})
	mustWriteFile(t, settingsPath, rawSettings, 0o600)

	s, err := New(context.Background(), Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(base, "host.json"),
		Execute:       assetTestExec,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	// 1. Blocked while external dep exists.
	var psName string
	for _, e := range s.Registry().Engines {
		if e.Path == psInst {
			psName = e.Name
			if e.Deletable {
				t.Fatalf("engine with external dependent installation must not be Deletable: %+v", e)
			}
		}
	}
	if err := s.DeleteManagedAsset("engine", psName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable with external dependent, got %+v", err)
	}

	// Remove depInst and test surviving model config referencing file inside psDir blocks uninstall.
	_, _ = s.RemoveEngineInstallation(depInst)
	psName = s.Registry().Engines[0].Name
	mustWriteFile(t, filepath.Join(psDir, "weights", "inside.gguf"), []byte("w"), 0o600)
	survivingCfg := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "extref", "strata-in-runtime.json"), "In Runtime", []string{"--model", filepath.Join(psDir, "weights", "inside.gguf")})
	if err := s.DeleteManagedAsset("engine", psName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable when surviving config uses runtime weights, got %+v", err)
	}
	_ = os.RemoveAll(filepath.Dir(survivingCfg))

	// 2. Concurrent admission blocks downloads/runs while deletingAssets > 0.
	s.mu.Lock()
	s.deletingAssets = 1
	dlErr := s.admitDownloadLocked()
	runErr := s.admitRunLocked()
	s.deletingAssets = 0
	s.mu.Unlock()
	if dlErr == nil || dlErr.Details["reason"] != "workspace_busy" || runErr == nil || runErr.Details["reason"] != "workspace_busy" {
		t.Fatalf("expected workspace_busy during deletion: dl=%+v run=%+v", dlErr, runErr)
	}

	// 3. Failed saveSettings keeps runtime 100% intact (including serve/server.py!).
	s.mu.Lock()
	s.paths = &BoundPaths{
		CLI:       os.Args[0],
		Workspace: ws,
		Engines:   filepath.Join(ws, "engines"),
		Models:    modelsRoot,
		Results:   filepath.Join(base, "other-results"),
	}
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("engine", psName); err == nil || err.Code != "conflict" || err.Details["reason"] != "paths_bound" {
		t.Fatalf("expected saveSettings failure (paths_bound) on uninstall, got %+v", err)
	}
	if _, statErr := os.Stat(filepath.Join(psDir, "serve", "server.py")); statErr != nil {
		t.Fatalf("serve/server.py must stay intact on saveSettings failure: %v", statErr)
	}
	if !slices.Contains(s.Settings().Settings.EngineInstallations, psInst) {
		t.Fatalf("failed saveSettings must keep installation registered: %+v", s.Settings().Settings.EngineInstallations)
	}
	s.mu.Lock()
	s.paths = nil
	s.mu.Unlock()

	// 4. Verified delete rejects same-named replacement & succeeds with valid fingerprint.
	relWork := filepath.Join(base, "rel-work")
	relCfgDir := filepath.Join(dataDir, "models", "rel-split")
	mustMkdir(t, relWork)
	relShard2 := filepath.Join(relCfgDir, "r-00002-of-00002.gguf")
	mustWriteFile(t, relShard2, []byte("2"), 0o600)
	relCfg := filepath.Join(relCfgDir, "strata-rel-split.json")
	relPayload, _ := json.Marshal(map[string]any{
		"exe": "/opt/ps/strata", "cwd": relWork,
		"args": []string{"--model", "r-00001-of-00002.gguf"}, "model_name": "Rel Split",
	})
	mustWriteFile(t, relCfg, relPayload, 0o600)

	if info, ierr := s.InspectDeleteAsset("model", "Rel Split"); ierr != nil {
		t.Fatalf("inspect Rel Split failed: %+v", ierr)
	} else if verr := s.DeleteManagedAssetVerified("model", "Rel Split", info.TargetPath, "wrong-fp"); verr == nil || verr.Code != "conflict" {
		t.Fatalf("expected conflict on mismatched delete fingerprint, got %+v", verr)
	} else if verr := s.DeleteManagedAssetVerified("model", "Rel Split", info.TargetPath, info.Fingerprint); verr != nil {
		t.Fatalf("verified delete failed: %+v", verr)
	}
	if _, err := os.Stat(relShard2); !os.IsNotExist(err) {
		t.Fatalf("expected relative broken split shard 2 removed (relCfg=%s), err=%v", relCfg, err)
	}

	// 5. Dual-case duplicate installation + symlink manifest.json rejection + root config migration.
	upperInst := filepath.Join(ws, "engines", "PS-MAIN", "installation.json")
	if infoU, errU := os.Lstat(upperInst); errU == nil {
		if infoL, errL := os.Lstat(psInst); errL == nil && os.SameFile(infoU, infoL) {
			s.mu.Lock()
			s.settings.EngineInstallations = append(s.settings.EngineInstallations, upperInst)
			s.mu.Unlock()
		}
	}
	if err := s.DeleteManagedAsset("engine", psName); err != nil {
		t.Fatalf("final engine uninstall failed: %+v", err)
	}
	if len(s.Settings().Settings.EngineInstallations) != 0 {
		t.Fatalf("expected all case-variant registrations removed: %+v", s.Settings().Settings.EngineInstallations)
	}
	var foundMigrated bool
	for _, m := range s.Registry().Models {
		if m.Name == "Root Pack" && m.Deletable && m.Available {
			foundMigrated = true
		}
	}
	if !foundMigrated {
		t.Fatalf("expected root config migrated to polystrata-data and still Deletable: %+v", s.Registry().Models)
	}
}

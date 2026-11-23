package core

import (
	"bytes"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"testing"
)

func TestPolystrataUninstallDualDataRootMigrationUsesDeclaredDataDir(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	modelsDataDir := filepath.Join(modelsRoot, "polystrata-data")
	wsDataDir := filepath.Join(ws, "engines", "PolyStrata-data")
	mustMkdir(t, ws, modelsDataDir, wsDataDir)

	// Two data roots both contain packs/alpha with distinct weights sizes (17 B vs 34 B).
	modelsAlpha := filepath.Join(modelsDataDir, "packs", "alpha", "weights.bin")
	wsAlpha := filepath.Join(wsDataDir, "packs", "alpha", "weights.bin")
	mustWriteFile(t, modelsAlpha, bytes.Repeat([]byte("a"), 17), 0o600)
	mustWriteFile(t, wsAlpha, bytes.Repeat([]byte("b"), 34), 0o600)

	psDir := filepath.Join(ws, "engines", "ps-dual-dd")
	psInst := setupPolystrataInstallFixture(t, psDir, wsDataDir, "v0.1.45-dual", true)
	cfgBytes, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(psDir, ".venv", "bin", "python"),
		"args":       []string{"--pack", "packs/alpha"},
		"model_name": "Alpha Pack",
	})
	mustWriteFile(t, filepath.Join(psDir, "strata-alpha.json"), cfgBytes, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	findAlpha := func() ModelAsset {
		t.Helper()
		for _, m := range s.Registry().Models {
			if m.Name == "Alpha Pack" {
				return m
			}
		}
		t.Fatalf("missing Alpha Pack in Registry().Models: %+v", s.Registry().Models)
		return ModelAsset{}
	}

	before := findAlpha()
	if !before.Available || before.SizeBytes == nil || *before.SizeBytes != 34 {
		t.Fatalf("expected 34 B declared PolyStrata-data pack before uninstall, got %+v", before)
	}

	engName := s.Registry().Engines[0].Name
	if err := s.DeleteManagedAsset("engine", engName); err != nil {
		t.Fatalf("uninstall failed: %+v", err)
	}

	after := findAlpha()
	if !after.Available || !after.Deletable || after.SizeBytes == nil || *after.SizeBytes != 34 {
		t.Fatalf("migrated Alpha Pack must preserve original 34 B weights from wsDataDir: %+v", after)
	}
	if err := s.DeleteManagedAsset("model", after.Name); err != nil {
		t.Fatalf("deleting migrated Alpha Pack failed: %+v", err)
	}
	if _, err := os.Stat(wsAlpha); !os.IsNotExist(err) {
		t.Fatalf("expected 34 B wsDataDir pack removed, stat err=%v", err)
	}
	if _, err := os.Stat(modelsAlpha); err != nil {
		t.Fatalf("unrelated 17 B modelsDataDir pack must remain untouched: %v", err)
	}
}

func TestPolystrataUninstallPackPlanProtectsOtherInstallDataDir(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	parentPack := filepath.Join(dataDir, "packs", "parent-pack")
	nestedSubData := filepath.Join(parentPack, "sub-data")
	mustMkdir(t, ws, nestedSubData)

	keepFile := filepath.Join(nestedSubData, "keep.bin")
	mustWriteFile(t, filepath.Join(parentPack, "outer.bin"), bytes.Repeat([]byte("o"), 128), 0o600)
	mustWriteFile(t, keepFile, bytes.Repeat([]byte("k"), 256), 0o600)
	cfgPath := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "p", "strata-parent.json"), "Parent Pack", []string{"--pack", parentPack})

	otherDir := filepath.Join(ws, "engines", "ps-other")
	otherInst := setupPolystrataInstallFixture(t, otherDir, nestedSubData, "v0.1.45-other", true)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{otherInst}
	s.mu.Unlock()

	info, err := s.InspectDeleteAsset("model", "Parent Pack")
	if err != nil {
		t.Fatal(err)
	}
	if info.Scope != "polystrata_model_config_only" || !info.PreservedShared {
		t.Fatalf("expected pack enclosing other install data_dir to preserve weights: %+v", info)
	}
	if err := s.DeleteManagedAssetVerified("model", "Parent Pack", info.TargetPath, info.Fingerprint); err != nil {
		t.Fatalf("delete Parent Pack config failed: %+v", err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("expected strata-parent.json removed, stat err=%v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("other installation's data_dir contents must survive: %v", err)
	}
}

func TestPolystrataUninstallIntraConfigHardlinksAndStandaloneGGUF(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	dualDir := filepath.Join(dataDir, "models", "dual")
	mustMkdir(t, ws, dualDir)

	fileA := filepath.Join(dualDir, "a.gguf")
	fileB := filepath.Join(dualDir, "b.gguf")
	mustWriteFile(t, fileA, bytes.Repeat([]byte("x"), 200), 0o600)
	linked := os.Link(fileA, fileB) == nil

	cfgDual := writeStrataConfigFixture(t, filepath.Join(dualDir, "strata-dual-hl.json"), "Dual Hardlink", []string{
		"--model", fileA,
		"--draft-model", fileB,
	})
	altModelsRoot := filepath.Join(base, "alt-models")
	soloDataDir := filepath.Join(altModelsRoot, "polystrata-data")
	soloGGUF := filepath.Join(soloDataDir, "solo.gguf")
	mustWriteFile(t, soloGGUF, bytes.Repeat([]byte("s"), 150), 0o600)
	scanned := []inferyard.ScannedModel{
		{Path: soloGGUF, Name: "solo", Format: "gguf", SizeBytes: ptr(int64(150)), Available: true},
	}

	s := testService(t, assetTestExecFor(&scanned))
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.ExtraModelsRoots = []string{altModelsRoot}
	s.assets.Parsed = &inferyard.AssetScan{Definition: inferyard.AssetScanDefinition, Models: scanned}
	s.mu.Unlock()

	if linked {
		cfgStat, err := os.Lstat(cfgDual)
		if err != nil {
			t.Fatal(err)
		}
		info, ierr := s.InspectDeleteAsset("model", "Dual Hardlink")
		if ierr != nil {
			t.Fatal(ierr)
		}
		wantBytes := cfgStat.Size() + 200
		if info.ReclaimableBytes == nil || *info.ReclaimableBytes != wantBytes {
			t.Fatalf("expected ReclaimableBytes=%d (config + one 200 B inode), got %+v", wantBytes, info)
		}
		if derr := s.DeleteManagedAssetVerified("model", "Dual Hardlink", info.TargetPath, info.Fingerprint); derr != nil {
			t.Fatalf("delete Dual Hardlink failed: %+v", derr)
		}
		for _, p := range []string{cfgDual, fileA, fileB} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("expected all hardlinks and config %s removed, stat err=%v", p, err)
			}
		}
	}

	// Unreferenced standalone GGUF inside polystrata-data deletes only the GGUF file without removing soloDataDir.
	s.mu.Lock()
	s.settings.ModelsRoot = &altModelsRoot
	s.settings.ExtraModelsRoots = nil
	s.mu.Unlock()
	soloInfo, err := s.InspectDeleteAsset("model", "solo.gguf")
	if err != nil || !sameFileOrPath(soloInfo.TargetPath, soloGGUF) {
		t.Fatalf("expected standalone GGUF inspect to target only %s, got %+v err=%+v", soloGGUF, soloInfo, err)
	}
	if err := s.DeleteManagedAssetVerified("model", "solo.gguf", soloInfo.TargetPath, soloInfo.Fingerprint); err != nil {
		t.Fatalf("delete standalone solo.gguf failed: %+v", err)
	}
	if _, err := os.Stat(soloGGUF); !os.IsNotExist(err) {
		t.Fatalf("expected solo.gguf removed, stat err=%v", err)
	}
	if _, err := os.Stat(soloDataDir); err != nil {
		t.Fatalf("polystrata-data root must remain intact: %v", err)
	}
}

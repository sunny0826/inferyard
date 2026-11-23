package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPolystrataUninstallSymlinkConfigProtectsSharedWeights(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink config test uses POSIX symlinks")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	sharedPack := filepath.Join(dataDir, "packs", "shared-sym")
	mustMkdir(t, ws, sharedPack)

	sharedWeight := filepath.Join(sharedPack, "w.bin")
	mustWriteFile(t, sharedWeight, bytes.Repeat([]byte("s"), 256), 0o600)

	personalCfg := writeStrataConfigFixture(t, filepath.Join(base, "personal", "strata-b.json"), "Symlink Model B", []string{"--pack", sharedPack})
	if err := os.Symlink(personalCfg, filepath.Join(dataDir, "strata-b.json")); err != nil {
		t.Fatal(err)
	}
	cfgA := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "a", "strata-a.json"), "Model A", []string{"--pack", sharedPack})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.mu.Unlock()

	info, err := s.InspectDeleteAsset("model", "Model A")
	if err != nil {
		t.Fatal(err)
	}
	if info.Scope != "polystrata_model_config_only" || !info.PreservedShared {
		t.Fatalf("expected Model A delete to preserve symlinked Model B shared pack: %+v", info)
	}
	if err := s.DeleteManagedAssetVerified("model", "Model A", info.TargetPath, info.Fingerprint); err != nil {
		t.Fatalf("delete Model A failed: %+v", err)
	}
	if _, err := os.Stat(cfgA); !os.IsNotExist(err) {
		t.Fatalf("expected cfgA removed, stat err=%v", err)
	}
	if _, err := os.Stat(sharedWeight); err != nil {
		t.Fatalf("shared weight referenced by symlinked strata-b.json must survive: %v", err)
	}
}

func TestPolystrataUninstallEngineFingerprintInvalidatedWhenDataDirDisappears(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	packDir := filepath.Join(dataDir, "packs", "mig-fp")
	mustMkdir(t, ws, packDir)
	mustWriteFile(t, filepath.Join(packDir, "weights.bin"), bytes.Repeat([]byte("f"), 300), 0o600)

	psDir := filepath.Join(ws, "engines", "ps-mig-fp")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-mfp", true)
	rootCfg := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-mig.json"), "Migrated FP", []string{"--pack", packDir})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	info, err := s.InspectDeleteAsset("engine", engName)
	if err != nil || !info.KeepsModelData {
		t.Fatalf("expected inspect with KeepsModelData=true, got %+v err=%+v", info, err)
	}

	movedData := filepath.Join(base, "moved-data")
	if err := os.Rename(dataDir, movedData); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(movedData, dataDir) }()

	if verr := s.DeleteManagedAssetVerified("engine", engName, info.TargetPath, info.Fingerprint); verr == nil || verr.Code != "conflict" {
		t.Fatalf("expected delete_target_changed conflict when migration target disappears, got %+v", verr)
	}
	if _, err := os.Stat(rootCfg); err != nil {
		t.Fatalf("root config must survive rejected uninstall when migration target vanished: %v", err)
	}
}

func TestPolystrataUninstallRelativeCwdSwapBlockedAndIndependentDotDotAllowed(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	variantDir := filepath.Join(dataDir, "variant")
	mustMkdir(t, ws, variantDir)

	// Subcase A: relative cwd="variant" resolves cfgDir/w.gguf while installDir exists (no psDir/variant),
	// and would flip to variant/w.gguf after uninstall removes declWorkDir=psDir.
	wOuter := filepath.Join(dataDir, "w.gguf")
	wVar := filepath.Join(variantDir, "w.gguf")
	mustWriteFile(t, wOuter, bytes.Repeat([]byte("1"), 100), 0o600)
	mustWriteFile(t, wVar, bytes.Repeat([]byte("2"), 200), 0o600)

	psDirA := filepath.Join(ws, "engines", "ps-var")
	psInstA := setupPolystrataInstallFixture(t, psDirA, dataDir, "v0.1.45-var", true)
	varPayload, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(psDirA, ".venv", "bin", "python"),
		"cwd":        "variant",
		"args":       []string{"--model", "w.gguf"},
		"model_name": "Variant Swap",
	})
	varCfg := filepath.Join(dataDir, "strata-var.json")
	mustWriteFile(t, varCfg, varPayload, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInstA}
	s.mu.Unlock()

	engA := s.Registry().Engines[0].Name
	if _, err := s.InspectDeleteAsset("engine", engA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected InspectDeleteAsset blocked when uninstall alters relative cwd resolution, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", engA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected DeleteManagedAsset blocked when uninstall alters relative cwd resolution, got %+v", err)
	}

	// Remove varCfg and test Subcase B: --model ../weights.gguf resolving ONLY from cfgDir (dataDir/models/sub)
	// does NOT depend on psDir and uninstall succeeds cleanly.
	_ = os.Remove(varCfg)
	subCfgDir := filepath.Join(dataDir, "models", "sub")
	mustMkdir(t, subCfgDir)
	mustWriteFile(t, filepath.Join(dataDir, "models", "weights.gguf"), bytes.Repeat([]byte("w"), 128), 0o600)
	_ = writeStrataConfigFixture(t, filepath.Join(subCfgDir, "strata-rel-ok.json"), "Independent DotDot", []string{"--model", "../weights.gguf"})

	if err := s.DeleteManagedAsset("engine", engA); err != nil {
		t.Fatalf("expected uninstall to succeed for independent ../weights.gguf resolving via cfgDir: %+v", err)
	}
}

func TestPolystrataUninstallBlockedWhenOtherEngineConfigExePointsIntoRuntime(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	packDir := filepath.Join(dataDir, "packs", "shared-exe-pack")
	mustMkdir(t, ws, packDir)
	mustWriteFile(t, filepath.Join(packDir, "w.bin"), bytes.Repeat([]byte("e"), 128), 0o600)

	dirA := filepath.Join(ws, "engines", "ps-exe-a")
	instA := setupPolystrataInstallFixture(t, dirA, dataDir, "v0.1.45-ea", true)
	dirB := filepath.Join(base, "other-eng-b")
	instB := setupPolystrataInstallFixture(t, dirB, dataDir, "v0.1.45-eb", true)

	// Engine B's valid config executes binary inside Engine A's runtime.
	cfgBPayload, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(dirA, ".venv", "bin", "python"),
		"args":       []string{"--pack", packDir},
		"model_name": "B Using A Exe",
	})
	mustWriteFile(t, filepath.Join(dirB, "strata-b-uses-a.json"), cfgBPayload, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{instA, instB}
	s.mu.Unlock()

	var nameA string
	for _, e := range s.Registry().Engines {
		if e.Path == instA {
			nameA = e.Name
			if e.Deletable {
				t.Fatalf("Engine A referenced by Engine B config exe must have Deletable=false: %+v", e)
			}
		}
	}
	if nameA == "" {
		t.Fatalf("missing Engine A in Registry")
	}
	if _, err := s.InspectDeleteAsset("engine", nameA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected InspectDeleteAsset(A) asset_not_deletable, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", nameA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected DeleteManagedAsset(A) asset_not_deletable, got %+v", err)
	}

	// Verify sole-engine uninstall with A's own retained pack in dataDir (exe pointing to dirA) STILL succeeds.
	_, _ = s.RemoveEngineInstallation(instB)
	soloCfg, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(dirA, ".venv", "bin", "python"),
		"args":       []string{"--pack", packDir},
		"model_name": "Solo Retained Pack",
	})
	mustWriteFile(t, filepath.Join(dataDir, "strata-solo.json"), soloCfg, 0o600)
	nameA = s.Registry().Engines[0].Name
	if err := s.DeleteManagedAsset("engine", nameA); err != nil {
		t.Fatalf("expected sole Engine A uninstall to succeed despite A's exe in retained dataDir config: %+v", err)
	}
}

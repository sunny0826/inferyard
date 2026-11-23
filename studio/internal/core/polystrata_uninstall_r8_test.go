package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPolystrataUninstallRelativeCwdProtectsCLIResolvedSharedWeight(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	dirB := filepath.Join(ws, "engines", "ps-rel-cwd-b")
	instB := setupPolystrataInstallFixture(t, dirB, dataDir, "v0.1.45-rcb", true)
	mustWriteFile(t, filepath.Join(dirB, "variant", "model.gguf"), bytes.Repeat([]byte("v"), 64), 0o600)
	sharedWeight := filepath.Join(dataDir, "model.gguf")
	mustWriteFile(t, sharedWeight, bytes.Repeat([]byte("s"), 256), 0o600)

	// Model B declares relative cwd="variant": Studio resolves dirB/variant/model.gguf,
	// whereas CLI inspect_polystrata_payload ignores relative cwd and selects sharedWeight in dataDir.
	cfgBPayload, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(dirB, ".venv", "bin", "python"),
		"cwd":        "variant",
		"args":       []string{"--model", "model.gguf"},
		"model_name": "Rel Cwd Model B",
	})
	mustWriteFile(t, filepath.Join(dataDir, "models", "b", "strata-b.json"), cfgBPayload, 0o600)
	cfgA := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "a", "strata-a.json"), "Model A", []string{"--model", sharedWeight})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{instB}
	s.mu.Unlock()

	infoA, err := s.InspectDeleteAsset("model", "Model A")
	if err != nil {
		t.Fatal(err)
	}
	if infoA.Scope != "polystrata_model_config_only" || !infoA.PreservedShared {
		t.Fatalf("expected Model A to preserve CLI-resolved weight via relative cwd parity: %+v", infoA)
	}
	if err := s.DeleteManagedAssetVerified("model", "Model A", infoA.TargetPath, infoA.Fingerprint); err != nil {
		t.Fatalf("delete Model A failed: %+v", err)
	}
	if _, err := os.Stat(cfgA); !os.IsNotExist(err) {
		t.Fatalf("expected cfgA removed, stat err=%v", err)
	}
	if _, err := os.Stat(sharedWeight); err != nil {
		t.Fatalf("shared weight selected by CLI must remain intact: %v", err)
	}
}

func TestPolystrataUninstallBlockedWhenOtherEngineSymlinksConfigIntoRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink config test uses POSIX symlinks")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	packDir := filepath.Join(dataDir, "packs", "sym-prof")
	mustMkdir(t, ws, packDir)
	mustWriteFile(t, filepath.Join(packDir, "w.bin"), bytes.Repeat([]byte("p"), 128), 0o600)

	dirA := filepath.Join(ws, "engines", "ps-host-a")
	instA := setupPolystrataInstallFixture(t, dirA, dataDir, "v0.1.45-ha", true)
	dirB := filepath.Join(base, "other-engine-b")
	instB := setupPolystrataInstallFixture(t, dirB, dataDir, "v0.1.45-hb", true)

	// Engine B symlinks strata-b.json into dirA/profiles/worker.json (exe and pack are outside dirA).
	targetCfg := filepath.Join(dirA, "profiles", "worker.json")
	cfgPayload, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(dirB, ".venv", "bin", "python"),
		"args":       []string{"--pack", packDir},
		"model_name": "Worker Linked",
	})
	mustWriteFile(t, targetCfg, cfgPayload, 0o600)
	if err := os.Symlink(targetCfg, filepath.Join(dirB, "strata-b.json")); err != nil {
		t.Fatal(err)
	}

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
				t.Fatalf("Engine A hosting Engine B symlinked config must not be Deletable: %+v", e)
			}
		}
	}
	if _, err := s.InspectDeleteAsset("engine", nameA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected InspectDeleteAsset(A) asset_not_deletable, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", nameA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected DeleteManagedAsset(A) asset_not_deletable, got %+v", err)
	}
	if _, err := os.Stat(targetCfg); err != nil {
		t.Fatalf("linked profile target inside Engine A must remain intact: %v", err)
	}
}

func TestPolystrataUninstallOversizedConfigOver1MiBStillProtectsSharedPack(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	sharedPack := filepath.Join(dataDir, "packs", "big-shared")
	mustMkdir(t, ws, sharedPack)

	weight := filepath.Join(sharedPack, "weights.bin")
	mustWriteFile(t, weight, bytes.Repeat([]byte("w"), 256), 0o600)

	bigDoc, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--pack", sharedPack},
		"model_name": "Big Model B",
		"padding":    strings.Repeat("x", 1100*1024),
	})
	cfgBig := filepath.Join(dataDir, "models", "big", "strata-big.json")
	mustWriteFile(t, cfgBig, bigDoc, 0o600)
	cfgA := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "small", "strata-small.json"), "Small Model A", []string{"--pack", sharedPack})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.mu.Unlock()

	infoA, err := s.InspectDeleteAsset("model", "Small Model A")
	if err != nil {
		t.Fatal(err)
	}
	if infoA.Scope != "polystrata_model_config_only" || !infoA.PreservedShared {
		t.Fatalf("expected >1 MiB Model B config to protect shared pack: %+v", infoA)
	}
	if err := s.DeleteManagedAssetVerified("model", "Small Model A", infoA.TargetPath, infoA.Fingerprint); err != nil {
		t.Fatalf("delete Small Model A failed: %+v", err)
	}
	if _, err := os.Stat(cfgA); !os.IsNotExist(err) {
		t.Fatalf("expected cfgA removed, stat err=%v", err)
	}
	if _, err := os.Stat(weight); err != nil {
		t.Fatalf("shared weight referenced by >1 MiB config must survive: %v", err)
	}
}

func TestPolystrataUninstallBlockedWhenRemovingFirstEngineFlipsSharedConfigToSecondEngine(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	dirA := filepath.Join(ws, "engines", "ps-ord-a")
	instA := setupPolystrataInstallFixture(t, dirA, dataDir, "v0.1.45-oa", true)
	dirB := filepath.Join(ws, "engines", "ps-ord-b")
	instB := setupPolystrataInstallFixture(t, dirB, dataDir, "v0.1.45-ob", true)

	// No w.gguf in dirA, 100 B in dataDir, 200 B in dirB:
	// while [instA, instB] is registered, scanPolystrataModelConfigs scans shared dataDir under instA -> 100 B.
	// Uninstalling instA would shift priority to instB -> dirB/w.gguf (200 B).
	mustWriteFile(t, filepath.Join(dataDir, "w.gguf"), bytes.Repeat([]byte("1"), 100), 0o600)
	mustWriteFile(t, filepath.Join(dirB, "w.gguf"), bytes.Repeat([]byte("2"), 200), 0o600)
	_ = writeStrataConfigFixture(t, filepath.Join(dataDir, "strata-shared.json"), "Order Flip Model", []string{"--model", "w.gguf"})

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
				t.Fatalf("Engine A whose removal flips surviving model asset to Engine B must not be Deletable: %+v", e)
			}
		}
	}
	if _, err := s.InspectDeleteAsset("engine", nameA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected InspectDeleteAsset(A) asset_not_deletable, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", nameA); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected DeleteManagedAsset(A) asset_not_deletable, got %+v", err)
	}
}

func TestPolystrataUninstallInspectFailsImmediatelyOnUnreadableRootConfig(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0000 unreadable file test requires non-root POSIX environment")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-unread")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-ur", true)
	unreadCfg := filepath.Join(psDir, "strata-unreadable.json")
	mustWriteFile(t, unreadCfg, []byte(`{}`), 0o600)
	if err := os.Chmod(unreadCfg, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadCfg, 0o600) })

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	if _, err := s.InspectDeleteAsset("engine", engName); err == nil || err.Code != "internal_error" || !strings.Contains(err.Message, "strata-unreadable.json") {
		t.Fatalf("expected InspectDeleteAsset to fail loudly naming strata-unreadable.json, got %+v", err)
	}
}

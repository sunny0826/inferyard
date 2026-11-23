package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolystrataUninstallSharedRelativeConfigAcrossTwoEnginesProtectsBoth(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	dirA := filepath.Join(ws, "engines", "ps-a")
	dirB := filepath.Join(ws, "engines", "ps-b")
	instA := setupPolystrataInstallFixture(t, dirA, dataDir, "v0.1.45-a", true)
	instB := setupPolystrataInstallFixture(t, dirB, dataDir, "v0.1.45-b", true)

	// Shared config uses relative --model weights.gguf: engine A resolves it to dirA (100 B),
	// while engine B has no dirA file and resolves it to shared dataDir (200 B).
	weightA := filepath.Join(dirA, "weights.gguf")
	weightB := filepath.Join(dataDir, "weights.gguf")
	mustWriteFile(t, weightA, bytes.Repeat([]byte("a"), 100), 0o600)
	mustWriteFile(t, weightB, bytes.Repeat([]byte("b"), 200), 0o600)
	_ = writeStrataConfigFixture(t, filepath.Join(dataDir, "strata-keep.json"), "Keep Shared", []string{"--model", "weights.gguf"})

	cfgC := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "mc", "strata-c.json"), "Model C", []string{"--model", weightB})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{instA, instB}
	s.mu.Unlock()

	info, err := s.InspectDeleteAsset("model", "Model C")
	if err != nil {
		t.Fatal(err)
	}
	if info.Scope != "polystrata_model_config_only" || !info.PreservedShared {
		t.Fatalf("expected Model C to preserve weightB resolved under engine B context, got %+v", info)
	}
	if err := s.DeleteManagedAssetVerified("model", "Model C", info.TargetPath, info.Fingerprint); err != nil {
		t.Fatalf("delete Model C failed: %+v", err)
	}
	if _, err := os.Stat(cfgC); !os.IsNotExist(err) {
		t.Fatalf("expected cfgC removed, stat err=%v", err)
	}
	if got, err := os.ReadFile(weightB); err != nil || len(got) != 200 {
		t.Fatalf("shared weightB needed by engine B must remain intact: len=%d err=%v", len(got), err)
	}
}

func TestPolystrataUninstallBlockedWhenSurvivingDataDirConfigTraversesRuntime(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	wsDataDir := filepath.Join(ws, "engines", "PolyStrata-data")
	mustMkdir(t, ws, modelsRoot, wsDataDir)

	psDir := filepath.Join(ws, "engines", "ps-trav")
	psInst := setupPolystrataInstallFixture(t, psDir, wsDataDir, "v0.1.45-trav", true)
	mono := filepath.Join(wsDataDir, "mono.gguf")
	mustWriteFile(t, mono, bytes.Repeat([]byte("m"), 128), 0o600)
	_ = writeStrataConfigFixture(t, filepath.Join(wsDataDir, "models", "m", "strata-mono.json"), "Mono Trav", []string{
		"--model", psDir + "/../PolyStrata-data/mono.gguf",
	})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	if _, err := s.InspectDeleteAsset("engine", engName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected InspectDeleteAsset blocked when surviving config traverses installDir, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", engName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected DeleteManagedAsset blocked when surviving config traverses installDir, got %+v", err)
	}
	if _, err := os.Stat(psDir); err != nil {
		t.Fatalf("runtime installDir must survive when blocked: %v", err)
	}
}

func TestPolystrataUninstallRejectsMigratingPlaintextCredentialRootConfig(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	packDir := filepath.Join(dataDir, "packs", "cred-pack")
	mustMkdir(t, ws, packDir)
	mustWriteFile(t, filepath.Join(packDir, "w.bin"), bytes.Repeat([]byte("c"), 100), 0o600)

	cases := []struct {
		name        string
		extraFields map[string]any
		extraArgs   []string
		wantField   string
		secret      string
	}{
		{
			name:        "api_key",
			extraFields: map[string]any{"api_key": "sk-super-secret-value"},
			wantField:   "api_key",
			secret:      "sk-super-secret-value",
		},
		{
			name:        "env_hf_token",
			extraFields: map[string]any{"env": map[string]any{"HF_TOKEN": "hf_secret_123"}},
			wantField:   "env.HF_TOKEN",
			secret:      "hf_secret_123",
		},
		{
			name:      "args_api_key",
			extraArgs: []string{"--api-key=cli-secret-token"},
			wantField: "args",
			secret:    "cli-secret-token",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			psDir := filepath.Join(ws, "engines", "ps-cred-"+tc.name)
			psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-"+tc.name, true)
			args := append([]string{"--pack", packDir}, tc.extraArgs...)
			doc := map[string]any{
				"exe":        filepath.Join(psDir, ".venv", "bin", "python"),
				"args":       args,
				"model_name": "Cred Pack " + tc.name,
			}
			for k, v := range tc.extraFields {
				doc[k] = v
			}
			rawCfg, _ := json.Marshal(doc)
			rootCfg := filepath.Join(psDir, "strata-secret-"+tc.name+".json")
			mustWriteFile(t, rootCfg, rawCfg, 0o600)

			s := testService(t, assetTestExec)
			s.mu.Lock()
			s.settings.WorkspaceRoot = &ws
			s.settings.ModelsRoot = &modelsRoot
			s.settings.EngineInstallations = []string{psInst}
			s.mu.Unlock()

			engName := s.Registry().Engines[0].Name
			if _, err := s.InspectDeleteAsset("engine", engName); err == nil || err.Code != "asset_not_deletable" || !strings.Contains(err.Message, tc.wantField) || strings.Contains(err.Message, tc.secret) {
				t.Fatalf("unexpected InspectDeleteAsset credential error: %+v", err)
			}
			if err := s.DeleteManagedAsset("engine", engName); err == nil || err.Code != "asset_not_deletable" || !strings.Contains(err.Message, tc.wantField) || strings.Contains(err.Message, tc.secret) {
				t.Fatalf("unexpected DeleteManagedAsset credential error: %+v", err)
			}
			if _, err := os.Stat(filepath.Join(dataDir, filepath.Base(rootCfg))); !os.IsNotExist(err) {
				t.Fatalf("plaintext credential config must never be copied into dataDir: err=%v", err)
			}
			if _, err := os.Stat(rootCfg); err != nil {
				t.Fatalf("original root config must stay intact: %v", err)
			}
			_ = os.RemoveAll(psDir)
		})
	}
}

func TestPolystrataUninstallEngineFingerprintDetectsExternalHardlinkReclaimChange(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-hl-fp")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-hlfp", true)
	bigBin := filepath.Join(psDir, "runtime-blob.bin")
	mustWriteFile(t, bigBin, bytes.Repeat([]byte("r"), 2048), 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	infoBefore, err := s.InspectDeleteAsset("engine", engName)
	if err != nil {
		t.Fatal(err)
	}
	extLink := filepath.Join(base, "external-blob-link.bin")
	if err := os.Link(bigBin, extLink); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	infoAfter, err := s.InspectDeleteAsset("engine", engName)
	if err != nil {
		t.Fatal(err)
	}
	if infoBefore.ReclaimableBytes == nil || infoAfter.ReclaimableBytes == nil || *infoAfter.ReclaimableBytes >= *infoBefore.ReclaimableBytes {
		t.Fatalf("expected ReclaimableBytes to decrease after external hardlink: before=%v after=%v", infoBefore.ReclaimableBytes, infoAfter.ReclaimableBytes)
	}
	if infoBefore.Fingerprint == infoAfter.Fingerprint {
		t.Fatalf("expected engine fingerprint to change when ReclaimableBytes changes")
	}
	if err := s.DeleteManagedAssetVerified("engine", engName, infoBefore.TargetPath, infoBefore.Fingerprint); err == nil || err.Code != "conflict" {
		t.Fatalf("expected delete_target_changed conflict on stale engine fingerprint, got %+v", err)
	}
	if _, err := os.Stat(psDir); err != nil {
		t.Fatalf("engine runtime must stay intact on stale fingerprint: %v", err)
	}
}

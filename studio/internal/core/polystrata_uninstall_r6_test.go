package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
			name:        "env_hf_token_null",
			extraFields: map[string]any{"env": map[string]any{"HF_TOKEN": nil}},
			wantField:   "env.HF_TOKEN",
			secret:      "unexpected_never_echoed",
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

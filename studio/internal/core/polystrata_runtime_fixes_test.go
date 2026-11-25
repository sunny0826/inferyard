package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPolystrataMtpDeduplicationResidentMemoryAndCleanup(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(ws, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "polystrata")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "0.1.45-metal", true)

	// Stage realistic Qwen3.8-Flash-Next Q2_0 layout:
	// - packs/q2_0/dense.bin (resident)
	// - models/Q2_0/*-00001-of-00002.gguf (--native, mmap-experts) + .done
	// - models/Q2_0/*-00002-of-00002.gguf (--ple-gguf, SSD table) + .done
	// - mtp/{mtp-manifest.json, mtp-q2_0.gguf, rt/draft_vocab.bin, tensors/mtp.layers.0.mlp.experts.down_proj.bin}
	// - models/gemma/MTP/mtp-gemma.gguf (uppercase MTP auxiliary head)
	packDir := filepath.Join(dataDir, "packs", "q2_0")
	mustWriteFile(t, filepath.Join(packDir, "dense.bin"), bytes.Repeat([]byte("d"), 1024), 0o600)
	mustWriteFile(t, filepath.Join(packDir, "tokenizer", "vocab.json"), []byte("{}"), 0o600)

	shard1 := filepath.Join(dataDir, "models", "Q2_0", "Qwen-00001-of-00002.gguf")
	shard2 := filepath.Join(dataDir, "models", "Q2_0", "Qwen-00002-of-00002.gguf")
	mustWriteFile(t, shard1, bytes.Repeat([]byte("n"), 10000), 0o600)
	mustWriteFile(t, shard1+".done", []byte("ok\n"), 0o600)
	mustWriteFile(t, shard2, bytes.Repeat([]byte("p"), 20000), 0o600)
	mustWriteFile(t, shard2+".done", []byte("ok\n"), 0o600)

	mtpDir := filepath.Join(dataDir, "mtp")
	mtpRtDir := filepath.Join(mtpDir, "rt")
	mtpRawGGUF := filepath.Join(mtpDir, "mtp-q2_0.gguf")
	mustWriteFile(t, filepath.Join(mtpDir, "mtp-manifest.json"), []byte("{}"), 0o600)
	mustWriteFile(t, mtpRawGGUF, bytes.Repeat([]byte("g"), 5000), 0o600)
	mustWriteFile(t, filepath.Join(mtpRtDir, "draft_vocab.bin"), bytes.Repeat([]byte("v"), 200), 0o600)
	mustWriteFile(t, filepath.Join(mtpDir, "tensors", "mtp.layers.0.mlp.experts.down_proj.bin"), bytes.Repeat([]byte("e"), 5000), 0o600)

	upperMtpGGUF := filepath.Join(dataDir, "models", "gemma", "MTP", "mtp-gemma.gguf")
	mustWriteFile(t, upperMtpGGUF, bytes.Repeat([]byte("u"), 1000), 0o600)

	cfgPath := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-q2_0.json"), "qwen3.8-flash-next-q2_0", []string{
		"--pack", packDir,
		"--native", shard1,
		"--ple-gguf", shard2,
		"--mtp", mtpRtDir,
		"--max-context", "32768",
		"--mmap-experts",
	})

	scanSnap, _ := json.Marshal(map[string]any{
		"definition": "asset-scan.v1",
		"models": []map[string]any{
			{"path": shard1, "name": "Qwen", "format": "gguf", "size_bytes": 30000, "available": true},
			{"path": mtpRawGGUF, "name": "mtp-q2_0", "format": "gguf", "size_bytes": 5000, "available": true},
			{"path": upperMtpGGUF, "name": "mtp-gemma", "format": "gguf", "size_bytes": 1000, "available": true},
		},
		"engines":           []any{},
		"roots":             []any{},
		"total_model_bytes": 36000,
	})

	// 16 KB simulated Apple Silicon unified memory? Use realistic GiB scale check on ResidentBytes ratio first.
	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.EngineInstallations = []string{psInst}
	s.assets.Snapshot = scanSnap
	s.mu.Unlock()

	reg := s.Registry()
	if len(reg.Models) != 1 || reg.Models[0].Format != "strata_pack" {
		t.Fatalf("expected only 1 deduplicated strata_pack model (no leaked mtp-*.gguf), got %+v", reg.Models)
	}
	mod := reg.Models[0]
	if mod.SizeBytes == nil || mod.ResidentBytes == nil {
		t.Fatalf("expected both SizeBytes and ResidentBytes populated, got %+v", mod)
	}
	// On-disk SizeBytes includes pack (1026) + shard1 (10000) + shard2 (20000) + mtp dir (10202) = 41228.
	if *mod.SizeBytes != 41228 {
		t.Fatalf("SizeBytes=%d want 41228", *mod.SizeBytes)
	}
	// ResidentBytes excludes shard2 (--ple-gguf 20000) and mtp-q2_0.gguf (5000), and scales mmap experts to 20%:
	// pack (1026) + shard1*0.2 (2000) + mtp manifest/rt (202) + mtp experts*0.2 (1000) = 4228.
	if *mod.ResidentBytes != 4228 {
		t.Fatalf("ResidentBytes=%d want 4228", *mod.ResidentBytes)
	}

	// DeleteManagedAsset(model) must clean up packs/q2_0, mtp/ (including mtp-q2_0.gguf & tensors/), shards + .done, and prune models/Q2_0.
	delInfo, apiErr := s.InspectDeleteAsset("model", mod.Name)
	if apiErr != nil || delInfo.Scope != "polystrata_model" {
		t.Fatalf("InspectDeleteAsset failed: info=%+v err=%+v", delInfo, apiErr)
	}
	if apiErr := s.DeleteManagedAssetVerified("model", mod.Name, delInfo.TargetPath, delInfo.Fingerprint); apiErr != nil {
		t.Fatalf("DeleteManagedAssetVerified failed: %+v", apiErr)
	}
	for _, gone := range []string{cfgPath, packDir, mtpDir, shard1, shard1 + ".done", shard2, shard2 + ".done", filepath.Dir(shard1)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("expected %s deleted, stat err=%v", gone, err)
		}
	}
}

func TestApplyStandaloneArgsSkipsLlamaFlagsForPolystrata(t *testing.T) {
	gpuLayers := -1
	threads := 8
	req := StandaloneStartRequest{
		Model:       "qwen3.8-flash-next-q2_0",
		Engine:      "PolyStrata · 0.1.45-metal",
		ContextSize: 8192,
		GPULayers:   &gpuLayers,
		Threads:     &threads,
		ExtraArgs:   []string{"--fit-max-tokens"},
	}

	psStartup := applyStandaloneArgs(Startup{
		Path:   "/opt/ps/.venv/bin/python",
		Args:   []string{"/opt/ps/serve/server.py", "--engine", "strata", "--config", "/tmp/strata.json", "--port", "48857"},
		Family: "polystrata",
	}, req)
	for _, forbidden := range []string{"--n-gpu-layers", "-ngl", "--threads", "-t"} {
		if slices.Contains(psStartup.Args, forbidden) {
			t.Fatalf("polystrata startup must not receive %s: %v", forbidden, psStartup.Args)
		}
	}
	if !slices.Contains(psStartup.Args, "--fit-max-tokens") {
		t.Fatalf("expected explicit ExtraArgs preserved for polystrata, got %v", psStartup.Args)
	}

	llamaStartup := applyStandaloneArgs(Startup{
		Path:   "/opt/llama/llama-server",
		Args:   []string{"-m", "/models/m.gguf", "--port", "48857"},
		Family: "llama-cpp",
	}, req)
	if !slices.Contains(llamaStartup.Args, "--n-gpu-layers") || !slices.Contains(llamaStartup.Args, "--threads") {
		t.Fatalf("llama-cpp startup must receive --n-gpu-layers and --threads: %v", llamaStartup.Args)
	}
}

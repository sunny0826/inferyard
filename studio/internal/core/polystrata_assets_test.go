package core

import (
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"testing"
)

func TestPolystrataMultiAssetValidationAndPartialSharedBytes(t *testing.T) {
	ws := t.TempDir()
	modelsRoot := filepath.Join(ws, "models")
	psDataDir := filepath.Join(modelsRoot, "polystrata-data")
	sharedDir := filepath.Join(modelsRoot, "shared")
	psDir := filepath.Join(ws, "engines", "polystrata-multi")
	packDir := filepath.Join(psDataDir, "models", "qwen38", "pack")
	for _, d := range []string{packDir, sharedDir, psDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Multi-asset config (--pack, --native, --ple-gguf, vision.mmproj) totaling 100 B
	tokFile := filepath.Join(packDir, "tok.json")
	nativeGGUF := filepath.Join(psDataDir, "models", "qwen38", "native.gguf")
	pleGGUF := filepath.Join(psDataDir, "models", "qwen38", "ple.gguf")
	mmprojGGUF := filepath.Join(psDataDir, "models", "qwen38", "mmproj.gguf")
	for p, n := range map[string]int{tokFile: 15, nativeGGUF: 35, pleGGUF: 20, mmprojGGUF: 30} {
		if err := os.WriteFile(p, make([]byte, n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	multiCfg, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--pack", packDir, "--native", nativeGGUF, "--ple-gguf", pleGGUF},
		"vision":     map[string]any{"mmproj": mmprojGGUF},
		"model_name": "Qwen3.8 Multi-Asset",
	})
	if err := os.WriteFile(filepath.Join(psDataDir, "models", "qwen38", "strata-qwen38.json"), multiCfg, 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Partially shared config: external main (100 B) + internal draft (10 B) -> row 110 B, TotalModelBytes 110 B
	extMain := filepath.Join(sharedDir, "ext-main.gguf")
	intDraft := filepath.Join(psDataDir, "models", "int-draft.gguf")
	_ = os.WriteFile(extMain, make([]byte, 100), 0o600)
	_ = os.WriteFile(intDraft, make([]byte, 10), 0o600)
	partialCfg, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--model", extMain, "--draft-model", intDraft},
		"model_name": "Partially Shared",
	})
	if err := os.WriteFile(filepath.Join(psDir, "strata-partial.json"), partialCfg, 0o600); err != nil {
		t.Fatal(err)
	}

	instPath := filepath.Join(psDir, "installation.json")
	instPayload, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": psDir, "data_dir": psDataDir,
	})
	_ = os.WriteFile(instPath, instPayload, 0o600)

	s := engineService(t, ws)
	s.mu.Lock()
	s.settings.ModelsRoot = ptr(modelsRoot)
	s.settings.EngineInstallations = []string{instPath}
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{
			{Path: nativeGGUF, Name: "native", Format: "gguf", SizeBytes: ptr(int64(35)), Available: true},
			{Path: pleGGUF, Name: "ple", Format: "gguf", SizeBytes: ptr(int64(20)), Available: true},
			{Path: mmprojGGUF, Name: "mmproj", Format: "gguf", SizeBytes: ptr(int64(30)), Available: true},
			{Path: extMain, Name: "ext-main", Format: "gguf", SizeBytes: ptr(int64(100)), Available: true},
			{Path: intDraft, Name: "int-draft", Format: "gguf", SizeBytes: ptr(int64(10)), Available: true},
		},
		Engines: []inferyard.ScannedEngine{},
	}
	s.mu.Unlock()

	reg := s.Registry()
	byName := map[string]ModelAsset{}
	for _, m := range reg.Models {
		byName[m.Name] = m
	}
	multi := byName["Qwen3.8 Multi-Asset"]
	if !multi.Available || multi.Reason != "" || multi.SizeBytes == nil || *multi.SizeBytes != 100 {
		t.Fatalf("expected multi-asset config Available=true SizeBytes=100, got %+v", multi)
	}
	partial := byName["Partially Shared"]
	if !partial.Available || partial.SizeBytes == nil || *partial.SizeBytes != 110 {
		t.Fatalf("expected partially-shared config SizeBytes=110, got %+v", partial)
	}
	if _, ok := byName["ext-main.gguf"]; !ok {
		t.Fatalf("expected external shared GGUF to remain visible: %+v", reg.Models)
	}
	if _, ok := byName["int-draft.gguf"]; ok {
		t.Fatalf("expected internal draft GGUF to be deduplicated: %+v", reg.Models)
	}
	if reg.TotalModelBytes != 210 { // 100 (multi-asset) + 110 (ext-main + int-draft without double-count)
		t.Fatalf("TotalModelBytes = %d, want 210 (ext-main 100 must not be double-counted)", reg.TotalModelBytes)
	}

	// Remove --pack directory while GGUFs remain -> multi-asset config becomes unavailable & stops hiding raw GGUFs
	if err := os.RemoveAll(packDir); err != nil {
		t.Fatal(err)
	}
	regNoPack := s.Registry()
	byNameNoPack := map[string]ModelAsset{}
	for _, m := range regNoPack.Models {
		byNameNoPack[m.Name] = m
	}
	multiMissing := byNameNoPack["Qwen3.8 Multi-Asset"]
	if multiMissing.Available || multiMissing.Reason != "polystrata_config_model_missing" || multiMissing.SizeBytes != nil {
		t.Fatalf("expected missing pack to mark config unavailable with SizeBytes=nil: %+v", multiMissing)
	}
	if _, ok := byNameNoPack["native.gguf"]; !ok {
		t.Fatalf("broken strata_pack must not hide existing raw GGUFs: %+v", regNoPack.Models)
	}

	// Restore pack and remove vision.mmproj -> still marked unavailable
	_ = os.MkdirAll(packDir, 0o755)
	_ = os.WriteFile(tokFile, make([]byte, 15), 0o600)
	_ = os.Remove(mmprojGGUF)
	regNoMM := s.Registry()
	byNameNoMM := map[string]ModelAsset{}
	for _, m := range regNoMM.Models {
		byNameNoMM[m.Name] = m
	}
	if got := byNameNoMM["Qwen3.8 Multi-Asset"]; got.Available || got.Reason != "polystrata_config_model_missing" || got.SizeBytes != nil {
		t.Fatalf("expected missing vision.mmproj to mark config unavailable with SizeBytes=nil: %+v", got)
	}
}

func TestPolystrataUpstreamFlagsTokenizers65ShardsAndSharedSplitShards(t *testing.T) {
	ws := t.TempDir()
	modelsRoot := filepath.Join(ws, "models")
	psDataDir := filepath.Join(modelsRoot, "polystrata-data")
	sharedDir := filepath.Join(modelsRoot, "shared")
	psDir := filepath.Join(ws, "engines", "polystrata-edge")
	for _, d := range []string{psDataDir, sharedDir, psDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	mainGGUF := filepath.Join(psDataDir, "main.gguf")
	embdGGUF := filepath.Join(psDataDir, "embd.gguf")
	mtpGGUF := filepath.Join(psDataDir, "mtp.gguf")
	tokDir := filepath.Join(psDataDir, "tok")
	_ = os.MkdirAll(tokDir, 0o755)
	_ = os.WriteFile(filepath.Join(tokDir, "vocab.json"), make([]byte, 5), 0o600)
	_ = os.WriteFile(mainGGUF, make([]byte, 50), 0o600)
	_ = os.WriteFile(embdGGUF, make([]byte, 20), 0o600)
	_ = os.WriteFile(mtpGGUF, make([]byte, 10), 0o600)

	// Incomplete 65-shard GGUF (only shard 1 present)
	shard65 := filepath.Join(psDataDir, "large-00001-of-00065.gguf")
	_ = os.WriteFile(shard65, make([]byte, 212), 0o600)

	// External shared 2-shard split GGUF (212 + 100 = 312 B), scanned on shard 1 by CLI
	ext1 := filepath.Join(sharedDir, "ext-00001-of-00002.gguf")
	ext2 := filepath.Join(sharedDir, "ext-00002-of-00002.gguf")
	_ = os.WriteFile(ext1, make([]byte, 212), 0o600)
	_ = os.WriteFile(ext2, make([]byte, 100), 0o600)

	writeCfg := func(name string, payload map[string]any) {
		raw, _ := json.Marshal(payload)
		if err := os.WriteFile(filepath.Join(psDir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Valid config with numeric --mtp 1 + --embd-gguf + tokenizer
	writeCfg("strata-nummtp.json", map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--model", mainGGUF, "--embd-gguf", embdGGUF, "--mtp", "1"},
		"tokenizer":  tokDir,
		"model_name": "Numeric MTP Valid",
	})
	// Missing path-valued --mtp, missing --embd-gguf, missing tokenizer
	writeCfg("strata-miss-embd.json", map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--model", mainGGUF, "--embd-gguf", filepath.Join(psDataDir, "missing-embd.gguf")},
		"model_name": "Missing Embd",
	})
	writeCfg("strata-miss-mtp.json", map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--model", mainGGUF, "--mtp", filepath.Join(psDataDir, "missing-mtp.gguf")},
		"model_name": "Missing Path MTP",
	})
	writeCfg("strata-miss-tok.json", map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--model", mainGGUF}, "tokenizer": filepath.Join(psDataDir, "missing-tok"),
		"model_name": "Missing Tokenizer",
	})
	writeCfg("strata-65shards.json", map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--model", shard65},
		"model_name": "Incomplete 65 Shards",
	})
	writeCfg("strata-shared-split.json", map[string]any{
		"exe": "/opt/ps/strata", "args": []string{"--model", ext1},
		"model_name": "Shared Split Pack",
	})

	instPath := filepath.Join(psDir, "installation.json")
	instPayload, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": psDir, "data_dir": psDataDir,
	})
	_ = os.WriteFile(instPath, instPayload, 0o600)

	s := engineService(t, ws)
	s.mu.Lock()
	s.settings.ModelsRoot = ptr(modelsRoot)
	s.settings.EngineInstallations = []string{instPath}
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{
			{Path: mainGGUF, Name: "main", Format: "gguf", SizeBytes: ptr(int64(50)), Available: true},
			{Path: shard65, Name: "large-00001-of-00065", Format: "gguf", SizeBytes: ptr(int64(212)), Available: false, Reason: ptr("split_gguf_incomplete")},
			{Path: ext1, Name: "ext-00001-of-00002", Format: "gguf", SizeBytes: ptr(int64(312)), Available: true},
		},
		Engines: []inferyard.ScannedEngine{},
	}
	s.mu.Unlock()

	reg := s.Registry()
	byName := map[string]ModelAsset{}
	for _, m := range reg.Models {
		byName[m.Name] = m
	}

	// Numeric --mtp=1 accepted (50+20+5=75 B) and hides main.gguf
	if nm := byName["Numeric MTP Valid"]; !nm.Available || nm.SizeBytes == nil || *nm.SizeBytes != 75 {
		t.Fatalf("expected Numeric MTP Valid Available=true SizeBytes=75, got %+v", nm)
	}
	if _, ok := byName["main.gguf"]; ok {
		t.Fatalf("expected main.gguf hidden by valid strata-nummtp.json: %+v", reg.Models)
	}
	for _, missName := range []string{"Missing Embd", "Missing Path MTP", "Missing Tokenizer", "Incomplete 65 Shards"} {
		m := byName[missName]
		if m.Available || m.Reason != "polystrata_config_model_missing" || m.SizeBytes != nil {
			t.Fatalf("expected %q unavailable with SizeBytes=nil: %+v", missName, m)
		}
	}
	if _, ok := byName["large-00001-of-00065.gguf"]; !ok {
		t.Fatalf("incomplete 65-shard raw GGUF must remain visible: %+v", reg.Models)
	}
	if ss := byName["Shared Split Pack"]; !ss.Available || ss.SizeBytes == nil || *ss.SizeBytes != 312 {
		t.Fatalf("expected Shared Split Pack SizeBytes=312, got %+v", ss)
	}
	if rawExt := byName["ext-00001-of-00002.gguf"]; !rawExt.Available || rawExt.SizeBytes == nil || *rawExt.SizeBytes != 312 {
		t.Fatalf("expected external split GGUF row SizeBytes=312, got %+v", rawExt)
	}
	// TotalModelBytes: 75 (Numeric MTP) + 212 (large-00001 raw incomplete row) + 312 (external split shards counted ONCE) = 599
	if reg.TotalModelBytes != 75+212+312 {
		t.Fatalf("TotalModelBytes = %d, want 599 (external split shard 2 must not double-count)", reg.TotalModelBytes)
	}
}

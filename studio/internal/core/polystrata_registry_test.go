package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPolystrataRegistryDeduplicationNamingSizesAndAvailability(t *testing.T) {
	ws := t.TempDir()
	modelsRoot := filepath.Join(ws, "models")
	psDataDir := filepath.Join(modelsRoot, "polystrata-data")
	ggufDir := filepath.Join(psDataDir, "models", "qwen36")
	sharedDir := filepath.Join(modelsRoot, "shared")
	ps1Dir := filepath.Join(ws, "engines", "polystrata-1")
	ps2Dir := filepath.Join(ws, "engines", "polystrata-2")
	for _, d := range []string{ggufDir, sharedDir, filepath.Join(ps1Dir, "engine-poly"), ps2Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	psGGUF := filepath.Join(ggufDir, "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf")
	sharedGGUF := filepath.Join(sharedDir, "Qwen3-8B-Shared.gguf")
	draftGGUF := filepath.Join(psDataDir, "models", "qwen36", "draft.gguf")
	unreferencedGGUF := filepath.Join(psDataDir, "models", "unreferenced.gguf")
	if err := os.WriteFile(psGGUF, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(draftGGUF, make([]byte, 512), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharedGGUF, make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unreferencedGGUF, make([]byte, 1024), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgDataGGUF, _ := json.Marshal(map[string]any{
		"exe":          "/opt/ps/strata",
		"args":         []string{"--model", psGGUF, "--draft-model", draftGGUF, "--max-context", "32768"},
		"model_name":   "Qwen3.6-35B-A3B (UD-Q4_K_M)",
		"display_name": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
	})
	cfgSharedGGUF, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--model", sharedGGUF, "--max-context", "32768"},
		"model_name": "Shared via PolyStrata",
	})
	cfgMissing, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/strata",
		"args":       []string{"--model", filepath.Join(psDataDir, "models", "missing.gguf")},
		"model_name": "Missing Model",
	})
	for p, body := range map[string][]byte{
		filepath.Join(ggufDir, "strata-qwen36.json"):       cfgDataGGUF,
		filepath.Join(ps1Dir, "strata-shared.json"):        cfgSharedGGUF,
		filepath.Join(ps1Dir, "strata-missing.json"):       cfgMissing,
		filepath.Join(ps1Dir, "engine-poly", "BUILD.json"): []byte(`{"version":"v0.1.45-poly.10"}`),
	} {
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	inst1 := filepath.Join(ps1Dir, "installation.json")
	inst2 := filepath.Join(ps2Dir, "installation.json")
	writeInst := func(p, workDir string) {
		raw, _ := json.Marshal(map[string]any{
			"schema_version": 3, "definition": "local-engine-installation.v1",
			"family": "polystrata", "working_directory": workDir, "data_dir": psDataDir,
		})
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeInst(inst1, ps1Dir)
	writeInst(inst2, ps2Dir)

	s := engineService(t, ws)
	s.mu.Lock()
	s.settings.ModelsRoot = ptr(modelsRoot)
	s.settings.EngineInstallations = []string{inst1, inst2}
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models: []inferyard.ScannedModel{
			{Path: psGGUF, Name: "Qwen3.6-35B-A3B-UD-Q4_K_M", Format: "gguf", SizeBytes: ptr(int64(4096)), Available: true},
			{Path: draftGGUF, Name: "draft", Format: "gguf", SizeBytes: ptr(int64(512)), Available: true},
			{Path: sharedGGUF, Name: "Qwen3-8B-Shared", Format: "gguf", SizeBytes: ptr(int64(2048)), Available: true},
			{Path: unreferencedGGUF, Name: "unreferenced", Format: "gguf", SizeBytes: ptr(int64(1024)), Available: true},
		},
		Engines:         []inferyard.ScannedEngine{},
		TotalModelBytes: ptr(int64(4096 + 512 + 2048 + 1024)),
	}
	s.mu.Unlock()

	reg := s.Registry()
	byName := map[string]ModelAsset{}
	for _, m := range reg.Models {
		byName[m.Name] = m
		if strings.HasSuffix(m.Name, "Qwen3.6-35B-A3B-UD-Q4_K_M.gguf") || m.Name == "draft.gguf" {
			t.Fatalf("raw GGUF inside PolyStrata data_dir must be deduplicated when covered by strata-*.json: %+v", reg.Models)
		}
	}
	qwen, ok := byName["Qwen3.6-35B-A3B (UD-Q4_K_M)"]
	if !ok || qwen.Format != "strata_pack" || !qwen.Available || qwen.Deletable {
		t.Fatalf("unexpected deduplicated PolyStrata model: ok=%v asset=%+v", ok, qwen)
	}
	if qwen.SizeBytes == nil || *qwen.SizeBytes != 4096+512 {
		t.Fatalf("expected primary+draft SizeBytes=4608, got %v", qwen.SizeBytes)
	}
	if qwen.ContextLength == nil || *qwen.ContextLength != 32768 {
		t.Fatalf("expected ContextLength=32768, got %v", qwen.ContextLength)
	}

	sharedRaw, ok := byName["Qwen3-8B-Shared.gguf"]
	if !ok || sharedRaw.Format != "gguf" || !sharedRaw.Available {
		t.Fatalf("external shared GGUF outside data_dir must stay visible for llama.cpp: %+v", reg.Models)
	}
	if _, ok := byName["unreferenced.gguf"]; !ok {
		t.Fatalf("uncovered GGUF inside data_dir must remain visible even alongside invalid strata config: %+v", reg.Models)
	}
	if _, ok := byName["Shared via PolyStrata"]; !ok {
		t.Fatalf("expected PolyStrata config referencing external GGUF to remain visible: %+v", reg.Models)
	}

	missing, ok := byName["Missing Model"]
	if !ok || missing.Available || missing.Reason != "polystrata_config_model_missing" || missing.SizeBytes != nil {
		t.Fatalf("unexpected missing-asset strata_pack state: ok=%v asset=%+v", ok, missing)
	}

	// TotalModelBytes: 4096+512 (PolyStrata pack) + 2048 (shared GGUF counted once) + 1024 (unreferenced) = 7680
	if reg.TotalModelBytes != 4096+512+2048+1024 {
		t.Fatalf("TotalModelBytes = %d, want %d (shared GGUF must not double-count)", reg.TotalModelBytes, 4096+512+2048+1024)
	}

	var engNames []string
	for _, eng := range reg.Engines {
		engNames = append(engNames, eng.Name)
		if strings.Contains(eng.Name, "installation.json") {
			t.Fatalf("managed PolyStrata engine must not use literal installation.json name: %+v", eng)
		}
	}
	if !slices.Contains(engNames, "PolyStrata · v0.1.45-poly.10") || !slices.Contains(engNames, "PolyStrata · polystrata-2") {
		t.Fatalf("expected version and folder-slug PolyStrata engine names, got %v", engNames)
	}
}

func TestPolystrataSetupPopulatesEngineChecksBeforeDownloadStateEvent(t *testing.T) {
	ws := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{
		"workspace_root": ws,
		"results_root":   filepath.Join(ws, "results"),
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("e", 64)
	s, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 4 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
				payload, _ := json.Marshal(map[string]any{
					"command": "engine check", "status": "checked", "completeness": "complete",
					"run_id": nil, "evidence_dir": nil, "limitations": []string{},
					"details": map[string]any{
						"definition": "local-engine-check.v2", "eligible": true, "reason": nil,
						"family": "polystrata", "declaration_sha256": hash,
						"verification_scope":  "installation_runtime_closure_only",
						"model_requests_sent": 0, "ready_to_run": false,
					},
				})
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			outDir := args[slices.Index(args, "--out")+1]
			if err := os.MkdirAll(filepath.Join(outDir, "engine-poly"), 0o755); err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(outDir, "engine-poly", "BUILD.json"), []byte(`{"version":"v0.1.45-poly.10"}`), 0o600)
			instPath := filepath.Join(outDir, "installation.json")
			cfgPath := filepath.Join(outDir, "strata-qwen36.json")
			modelFile := filepath.Join(outDir, "model.gguf")
			_ = os.WriteFile(modelFile, []byte("gguf"), 0o600)
			_ = os.WriteFile(cfgPath, []byte(`{"exe":"/bin/true","args":["--model","model.gguf"],"model_name":"Qwen3.6-35B-A3B"}`), 0o600)
			_ = os.WriteFile(instPath, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"polystrata","working_directory":`+mustJSONQuote(outDir)+`}`), 0o600)
			payload, _ := json.Marshal(map[string]any{
				"command": "engine prepare-polystrata", "status": "completed", "completeness": "complete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{},
				"details": map[string]any{"installation_path": instPath, "model_config_path": cfgPath},
			})
			return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	ch, off, cursor := s.Bus.Subscribe()
	defer off()

	task, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36", Model: "UD-Q4_K_M"})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}

	deadline := time.After(5 * time.Second)
	for {
		events, _, next := s.Bus.After(cursor)
		cursor = next
		for _, ev := range events {
			if ev.Type != "download.state" {
				continue
			}
			var payload struct {
				Task DownloadTask `json:"task"`
			}
			if json.Unmarshal(ev.Data, &payload) == nil && payload.Task.TaskId == task.TaskId && payload.Task.State == "done" {
				checks := s.EngineChecks()
				if el, ok := checks.Engines["PolyStrata · v0.1.45-poly.10"]; !ok || el.State != EngineCheckEligible {
					t.Fatalf("expected EngineChecks() to contain eligible PolyStrata immediately when download.state done fires, got %+v", checks.Engines)
				}
				return
			}
		}
		select {
		case <-ch:
		case <-deadline:
			t.Fatal("timed out waiting for done download.state event")
		}
	}
}

func mustJSONQuote(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

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

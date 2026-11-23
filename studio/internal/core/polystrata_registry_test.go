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
	if !ok || qwen.Format != "strata_pack" || !qwen.Available || !qwen.Deletable {
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

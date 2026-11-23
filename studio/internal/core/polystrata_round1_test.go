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

func TestPolystrataRound1Regressions(t *testing.T) {
	// 1. Stage transition clears stale percent and total_bytes
	ws := t.TempDir()
	s := engineService(t, ws)
	entry := &downloadEntry{Task: DownloadTask{TaskId: "t-stage", Kind: "polystrata", State: "downloading", Stage: "downloading_runtime"}}
	ev1, ok := parseTransferProgress(`{"event":"transfer.progress","stage":"downloading_runtime","downloaded_bytes":1134,"total_bytes":1134,"bytes_per_second":500,"step":2,"step_total":4,"percent":100.0,"detail":"extracting"}`)
	if !ok {
		t.Fatal("failed to parse downloading_runtime progress")
	}
	s.applyTransferProgress(entry, ev1)
	if entry.Task.Percent == nil || *entry.Task.Percent != 100.0 || entry.Task.TotalBytes == nil {
		t.Fatalf("expected 100%% and total bytes on stage 2: %+v", entry.Task)
	}
	ev2, ok := parseTransferProgress(`{"event":"transfer.progress","stage":"setup_engine","downloaded_bytes":0,"total_bytes":null,"bytes_per_second":null,"step":3,"step_total":7,"detail":"Python packages"}`)
	if !ok {
		t.Fatal("failed to parse setup_engine progress")
	}
	s.applyTransferProgress(entry, ev2)
	if entry.Task.Percent != nil || entry.Task.TotalBytes != nil || entry.Task.BytesPerSecond != nil {
		t.Fatalf("expected stale percent/total/rate cleared on stage transition, got %+v", entry.Task)
	}

	// 2. Different PolyStrata setup request during active setup returns conflict instead of coalescing
	started := make(chan struct{})
	release := make(chan struct{})
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{"workspace_root": ws, "results_root": filepath.Join(ws, "results")})
	_ = os.WriteFile(settingsPath, rawSettings, 0o600)
	s2, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			close(started)
			select {
			case <-ctx.Done():
				return inferyard.Execution{ExitCode: inferyard.ExitCancelled}, nil
			case <-release:
				return inferyard.Execution{ExitCode: 2}, nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s2.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s2.Shutdown(ctx)
	})
	t1, apiErr := s2.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36", Model: "UD-Q4_K_M"})
	if apiErr != nil {
		t.Fatalf("first setup failed: %+v", apiErr)
	}
	<-started
	// Identical request returns same task
	tDup, apiErr := s2.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36", Model: "UD-Q4_K_M"})
	if apiErr != nil || tDup.TaskId != t1.TaskId {
		t.Fatalf("identical request should coalesce: err=%+v id=%s want=%s", apiErr, tDup.TaskId, t1.TaskId)
	}
	// Different request returns conflict
	if _, conflictErr := s2.CreatePolystrataSetup(PolystrataSetupRequest{Family: "lfm25", Model: "UD-Q3_K_XL"}); conflictErr == nil || conflictErr.Code != "conflict" || conflictErr.Details["reason"] != "polystrata_setup_active" {
		t.Fatalf("expected polystrata_setup_active conflict for different request, got %+v", conflictErr)
	}
	// Retrying a failed different PolyStrata setup while another is active must conflict (not return active task)
	s2.mu.Lock()
	failedOtherID := uuid()
	s2.downloads = append(s2.downloads, &downloadEntry{
		Task: DownloadTask{
			Kind:      "polystrata",
			TaskId:    failedOtherID,
			State:     "failed",
			SourceUrl: polystrataDefaultSourceURL,
			OutName:   "polystrata-other",
			OutDir:    filepath.Join(ws, "engines", "polystrata-other"),
		},
		polystrata: &PolystrataSetupRequest{Family: "lfm25", Model: "UD-Q3_K_XL"},
	})
	s2.mu.Unlock()
	if _, retryConflict := s2.RetryDownload(failedOtherID); retryConflict == nil || retryConflict.Code != "conflict" || retryConflict.Details["reason"] != "polystrata_setup_active" {
		t.Fatalf("expected polystrata_setup_active conflict when retrying different setup, got %+v", retryConflict)
	}

	// Normal model download keeps catalog TotalBytes when first downloading event omits total_bytes
	modelEntry := &downloadEntry{
		Task: DownloadTask{
			Kind:       "model",
			TaskId:     uuid(),
			State:      "downloading",
			Stage:      "starting",
			TotalBytes: ptr(int64(4096)),
		},
	}
	s.applyTransferProgress(modelEntry, transferProgressLine{
		Event:           "transfer.progress",
		Stage:           "downloading",
		DownloadedBytes: ptr(int64(512)),
	})
	if modelEntry.Task.TotalBytes == nil || *modelEntry.Task.TotalBytes != 4096 {
		t.Fatalf("normal model download lost catalog TotalBytes on stage transition: %+v", modelEntry.Task.TotalBytes)
	}

	// 3. Registered polystrata installation exposes both top-level and nested data_dir/models/<subDir>/strata-*.json with Deletable=false
	modelsRoot := filepath.Join(ws, "models")
	psDir := filepath.Join(ws, "engines", "polystrata-pack")
	nestedModelsDir := filepath.Join(modelsRoot, "polystrata-data", "models", "q2_pack")
	if err := os.MkdirAll(psDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nestedModelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packDir := filepath.Join(modelsRoot, "polystrata-data", "packs", "q2")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "weights.bin"), []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPayload, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/build-metal/strata",
		"args":       []string{"--pack", packDir},
		"model_name": "qwen38-q2-0",
	})
	cfgPath := filepath.Join(psDir, "strata-qwen38-q2-0.json")
	if err := os.WriteFile(cfgPath, cfgPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	nestedCfgPayload, _ := json.Marshal(map[string]any{
		"exe":        "/opt/ps/build-metal/strata",
		"args":       []string{"--pack", packDir},
		"model_name": "q2-nested",
	})
	nestedCfgPath := filepath.Join(nestedModelsDir, "strata-q2-nested.json")
	if err := os.WriteFile(nestedCfgPath, nestedCfgPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(psDir, "installation.json")
	instPayload, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "polystrata", "working_directory": psDir,
		"data_dir": filepath.Join(modelsRoot, "polystrata-data"),
	})
	if err := os.WriteFile(instPath, instPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.settings.ModelsRoot = ptr(modelsRoot)
	s.settings.EngineInstallations = []string{instPath}
	s.mu.Unlock()
	reg := s.Registry()
	foundTop, foundNested := false, false
	for _, m := range reg.Models {
		if m.Format == "strata_pack" {
			if strings.Contains(m.Path, "strata-qwen38-q2-0.json") {
				if m.Deletable {
					t.Fatalf("external strata_pack config %s must not be Deletable", m.Path)
				}
				if m.Available {
					foundTop = true
				}
			}
			if strings.Contains(m.Path, "strata-q2-nested.json") {
				if !m.Deletable {
					t.Fatalf("managed polystrata-data config %s should be Deletable", m.Path)
				}
				if m.Available {
					foundNested = true
				}
			}
		}
	}
	if !foundTop || !foundNested {
		t.Fatalf("expected both top-level and nested strata-*.json in Registry().Models (top=%v nested=%v), got %+v", foundTop, foundNested, reg.Models)
	}

	// 4. Cancelling during RegisterEngineInstallation cancels cleanly without persisting settings
	regStarted := make(chan struct{})
	s3Settings := filepath.Join(t.TempDir(), "settings3.json")
	_ = os.WriteFile(s3Settings, rawSettings, 0o600)
	s3, err := New(t.Context(), Options{
		SettingsPath: s3Settings,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
				close(regStarted)
				<-ctx.Done()
				return inferyard.Execution{ExitCode: inferyard.ExitCancelled}, ctx.Err()
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			outIdx := slices.Index(args, "--out")
			outDir := args[outIdx+1]
			_ = os.MkdirAll(outDir, 0o755)
			ip := filepath.Join(outDir, "installation.json")
			_ = os.WriteFile(ip, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"polystrata"}`), 0o600)
			okPayload, _ := json.Marshal(map[string]any{
				"command": "engine prepare-polystrata", "status": "prepared", "completeness": "complete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{},
				"details": map[string]any{"out": outDir, "installation_path": ip},
			})
			return inferyard.Execution{ExitCode: 0, Stdout: okPayload}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s3.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s3.Shutdown(ctx)
	})
	task3, apiErr := s3.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36"})
	if apiErr != nil {
		t.Fatalf("setup s3 failed: %+v", apiErr)
	}
	<-regStarted
	if _, apiErr := s3.CancelDownload(task3.TaskId); apiErr != nil {
		t.Fatalf("cancel during registration failed: %+v", apiErr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, cur := range s3.Downloads() {
			if cur.TaskId == task3.TaskId && cur.State != "downloading" {
				if cur.State != "cancelled" {
					t.Fatalf("expected cancelled state when cancelled during registration, got %+v", cur)
				}
				if len(s3.Settings().Settings.EngineInstallations) != 0 {
					t.Fatalf("cancelled registration must not persist settings: %v", s3.Settings().Settings.EngineInstallations)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("s3 task did not finish after cancel during registration")
}

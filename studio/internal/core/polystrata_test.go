package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecommendedStarterFallbackAndSnapshots(t *testing.T) {
	ws := t.TempDir()
	s := engineService(t, ws)

	// 1. Without a device snapshot, returns safe static llama-cpp fallback
	rec := s.RecommendedStarter()
	if rec.FromSnapshot || rec.Status != "fallback_static" || rec.Engine != "llama-cpp" || rec.ModelID != "model-qwen3-0.6b-q8" {
		t.Fatalf("unexpected static fallback recommendation: %+v", rec)
	}
	if len(rec.Alternatives) == 0 {
		t.Fatal("expected static fallback alternatives")
	}

	// 2. With CLI hardware snapshot recommending PolyStrata
	s.mu.Lock()
	s.device.Snapshot = json.RawMessage(`{
		"kind": "device_preflight.v1",
		"hardware": {"platform": "Darwin"},
		"models": [],
		"recommendation": {"mode": "metal"},
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "polystrata",
			"backend": "metal",
			"model_id": "polystrata-qwen36-ud-q4-k-m",
			"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
			"family": "qwen36",
			"size": "UD-Q4_K_M",
			"engine_core": "strata-poly",
			"model_format": "gguf",
			"fit_level": "fits",
			"recommended_context": 32768,
			"estimated_download_bytes": 22663387424,
			"estimated_ram_bytes": 28700000000,
			"disk_free_bytes": 150000000000,
			"reason": "polystrata_hardware_fit",
			"gpu": {"name": "Apple M3 Max", "vendor": "apple", "unified_memory_bytes": 68719476736},
			"notes": [],
			"alternatives": [{
				"engine": "llama-cpp", "backend": "metal",
				"model_id": "model-qwen3-4b-q4km", "model_title": "Qwen3-4B-Q4_K_M（GGUF）",
				"model_format": "gguf", "fit_level": "fits",
				"recommended_context": 8192, "estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944
			}]
		},
		"platform_capabilities": {"live_supported": true}
	}`)
	s.mu.Unlock()

	psRec := s.RecommendedStarter()
	if !psRec.FromSnapshot || psRec.Status != "recommended" || psRec.Engine != "polystrata" || psRec.Backend != "metal" ||
		psRec.Family == nil || *psRec.Family != "qwen36" || psRec.GPU == nil || psRec.GPU.Name != "Apple M3 Max" ||
		psRec.GPU.UnifiedMemoryBytes == nil || *psRec.GPU.UnifiedMemoryBytes != 68719476736 ||
		len(psRec.Alternatives) != 1 {
		t.Fatalf("unexpected PolyStrata recommendation: %+v", psRec)
	}

	// 3. With older CLI snapshot lacking starter_recommendation, falls back cleanly
	s.mu.Lock()
	s.device.Snapshot = json.RawMessage(`{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}`)
	s.mu.Unlock()
	olderRec := s.RecommendedStarter()
	if olderRec.FromSnapshot || olderRec.Status != "fallback_static" || olderRec.Engine != "llama-cpp" {
		t.Fatalf("expected static fallback for older snapshot, got %+v", olderRec)
	}
}

func TestPolystrataCatalogRegistration(t *testing.T) {
	reg := DefaultEngineCatalog()
	ps, ok := reg.FamilyByID("polystrata")
	if !ok {
		t.Fatal("expected polystrata in DefaultEngineCatalog()")
	}
	if !ps.SupportsWizard || ps.ExternalApp || len(ps.Releases) != 1 {
		t.Fatalf("unexpected polystrata catalog entry: %+v", ps)
	}
	for _, plat := range []string{"darwin/arm64", "windows/amd64", "linux/amd64"} {
		builds := ps.Releases[0].BuildsForPlatform(plat)
		if len(builds) != 1 || builds[0].AcquireMode != EngineAcquireManagedSetup {
			t.Fatalf("platform %s builds = %+v, want 1 managed_setup build", plat, builds)
		}
	}
}

func TestCreatePolystrataSetupStreamRetryAndAutoRegister(t *testing.T) {
	ws := t.TempDir()
	modelsDir := filepath.Join(ws, "models")
	if err := os.MkdirAll(modelsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{
		"workspace_root": ws,
		"results_root":   filepath.Join(ws, "results"),
		"models_root":    modelsDir,
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var recordedArgs [][]string
	attempt := 0
	hash := strings.Repeat("c", 64)

	execFn := func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
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
					"verification_scope": "installation_runtime_closure_only",
					"model_requests_sent": 0, "ready_to_run": false,
				},
			})
			return inferyard.Execution{Stdout: payload}, nil
		}
		return inferyard.Execution{ExitCode: 0}, nil
	}

	execStreamFn := func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
		mu.Lock()
		attempt++
		curAttempt := attempt
		recordedArgs = append(recordedArgs, append([]string(nil), args...))
		mu.Unlock()

		outIdx := slices.Index(args, "--out")
		outDir := args[outIdx+1]
		logPath := filepath.Join(outDir, ".inferyard-setup.log")

		if curAttempt == 1 {
			// First attempt fails preflight with structured blocker
			if onLine != nil {
				onLine(`{"event":"transfer.progress","stage":"preflight","downloaded_bytes":0,"total_bytes":null,"bytes_per_second":null,"step":1,"step_total":4,"detail":"checking prerequisites"}`)
			}
			errPayload, _ := json.Marshal(map[string]any{
				"command": "engine prepare-polystrata", "status": "blocked", "completeness": "incomplete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{"polystrata_xcode_clt_missing"},
				"details": map[string]any{
					"failure_stage": "preflight", "failure_cause": "polystrata_xcode_clt_missing",
					"blockers": []map[string]any{{
						"code": "polystrata_xcode_clt_missing", "message": "Xcode CLT missing",
						"fix_command": "xcode-select --install",
					}},
				},
			})
			return inferyard.Execution{ExitCode: 2, Stdout: errPayload}, nil
		}

		// Second attempt (retry with --resume) streams progress and writes installation.json
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		instPath := filepath.Join(outDir, "installation.json")
		cfgPath := filepath.Join(outDir, "strata-qwen36.json")
		_ = os.WriteFile(logPath, []byte("ok\n"), 0o600)
		_ = os.WriteFile(cfgPath, []byte(`{"exe":"/bin/true","args":[]}`), 0o600)
		_ = os.WriteFile(instPath, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"polystrata"}`), 0o600)

		if onLine != nil {
			onLine(`{"event":"transfer.progress","stage":"downloading_runtime","downloaded_bytes":1024,"total_bytes":2048,"bytes_per_second":512.0,"step":2,"step_total":4,"percent":50.0,"detail":"downloading runtime"}`)
			onLine(`{"event":"transfer.progress","stage":"setup_engine","downloaded_bytes":0,"total_bytes":null,"bytes_per_second":null,"step":4,"step_total":7,"percent":75.0,"detail":"building strata-poly"}`)
			onLine(`{"event":"transfer.progress","stage":"downloading_model","downloaded_bytes":500,"total_bytes":1000,"bytes_per_second":250.0,"step":5,"step_total":7,"percent":50.0,"detail":"Qwen3.6.gguf"}`)
			onLine(`{"event":"transfer.progress","stage":"verifying","downloaded_bytes":0,"total_bytes":null,"bytes_per_second":null,"step":4,"step_total":4,"percent":100.0,"detail":"verified"}`)
		}
		okPayload, _ := json.Marshal(map[string]any{
			"command": "engine prepare-polystrata", "status": "prepared", "completeness": "complete",
			"run_id": nil, "evidence_dir": nil, "limitations": []string{},
			"details": map[string]any{
				"out": outDir, "installation_path": instPath,
				"model_config_path": cfgPath, "log_path": logPath,
			},
		})
		return inferyard.Execution{ExitCode: 0, Stdout: okPayload}, nil
	}

	s, err := New(t.Context(), Options{SettingsPath: settingsPath, Execute: execFn, ExecStream: execStreamFn})
	if err != nil {
		t.Fatal(err)
	}
	s.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	waitTask := func(id string) DownloadTask {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, task := range s.Downloads() {
				if task.TaskId == id && task.State != "downloading" {
					return task
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("task %s did not finish in time", id)
		return DownloadTask{}
	}

	// Attempt 1: fails with preflight blocker
	t1, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{
		Family: "qwen36", Model: "UD-Q4_K_M", Context: 32768, ModelSource: "modelscope",
	})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}
	done1 := waitTask(t1.TaskId)
	if done1.State != "failed" || done1.Error == nil || done1.Error.Details == nil {
		t.Fatalf("expected failed task with blocker details, got %+v", done1)
	}
	if _, hasBlockers := done1.Error.Details["blockers"]; !hasBlockers {
		t.Fatalf("expected structured blockers in error details, got %+v", done1.Error.Details)
	}

	// Attempt 2: RetryDownload reuses OutDir with --resume and auto-registers installation.json
	t2, apiErr := s.RetryDownload(t1.TaskId)
	if apiErr != nil {
		t.Fatalf("RetryDownload failed: %+v", apiErr)
	}
	done2 := waitTask(t2.TaskId)
	if done2.State != "done" || done2.InstallationPath == nil || done2.ModelConfigPath == nil || done2.LogPath == nil {
		t.Fatalf("expected completed polystrata setup with installation/config/log paths, got %+v", done2)
	}
	if done2.OutDir != done1.OutDir {
		t.Fatalf("retry should reuse OutDir %q, got %q", done1.OutDir, done2.OutDir)
	}
	if done2.Percent == nil || *done2.Percent != 100.0 || done2.Step == nil || done2.Detail == nil {
		t.Fatalf("expected progress metadata populated, got %+v", done2)
	}

	// Verify --resume was passed on retry and installation.json was auto-registered in settings
	mu.Lock()
	retryArgs := recordedArgs[1]
	mu.Unlock()
	if !slices.Contains(retryArgs, "--resume") || !slices.Contains(retryArgs, "--family") || !slices.Contains(retryArgs, "qwen36") {
		t.Fatalf("unexpected retry CLI args: %v", retryArgs)
	}
	registered := s.Settings().Settings.EngineInstallations
	canonicalInst, _ := canonical(*done2.InstallationPath)
	if !slices.Contains(registered, canonicalInst) {
		t.Fatalf("expected %q to be auto-registered in EngineInstallations=%v", canonicalInst, registered)
	}
}

func TestCancelPolystrataSetup(t *testing.T) {
	ws := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{"workspace_root": ws, "results_root": filepath.Join(ws, "results")})
	_ = os.WriteFile(settingsPath, rawSettings, 0o600)

	started := make(chan struct{})
	s, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			close(started)
			<-ctx.Done()
			return inferyard.Execution{ExitCode: inferyard.ExitCancelled}, nil
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

	task, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36"})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}
	<-started
	if _, apiErr := s.CancelDownload(task.TaskId); apiErr != nil {
		t.Fatalf("CancelDownload failed: %+v", apiErr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, cur := range s.Downloads() {
			if cur.TaskId == task.TaskId && cur.State == "cancelled" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("polystrata setup task did not transition to cancelled")
}

func TestPolystrataHealthCheckAndGracefulUnload(t *testing.T) {
	var loaded atomic.Bool
	var unloadCalled atomic.Bool
	var wrongService atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			svc := "strata"
			if wrongService.Load() {
				svc = "other"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"service": svc,
				"loaded":  loaded.Load(),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/unload":
			if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			unloadCalled.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	// Not loaded yet -> false even on HTTP 200
	if checkEngineHealth(client, srv.URL, true, "polystrata") {
		t.Fatal("expected polystrata health=false while loaded=false")
	}
	// Loaded with wrong service -> false
	loaded.Store(true)
	wrongService.Store(true)
	if checkEngineHealth(client, srv.URL, true, "polystrata") {
		t.Fatal("expected polystrata health=false when service!=strata")
	}
	// Loaded with service==strata -> true
	wrongService.Store(false)
	if !checkEngineHealth(client, srv.URL, true, "polystrata") {
		t.Fatal("expected polystrata health=true once service==strata and loaded==true")
	}

	// Graceful unload sends POST /unload with Content-Type: application/json
	if !unloadPolystrata(srv.URL) || !unloadCalled.Load() {
		t.Fatal("expected POST /unload with application/json to succeed before stopping polystrata")
	}
}

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
	cfgPath := filepath.Join(psDir, "strata-qwen38-q2-0.json")
	if err := os.WriteFile(cfgPath, []byte(`{"exe":"/opt/ps/build-metal/strata","args":["--pack","/opt/data/packs/q2"],"model_name":"qwen38-q2-0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedCfgPath := filepath.Join(nestedModelsDir, "strata-q2-nested.json")
	if err := os.WriteFile(nestedCfgPath, []byte(`{"exe":"/opt/ps/build-metal/strata","args":["--pack","/opt/data/packs/q2"],"model_name":"q2-nested"}`), 0o600); err != nil {
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
			if m.Deletable {
				t.Fatalf("strata_pack config %s must not be Deletable", m.Path)
			}
			if strings.Contains(m.Path, "strata-qwen38-q2-0.json") && m.Available {
				foundTop = true
			}
			if strings.Contains(m.Path, "strata-q2-nested.json") && m.Available {
				foundNested = true
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

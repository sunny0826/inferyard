package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPolystrataSetupEmitsVerifyingInstallationAndCachesEngineCheck(t *testing.T) {
	ws := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{
		"workspace_root": ws,
		"results_root":   filepath.Join(ws, "results"),
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	var checkCalls atomic.Int32
	regReady := make(chan struct{})
	allowReg := make(chan struct{})
	hash := strings.Repeat("d", 64)

	s, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 4 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
				if checkCalls.Add(1) == 1 {
					close(regReady)
					<-allowReg
				}
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
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				t.Fatal(err)
			}
			instPath := filepath.Join(outDir, "installation.json")
			cfgPath := filepath.Join(outDir, "strata-qwen36.json")
			_ = os.WriteFile(cfgPath, []byte(`{"exe":"/bin/true","args":[]}`), 0o600)
			_ = os.WriteFile(instPath, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"polystrata"}`), 0o600)
			if onLine != nil {
				onLine(`{"event":"transfer.progress","stage":"verifying","downloaded_bytes":0,"total_bytes":null,"bytes_per_second":null,"step":4,"step_total":4,"percent":100.0,"detail":"PolyStrata installation verified"}`)
			}
			payload, _ := json.Marshal(map[string]any{
				"command": "engine prepare-polystrata", "status": "completed", "completeness": "complete",
				"run_id": nil, "evidence_dir": nil, "limitations": []string{},
				"details": map[string]any{
					"installation_path": instPath,
					"model_config_path": cfgPath,
					"log_path":          filepath.Join(outDir, ".inferyard-setup.log"),
				},
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

	_, off, cursor := s.Bus.Subscribe()
	defer off()

	task, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36", Model: "UD-Q4_K_M"})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}

	// While RegisterEngineInstallation is running, task must report verifying_installation (and not stuck at 100%).
	<-regReady
	for _, cur := range s.Downloads() {
		if cur.TaskId == task.TaskId {
			if cur.Stage != "verifying_installation" || cur.Percent != nil {
				close(allowReg)
				t.Fatalf("expected verifying_installation with cleared percent during registration, got %+v", cur)
			}
		}
	}
	close(allowReg)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, cur := range s.Downloads() {
			if cur.TaskId == task.TaskId && cur.State != "downloading" {
				if cur.State != "done" {
					t.Fatalf("expected setup done, got state=%q err=%+v task=%+v", cur.State, cur.Error, cur)
				}
				goto finished
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out waiting for polystrata setup completion")

finished:
	events, _, _ := s.Bus.After(cursor)
	var stages []string
	var verifyingPercentNil bool
	for _, ev := range events {
		if ev.Type != "download.progress" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(ev.Data, &payload) != nil {
			continue
		}
		stage, _ := payload["stage"].(string)
		stages = append(stages, stage)
		if stage == "verifying_installation" {
			verifyingPercentNil = payload["percent"] == nil
		}
	}
	if !slices.Contains(stages, "verifying_installation") || !verifyingPercentNil {
		t.Fatalf("expected verifying_installation progress event with cleared percent: stages=%v pctNil=%v", stages, verifyingPercentNil)
	}

	// VerifyEngines() must reuse cached installation check without invoking CLI a second time.
	s.VerifyEngines()
	time.Sleep(100 * time.Millisecond)
	if got := checkCalls.Load(); got != 1 {
		t.Fatalf("expected VerifyEngines to reuse cached check (1 total CLI invocation), got %d", got)
	}
	checks := s.EngineChecks()
	if checks.Checking != 0 {
		t.Fatalf("expected 0 inflight checks after cached registration, got %d", checks.Checking)
	}
	var found bool
	for _, eng := range checks.Engines {
		if eng.Family == "polystrata" && eng.State == EngineCheckEligible && eng.VerificationScope == "installation_runtime_closure_only" && eng.DeclarationSHA256 != nil && *eng.DeclarationSHA256 == hash {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cached eligible PolyStrata entry in EngineChecks(), got %+v", checks.Engines)
	}
}

func TestApplyTransferProgressClearsStaleBytesAcrossSetupSubsteps(t *testing.T) {
	ws := t.TempDir()
	s := engineService(t, ws)
	d := &downloadEntry{
		Task: DownloadTask{
			TaskId: "ps-substep",
			Kind:   "polystrata",
			State:  "downloading",
			Stage:  "setup_engine",
			Step:   ptr(3),
		},
	}

	// 1. Substep download sets TotalBytes and Percent
	s.applyTransferProgress(d, transferProgressLine{
		Stage:           "setup_engine",
		Step:            ptr(3),
		StepTotal:       ptr(4),
		DownloadedBytes: ptr(int64(10 << 20)),
		TotalBytes:      ptr(int64(20 << 20)),
		Percent:         ptr(50.0),
		BytesPerSecond:  ptr(1024.0),
		Detail:          ptr("llama.cpp.zip"),
	})
	if d.Task.TotalBytes == nil || *d.Task.TotalBytes != 20<<20 {
		t.Fatalf("expected TotalBytes set during substep download, got %+v", d.Task.TotalBytes)
	}

	// 2. CMake compile percentage within same step=3 must clear stale TotalBytes & BytesPerSecond
	s.applyTransferProgress(d, transferProgressLine{
		Stage:           "setup_engine",
		Step:            ptr(3),
		StepTotal:       ptr(4),
		DownloadedBytes: ptr(int64(0)),
		Percent:         ptr(25.0),
		Detail:          ptr("Building CXX object"),
	})
	if d.Task.TotalBytes != nil || d.Task.BytesPerSecond != nil {
		t.Fatalf("expected stale byte metrics cleared on compile percent update: total=%v rate=%v", d.Task.TotalBytes, d.Task.BytesPerSecond)
	}
	if d.Task.Percent == nil || *d.Task.Percent != 25.0 {
		t.Fatalf("expected compile Percent=25 preserved, got %v", d.Task.Percent)
	}

	// 3. Plain compiler line preserves Percent=25 while updating Detail
	s.applyTransferProgress(d, transferProgressLine{
		Stage:           "setup_engine",
		Step:            ptr(3),
		StepTotal:       ptr(4),
		DownloadedBytes: ptr(int64(0)),
		Detail:          ptr("clang++ -O2 strata.cpp"),
	})
	if d.Task.Percent == nil || *d.Task.Percent != 25.0 || d.Task.Detail == nil || *d.Task.Detail != "clang++ -O2 strata.cpp" {
		t.Fatalf("plain line should keep Percent=25 and update Detail, got pct=%v detail=%v", d.Task.Percent, d.Task.Detail)
	}

	// 4. New === Step N: === banner clears stale Percent
	s.applyTransferProgress(d, transferProgressLine{
		Stage:           "setup_engine",
		Step:            ptr(3),
		StepTotal:       ptr(4),
		DownloadedBytes: ptr(int64(0)),
		Detail:          ptr("Step 4: the PolyStrata engine"),
	})
	if d.Task.Percent != nil {
		t.Fatalf("expected new Step banner to clear stale Percent, got %v", *d.Task.Percent)
	}
}

func TestRegisterInstallationCheckDoesNotOverwriteInvalidatedCheck(t *testing.T) {
	ws := t.TempDir()
	instDir := filepath.Join(ws, "engines", "polystrata")
	if err := os.MkdirAll(instDir, 0o755); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(instDir, "installation.json")
	if err := os.WriteFile(instPath, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"polystrata"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{
		"workspace_root":       ws,
		"results_root":         filepath.Join(ws, "results"),
		"engine_installations": []string{instPath},
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	var checkCalls atomic.Int32
	regStarted := make(chan struct{})
	allowReg := make(chan struct{})
	hash := strings.Repeat("a", 64)

	s, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			if len(args) >= 4 && args[0] == "engine" && args[1] == "check" && args[2] == "--installation" {
				call := checkCalls.Add(1)
				eligible := call == 1
				var reason, decl any
				if eligible {
					decl = hash
					close(regStarted)
					<-allowReg
				} else {
					reason = "local_engine_polystrata_engine_missing"
				}
				payload, _ := json.Marshal(map[string]any{
					"command": "engine check", "status": "checked", "completeness": "complete",
					"run_id": nil, "evidence_dir": nil, "limitations": []string{},
					"details": map[string]any{
						"definition": "local-engine-check.v2", "eligible": eligible, "reason": reason,
						"family": "polystrata", "declaration_sha256": decl,
						"verification_scope":  "installation_runtime_closure_only",
						"model_requests_sent": 0, "ready_to_run": false,
					},
				})
				return inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: payload}, nil
			}
			return inferyard.Execution{}, nil
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

	regDone := make(chan *ApiError, 1)
	go func() {
		_, apiErr := s.RegisterEngineInstallation(t.Context(), instPath)
		regDone <- apiErr
	}()

	<-regStarted
	s.InvalidateEngineChecks()

	deadline := time.Now().Add(5 * time.Second)
	var latest EngineEligibility
	for time.Now().Before(deadline) {
		ov := s.EngineChecks()
		for _, eng := range ov.Engines {
			if eng.Family == "polystrata" {
				latest = eng
			}
		}
		if ov.Checking == 0 && latest.State == EngineCheckIneligible {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if latest.State != EngineCheckIneligible {
		close(allowReg)
		t.Fatalf("expected invalidated check to finish as ineligible, got %+v", latest)
	}

	close(allowReg)
	if apiErr := <-regDone; apiErr != nil {
		t.Fatalf("RegisterEngineInstallation unexpected error: %+v", apiErr)
	}

	ov := s.EngineChecks()
	for _, eng := range ov.Engines {
		if eng.State != EngineCheckIneligible {
			t.Fatalf("stale RegisterEngineInstallation overwrote invalidated check: %+v", ov.Engines)
		}
	}
}

func TestPolystrataSetupAndRetryPreserveModelSource(t *testing.T) {
	ws := t.TempDir()
	s := engineService(t, ws)
	s.execStream = func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
		payload, _ := json.Marshal(map[string]any{
			"command": "engine prepare-polystrata", "status": "failed", "completeness": "failed",
			"run_id": nil, "evidence_dir": nil, "limitations": []string{"download_failed"},
		})
		return inferyard.Execution{ExitCode: inferyard.ExitToolError, Stdout: payload}, nil
	}

	task, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{
		Family:      "qwen36",
		Model:       "UD-Q4_K_M",
		ModelSource: "modelscope",
	})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}
	if task.ModelSource != "modelscope" {
		t.Fatalf("expected ModelSource=modelscope on created task, got %q", task.ModelSource)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		failed := false
		for _, cur := range s.Downloads() {
			if cur.TaskId == task.TaskId && cur.State == "failed" {
				failed = true
				break
			}
		}
		if failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	retried, retryErr := s.RetryDownload(task.TaskId)
	if retryErr != nil {
		t.Fatalf("RetryDownload failed: %+v", retryErr)
	}
	if retried.ModelSource != "modelscope" {
		t.Fatalf("expected retried task to preserve ModelSource=modelscope, got %q", retried.ModelSource)
	}
	retryDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(retryDeadline) {
		done := false
		for _, cur := range s.Downloads() {
			if cur.TaskId == retried.TaskId && cur.State != "downloading" {
				done = true
				break
			}
		}
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

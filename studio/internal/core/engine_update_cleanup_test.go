package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLocalInstallationFriendlyDisplayNames(t *testing.T) {
	_, workspace := isolatedHomeAndBase(t)
	s := testService(t, assetTestExec)
	s.settings.WorkspaceRoot = &workspace

	// 1) Managed mlx-lm with receipt Version -> "mlx-lm · 0.32.2".
	managedDir := filepath.Join(workspace, "engines", "mlx-lm-0.32.2")
	managedInst := filepath.Join(managedDir, "installation.json")
	mustWriteFile(t, managedInst, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"mlx-lm","executable":"python","environment":{}}`), 0o600)
	if err := writeManagedEngineReceipt(managedDir, managedEngineReceipt{
		Definition: managedEngineDefinition, Family: "mlx-lm", Version: "0.32.2", InstallMode: managedEngineVenvInstallMode,
	}); err != nil {
		t.Fatal(err)
	}

	// 2) External custom directory -> "vLLM · custom-vllm".
	extVllmInst := filepath.Join(workspace, "external", "custom-vllm", "installation.json")
	mustWriteFile(t, extVllmInst, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"vllm","executable":"python","environment":{}}`), 0o600)

	// 3) Parent directory matching family name -> "SGLang".
	sameNameInst := filepath.Join(workspace, "external", "sglang", "installation.json")
	mustWriteFile(t, sameNameInst, []byte(`{"schema_version":3,"definition":"local-engine-installation.v1","family":"sglang","executable":"python","environment":{}}`), 0o600)

	s.settings.EngineInstallations = []string{managedInst, extVllmInst, sameNameInst}

	namesByPath := map[string]string{}
	for _, eng := range s.Registry().Engines {
		if strings.Contains(eng.Name, "installation.json") {
			t.Fatalf("internal filename installation.json leaked into EngineAsset.Name: %+v", eng)
		}
		namesByPath[eng.Path] = eng.Name
	}
	if got := namesByPath[managedInst]; got != "mlx-lm · 0.32.2" {
		t.Fatalf("managed mlx-lm name = %q, want %q", got, "mlx-lm · 0.32.2")
	}
	if got := namesByPath[extVllmInst]; got != "vLLM · custom-vllm" {
		t.Fatalf("external vllm name = %q, want %q", got, "vLLM · custom-vllm")
	}
	if got := namesByPath[sameNameInst]; got != "SGLang" {
		t.Fatalf("same-dir sglang name = %q, want %q", got, "SGLang")
	}
}

func TestLlamaCppPrismOutNameAndUpstreamBinaryReceipt(t *testing.T) {
	if got := sanitizeEngineUpdateOutName("llama-cpp-prism", "prism-b12232-f702476"); got != "llama-cpp-prism-b12232-f702476" {
		t.Fatalf("sanitizeEngineUpdateOutName duplicated prism prefix: got %q", got)
	}
	if got := sanitizeEngineUpdateOutName("llama-cpp", "b12500"); got != "llama-cpp-b12500" {
		t.Fatalf("sanitizeEngineUpdateOutName(llama-cpp, b12500) = %q", got)
	}

	prismURL := "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b12232-f702476/llama-prism-b12232-bin-macos-arm64.tar.gz"
	r1 := managedBinaryReceipt(prismURL)
	if r1.Family != "llama-cpp" || r1.Version != "prism-b12232-f702476" || r1.InstallMode != "binary" {
		t.Fatalf("unexpected upstream PrismML binary receipt: %+v", r1)
	}

	ggmlURL := "https://github.com/ggml-org/llama.cpp/releases/download/b12500/llama-b12500-bin-macos-arm64.tar.gz"
	r2 := managedBinaryReceipt(ggmlURL)
	if r2.Family != "llama-cpp" || r2.Version != "b12500" {
		t.Fatalf("unexpected upstream ggml-org binary receipt: %+v", r2)
	}
}

func TestUpdateEngineCleansReplacedManagedBinaryDirAndPreservesActiveOrExternal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows binary update requires catalog runtime profile")
	}
	binName := "llama-server"
	prismDownloadURL := "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b12232-f702476/llama-prism-b12232-bin-macos-arm64.tar.gz"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/PrismML-Eng/llama.cpp/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "prism-b12232-f702476",
				"html_url": "https://github.com/PrismML-Eng/llama.cpp/releases/tag/prism-b12232-f702476",
				"assets": []map[string]any{{
					"name":                 "llama-prism-b12232-bin-macos-arm64.tar.gz",
					"browser_download_url": prismDownloadURL,
					"size":                 1024,
				}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mockExec := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "prepare" {
			var outDir string
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "--out" {
					outDir = args[i+1]
				}
			}
			newBin := filepath.Join(outDir, binName)
			mustWriteFile(t, newBin, []byte("#!/bin/sh\n"), 0o755)
			payload, _ := json.Marshal(map[string]any{
				"command": "engine prepare", "status": "complete", "completeness": "complete", "limitations": []string{},
				"details": map[string]any{"engine": newBin, "bytes": 1024, "sha256": strings.Repeat("b", 64)},
			})
			return inferyard.Execution{Stdout: payload, ExitCode: inferyard.ExitComplete}, nil
		}
		if len(args) >= 2 && args[0] == "engine" && args[1] == "check" {
			payload, _ := json.Marshal(map[string]any{
				"command": "engine check", "status": "checked", "completeness": "complete", "limitations": []string{},
				"details": map[string]any{"eligible": true, "reported_version": "12232", "expected_release": "prism-b12232-f702476", "mode": "metal"},
			})
			return inferyard.Execution{Stdout: payload, ExitCode: inferyard.ExitComplete}, nil
		}
		return assetTestExec(context.Background(), "", args...)
	}

	newUpdateTestSvc := func(t *testing.T, wsDir string, extraRoots []string) *Service {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		cliPath := filepath.Join(t.TempDir(), "inferyard")
		mustWriteFile(t, cliPath, []byte("#!/bin/sh\n"), 0o755)
		svc, err := New(ctx, Options{
			SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
			HostStatePath: filepath.Join(t.TempDir(), "host.json"),
			Execute:       mockExec,
			EngineUpdate: EngineUpdateOptions{
				HTTPClient:        srv.Client(),
				GitHubAPIBase:     srv.URL,
				CachePath:         filepath.Join(t.TempDir(), "update-cache.json"),
				Platform:          "darwin/arm64",
				AllowInsecureHTTP: true,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { svc.Shutdown(context.Background()) })
		if len(extraRoots) > 0 {
			svc.registryRoots = func(ws string) []string {
				return append([]string{filepath.Join(ws, "engines")}, extraRoots...)
			}
		}
		svc.settingsMu.Lock()
		svc.settings.InferyardPath = &cliPath
		svc.settings.WorkspaceRoot = &wsDir
		svc.settingsMu.Unlock()
		return svc
	}

	// Case 1: Idle workspace-managed binary directory is cleaned up after successful update download.
	ws1 := t.TempDir()
	oldDir := filepath.Join(ws1, "engines", "llama-cpp-prism-b10743")
	oldBin := filepath.Join(oldDir, binName)
	mustWriteFile(t, oldBin, []byte("#!/bin/sh\n"), 0o755)
	mustWriteFile(t, filepath.Join(oldDir, "engine-prepare.json"), []byte(`{"source_url":"https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/llama-prism-b10743-bin-macos-arm64.tar.gz"}`), 0o600)

	svc1 := newUpdateTestSvc(t, ws1, nil)
	canonOldBin, _ := canonical(oldBin)
	oldEngName := svc1.Registry().Engines[0].Name
	svc1.checkMu.Lock()
	svc1.checks[canonOldBin] = engineCheckEntry{key: "old", verified: true, result: EngineEligibility{State: EngineCheckEligible, Family: "llama-cpp", Release: "prism-b10743-adfffbe"}}
	svc1.checkMu.Unlock()
	if _, err := svc1.CheckEngineUpdates(context.Background()); err != nil {
		t.Fatalf("CheckEngineUpdates: %+v", err)
	}

	task1, apiErr := svc1.UpdateEngine(oldEngName)
	if apiErr != nil {
		t.Fatalf("UpdateEngine: %+v", apiErr)
	}
	if task1.OutName != "llama-cpp-prism-b12232-f702476" {
		t.Fatalf("OutName = %q, want llama-cpp-prism-b12232-f702476", task1.OutName)
	}
	wait(t, func() bool {
		tasks := svc1.Downloads()
		return len(tasks) > 0 && (tasks[0].State == "done" || tasks[0].State == "failed")
	})
	if svc1.Downloads()[0].State != "done" {
		t.Fatalf("update download failed: %+v", svc1.Downloads()[0])
	}
	if _, statErr := os.Stat(oldDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected replaced workspace-managed directory removed, statErr=%v", statErr)
	}
	svc1.checkMu.Lock()
	_, oldCheckStillCached := svc1.checks[canonOldBin]
	svc1.checkMu.Unlock()
	if oldCheckStillCached {
		t.Fatal("expected old engine check cache entry removed after cleanup")
	}
	receipt, ok := readManagedEngineReceipt(task1.OutDir)
	if !ok || receipt.Version != "prism-b12232-f702476" || receipt.Family != "llama-cpp" {
		t.Fatalf("expected new binary receipt with prism-b12232-f702476, got ok=%v receipt=%+v", ok, receipt)
	}

	// Case 2: Running workspace-managed binary directory is preserved (never deleted while active).
	ws2 := t.TempDir()
	activeOldDir := filepath.Join(ws2, "engines", "llama-cpp-prism-b10743")
	activeOldBin := filepath.Join(activeOldDir, binName)
	mustWriteFile(t, activeOldBin, []byte("#!/bin/sh\n"), 0o755)
	mustWriteFile(t, filepath.Join(activeOldDir, "engine-prepare.json"), []byte(`{"source_url":"https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/llama-prism-b10743-bin-macos-arm64.tar.gz"}`), 0o600)

	svc2 := newUpdateTestSvc(t, ws2, nil)
	activeName := svc2.Registry().Engines[0].Name
	if _, err := svc2.CheckEngineUpdates(context.Background()); err != nil {
		t.Fatalf("CheckEngineUpdates svc2: %+v", err)
	}
	svc2.mu.Lock()
	svc2.services["svc-active"] = &engineEntry{
		cmd:   &exec.Cmd{Path: activeOldBin},
		state: EngineState{State: "running", EngineName: activeName, Executable: activeOldBin},
	}
	svc2.mu.Unlock()

	if _, apiErr := svc2.UpdateEngine(activeName); apiErr != nil {
		t.Fatalf("UpdateEngine active: %+v", apiErr)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if tasks := svc2.Downloads(); len(tasks) > 0 && tasks[0].State == "done" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if _, statErr := os.Stat(activeOldDir); statErr != nil {
		t.Fatalf("active engine directory must be preserved, got statErr=%v", statErr)
	}
	svc2.mu.Lock()
	delete(svc2.services, "svc-active")
	svc2.mu.Unlock()

	// Case 3: External (non-workspace) engine directory is preserved.
	ws3 := t.TempDir()
	extRoot := t.TempDir()
	extOldDir := filepath.Join(extRoot, "llama-prism-ext")
	extOldBin := filepath.Join(extOldDir, binName)
	mustWriteFile(t, extOldBin, []byte("#!/bin/sh\n"), 0o755)
	mustWriteFile(t, filepath.Join(extOldDir, "engine-prepare.json"), []byte(`{"source_url":"https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/llama-prism-b10743-bin-macos-arm64.tar.gz"}`), 0o600)

	svc3 := newUpdateTestSvc(t, ws3, []string{extRoot})
	extName := svc3.Registry().Engines[0].Name
	if _, err := svc3.CheckEngineUpdates(context.Background()); err != nil {
		t.Fatalf("CheckEngineUpdates svc3: %+v", err)
	}
	if _, apiErr := svc3.UpdateEngine(extName); apiErr != nil {
		t.Fatalf("UpdateEngine external: %+v", apiErr)
	}
	wait(t, func() bool {
		tasks := svc3.Downloads()
		return len(tasks) > 0 && tasks[0].State == "done"
	})
	if _, statErr := os.Stat(extOldDir); statErr != nil {
		t.Fatalf("external engine directory outside workspace must not be deleted, got statErr=%v", statErr)
	}
}

func TestServePlanToStartupSetsDefaultModelForMLXLM(t *testing.T) {
	startup := servePlanToStartup(inferyard.ServePlan{
		Argv:             []string{"/ws/engines/mlx-lm/venv/bin/python3", "-m", "mlx_lm.server", "--model", "/models/Qwen3-0.6B-4bit", "--host", "127.0.0.1", "--port", "48857"},
		WorkingDirectory: "/ws/engines/mlx-lm",
		Environment:      map[string]string{},
		Endpoint:         "http://127.0.0.1:48857",
		ModelPath:        "/models/Qwen3-0.6B-4bit",
		ModelKind:        "directory",
		ContextSize:      8192,
		Family:           "mlx-lm",
		Adapter:          "local_engine_v1",
		Local:            true,
	})
	if startup.ServedModel != "default_model" {
		t.Fatalf("ServedModel = %q, want default_model", startup.ServedModel)
	}
}

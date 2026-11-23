package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func TestSettingsDownloadProxyValidationAndForwarding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	results := filepath.Join(root, "results")
	models := filepath.Join(root, "models")
	for _, dir := range []string{workspace, results, models} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(UserSettings{
		InferyardPath: ptr(os.Args[0]),
		WorkspaceRoot: &workspace,
		ResultsRoot:   &results,
		ModelsRoot:    &models,
	})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var recorded [][]string
	svc, apiErr := New(context.Background(), Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			mu.Lock()
			recorded = append(recorded, append([]string(nil), args...))
			mu.Unlock()
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			if len(args) >= 2 && args[0] == "model" && args[1] == "resolve" {
				return inferyard.Execution{Stdout: []byte(`{"command":"model resolve","status":"resolved","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"model-resolve.v1","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"23749fefcc72300e3a2ad315e1317431b06b590a","path":"Qwen3-0.6B-Q8_0.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/23749fefcc72300e3a2ad315e1317431b06b590a/Qwen3-0.6B-Q8_0.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/23749fefcc72300e3a2ad315e1317431b06b590a/Qwen3-0.6B-Q8_0.gguf","filename":"Qwen3-0.6B-Q8_0.gguf","bytes":639446688,"already_pinned":false,"model_requests_sent":0}}`)}, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`)}, nil
			}
			return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok","completeness":"complete","limitations":[],"details":{}}`)}, nil
		},
		ExecStream: func(_ context.Context, _ func(string), _ string, args ...string) (inferyard.Execution, error) {
			mu.Lock()
			recorded = append(recorded, append([]string(nil), args...))
			mu.Unlock()
			return inferyard.Execution{Stdout: []byte(`{"command":"model acquire","status":"prepared","completeness":"complete","limitations":[],"details":{"model":"/tmp/m.gguf","bytes":100,"sha256":"0000000000000000000000000000000000000000000000000000000000000000"}}`)}, nil
		},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	for _, bad := range []string{`"/relative"`, `"ftp://127.0.0.1:7890"`, `"http://user:pass@127.0.0.1:7890"`, `"http://127.0.0.1:7890/path"`} {
		if _, err := svc.UpdateSettings(map[string]json.RawMessage{"download_proxy": json.RawMessage(bad)}); err == nil || err.Code != "invalid_request" {
			t.Fatalf("expected invalid_request for %s, got %+v", bad, err)
		}
	}

	resp, errSave := svc.UpdateSettings(map[string]json.RawMessage{"download_proxy": json.RawMessage(`"http://127.0.0.1:7890/"`)})
	if errSave != nil || str(resp.Effective.DownloadProxy) != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy save: resp=%+v err=%+v", resp, errSave)
	}

	if _, errRes := svc.ResolveModelURL(context.Background(), "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf", ""); errRes != nil {
		t.Fatalf("ResolveModelURL failed: %+v", errRes)
	}

	task, errDL := svc.CreateModelDownload(CreateDownloadRequest{
		SourceUrl: "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/23749fefcc72300e3a2ad315e1317431b06b590a/Qwen3-0.6B-Q8_0.gguf",
	}, "")
	if errDL != nil {
		t.Fatalf("CreateModelDownload failed: %+v", errDL)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		sawResolveProxy, sawAcquireProxy := false, false
		for _, call := range recorded {
			if len(call) >= 2 && call[0] == "model" && call[1] == "resolve" && slices.Contains(call, "--proxy-origin") && slices.Contains(call, "http://127.0.0.1:7890") {
				sawResolveProxy = true
			}
			if len(call) >= 2 && call[0] == "model" && call[1] == "acquire" && slices.Contains(call, "--proxy-origin") && slices.Contains(call, "http://127.0.0.1:7890") {
				sawAcquireProxy = true
			}
		}
		mu.Unlock()
		if sawResolveProxy && sawAcquireProxy {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	mu.Lock()
	sawResolveProxy, sawAcquireProxy := false, false
	for _, call := range recorded {
		if len(call) >= 2 && call[0] == "model" && call[1] == "resolve" && slices.Contains(call, "--proxy-origin") {
			sawResolveProxy = true
		}
		if len(call) >= 2 && call[0] == "model" && call[1] == "acquire" && slices.Contains(call, "--proxy-origin") {
			sawAcquireProxy = true
		}
	}
	mu.Unlock()
	if !sawResolveProxy || !sawAcquireProxy {
		t.Fatalf("expected --proxy-origin for task %s, recorded=%v", task.TaskId, recorded)
	}
}

func TestExtraModelsRootsScanAndDeviceCheckMultiRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	results := filepath.Join(root, "results")
	modelsA := filepath.Join(root, "models-a")
	modelsB := filepath.Join(root, "models-b")
	for _, dir := range []string{workspace, results, modelsA, modelsB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, "settings.json")
	raw, _ := json.Marshal(UserSettings{
		InferyardPath: ptr(os.Args[0]),
		WorkspaceRoot: &workspace,
		ResultsRoot:   &results,
		ModelsRoot:    &modelsA,
	})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var recorded [][]string
	svc, apiErr := New(context.Background(), Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			mu.Lock()
			recorded = append(recorded, append([]string(nil), args...))
			mu.Unlock()
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
				return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`)}, nil
			}
			if len(args) > 0 && args[0] == "device-check" {
				return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	resp, errAdd := svc.AddExtraModelsRoot(modelsB)
	if errAdd != nil {
		t.Fatalf("AddExtraModelsRoot failed: %+v", errAdd)
	}
	if !slices.Equal(resp.Effective.ExtraModelsRoots, []string{modelsB}) {
		t.Fatalf("unexpected ExtraModelsRoots: %+v", resp.Effective.ExtraModelsRoots)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		sawScan, sawDevice := false, false
		for _, call := range recorded {
			if len(call) >= 6 && call[0] == "assets" && call[1] == "scan" && slices.Contains(call, modelsA) && slices.Contains(call, modelsB) {
				sawScan = true
			}
			if len(call) >= 6 && call[0] == "device-check" && slices.Contains(call, modelsA) && slices.Contains(call, modelsB) {
				sawDevice = true
			}
		}
		mu.Unlock()
		if sawScan && sawDevice {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	mu.Lock()
	sawScan, sawDevice := false, false
	for _, call := range recorded {
		if len(call) >= 6 && call[0] == "assets" && call[1] == "scan" && slices.Contains(call, modelsA) && slices.Contains(call, modelsB) {
			sawScan = true
		}
		if len(call) >= 6 && call[0] == "device-check" && slices.Contains(call, modelsA) && slices.Contains(call, modelsB) {
			sawDevice = true
		}
	}
	mu.Unlock()
	if !sawScan || !sawDevice {
		t.Fatalf("expected multi-root scan & device-check, recorded=%v", recorded)
	}

	respRm, errRm := svc.RemoveExtraModelsRoot(modelsB)
	if errRm != nil || len(respRm.Effective.ExtraModelsRoots) != 0 {
		t.Fatalf("RemoveExtraModelsRoot failed: resp=%+v err=%+v", respRm, errRm)
	}
}

func TestSettingsThemeValidationPersistenceAndLegacyFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, "settings.json")
	legacyRaw := []byte(`{"workspace_root":` + string(mustJSON(workspace)) + `}`)
	if err := os.WriteFile(settingsPath, legacyRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	newSvc := func() *Service {
		svc, apiErr := New(context.Background(), Options{
			SettingsPath:  settingsPath,
			HostStatePath: filepath.Join(root, "host.json"),
			Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				if len(args) > 0 && args[0] == "--version" {
					return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
				}
				return inferyard.Execution{Stdout: []byte(`{"command":"probe","status":"ok","completeness":"complete","limitations":[],"details":{}}`)}, nil
			},
		})
		if apiErr != nil {
			t.Fatal(apiErr)
		}
		t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
		return svc
	}

	svc := newSvc()
	if got := svc.Theme(); got != ThemeSystem {
		t.Fatalf("legacy Theme() = %q, want %q", got, ThemeSystem)
	}
	if got := svc.Settings().Effective.Theme; got != ThemeSystem {
		t.Fatalf("legacy Effective.Theme = %q, want %q", got, ThemeSystem)
	}

	for _, bad := range []string{`"auto"`, `"DARK"`, `"blue"`, `123`, `true`, `{}`} {
		if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"theme": json.RawMessage(bad)}); apiErr == nil || apiErr.Code != "invalid_request" {
			t.Fatalf("expected invalid_request for %s, got %+v", bad, apiErr)
		}
	}
	if got := svc.Theme(); got != ThemeSystem {
		t.Fatalf("invalid updates mutated theme to %q", got)
	}

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: `"light"`, want: ThemeLight},
		{raw: `"dark"`, want: ThemeDark},
		{raw: `""`, want: ThemeSystem},
		{raw: `"dark"`, want: ThemeDark},
		{raw: `null`, want: ThemeSystem},
		{raw: `"system"`, want: ThemeSystem},
		{raw: `"dark"`, want: ThemeDark},
	} {
		resp, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"theme": json.RawMessage(tc.raw)})
		if apiErr != nil {
			t.Fatalf("UpdateSettings(%s) failed: %+v", tc.raw, apiErr)
		}
		if resp.Effective.Theme != tc.want || resp.Settings.Theme != tc.want || svc.Theme() != tc.want {
			t.Fatalf("UpdateSettings(%s): got settings=%q effective=%q svc=%q, want %q",
				tc.raw, resp.Settings.Theme, resp.Effective.Theme, svc.Theme(), tc.want)
		}
	}

	reloaded := newSvc()
	if reloaded.Theme() != ThemeDark || reloaded.Settings().Effective.Theme != ThemeDark || reloaded.Settings().Settings.Theme != ThemeDark {
		t.Fatalf("reloaded theme mismatch: svc=%q resp=%+v", reloaded.Theme(), reloaded.Settings())
	}
}

func TestSettingsThemeAllowedWhenPathsBoundAndPreservesCaches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cliPath, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	results := filepath.Join(root, "results")
	models := filepath.Join(root, "models")
	engines := filepath.Join(root, "engines")
	for _, dir := range []string{workspace, results, models, engines} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, "settings.json")

	var mu sync.Mutex
	var recorded [][]string
	execFn := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		mu.Lock()
		recorded = append(recorded, append([]string(nil), args...))
		mu.Unlock()
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
		}
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			return inferyard.Execution{Stdout: []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`)}, nil
		}
		if len(args) > 0 && args[0] == "device-check" {
			return inferyard.Execution{Stdout: []byte(`{"command":"device-check","status":"inspected","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{},"models":[],"recommendation":{},"platform_capabilities":{}}}`)}, nil
		}
		return inferyard.Execution{}, nil
	}

	paths := &BoundPaths{
		CLI: cliPath, Workspace: workspace, Results: results, Models: models, Engines: engines, Settings: settingsPath,
		Source: "dev", WindowTitle: "Dev", SourceLabel: "dev",
	}
	svc, apiErr := New(context.Background(), Options{
		Paths:         paths,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute:       execFn,
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	// Wait briefly for initial New() scans/device checks, then seed cached snapshot and engine state.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if svc.Device().CapturedAt != nil {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	svc.mu.Lock()
	svc.device.Snapshot = json.RawMessage(`{"cached":true}`)
	devGenBefore := svc.deviceGeneration
	assetsGenBefore := svc.assetsGen
	svc.engine = &engineEntry{state: EngineState{State: "running"}}
	svc.mu.Unlock()

	mu.Lock()
	callsBefore := len(recorded)
	mu.Unlock()

	if _, errBound := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": json.RawMessage(`"/tmp/other"`)}); errBound == nil || errBound.Code != "conflict" {
		t.Fatalf("expected paths_bound conflict for workspace_root, got %+v", errBound)
	}

	resp, errTheme := svc.UpdateSettings(map[string]json.RawMessage{"theme": json.RawMessage(`"dark"`)})
	if errTheme != nil {
		t.Fatalf("UpdateSettings(theme) under BoundPaths failed: %+v", errTheme)
	}
	if resp.Effective.Theme != ThemeDark || svc.Theme() != ThemeDark {
		t.Fatalf("bound theme update mismatch: resp=%+v svc=%q", resp, svc.Theme())
	}

	svc.mu.Lock()
	devGenAfter := svc.deviceGeneration
	assetsGenAfter := svc.assetsGen
	snapAfter := string(svc.device.Snapshot)
	svc.engine = nil
	svc.mu.Unlock()

	if devGenAfter != devGenBefore || assetsGenAfter != assetsGenBefore || snapAfter != `{"cached":true}` {
		t.Fatalf("theme update invalidated caches: devGen %d->%d assetsGen %d->%d snap=%s",
			devGenBefore, devGenAfter, assetsGenBefore, assetsGenAfter, snapAfter)
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	extraCalls := recorded[callsBefore:]
	mu.Unlock()
	for _, call := range extraCalls {
		if len(call) >= 1 && (call[0] == "device-check" || (len(call) >= 2 && call[0] == "assets" && call[1] == "scan")) {
			t.Fatalf("theme update unexpectedly triggered scan/device-check: %v", extraCalls)
		}
	}

	svcReloaded, reloadErr := New(context.Background(), Options{
		Paths:         paths,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute:       execFn,
	})
	if reloadErr != nil {
		t.Fatal(reloadErr)
	}
	t.Cleanup(func() { _ = svcReloaded.Shutdown(context.Background()) })
	if svcReloaded.Theme() != ThemeDark {
		t.Fatalf("bound paths reload lost theme: got %q, want %q", svcReloaded.Theme(), ThemeDark)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

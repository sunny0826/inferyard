package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDarwinVLLMDualWheelUpdateAndLlamaKleidiaiFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/ggml-org/llama.cpp/releases/latest":
			// v1.1.0 has no binary archive -> must fall back to /releases?per_page=5
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.1.0",
				"html_url": "https://github.com/ggml-org/llama.cpp/releases/tag/v1.1.0",
				"assets": []map[string]any{
					{"name": "nightly-tag.txt", "browser_download_url": "https://github.com/ggml-org/llama.cpp/releases/download/v1.1.0/nightly-tag.txt", "size": 12},
				},
			})
		case "/repos/ggml-org/llama.cpp/releases":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"tag_name": "b12810",
					"html_url": "https://github.com/ggml-org/llama.cpp/releases/tag/b12810",
					"assets": []map[string]any{
						{"name": "llama-b12810-bin-macos-arm64-kleidiai.tar.gz", "browser_download_url": "https://github.com/ggml-org/llama.cpp/releases/download/b12810/llama-b12810-bin-macos-arm64-kleidiai.tar.gz", "size": 99},
						{"name": "llama-b12810-bin-macos-arm64.tar.gz", "browser_download_url": "https://github.com/ggml-org/llama.cpp/releases/download/b12810/llama-b12810-bin-macos-arm64.tar.gz", "size": 100},
					},
				},
			})
		case "/repos/vllm-project/vllm/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.34.0",
				"html_url": "https://github.com/vllm-project/vllm/releases/tag/v0.34.0",
				"assets": []map[string]any{
					{"name": "vllm-0.34.0+cpu-cp38-abi3-macosx_11_0_arm64.whl", "browser_download_url": "https://github.com/vllm-project/vllm/releases/download/v0.34.0/vllm-0.34.0%2Bcpu-cp38-abi3-macosx_11_0_arm64.whl", "size": 29000000},
				},
			})
		case "/repos/vllm-project/vllm-metal/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.34.0",
				"html_url": "https://github.com/vllm-project/vllm-metal/releases/tag/v0.34.0",
				"assets": []map[string]any{
					{"name": "vllm_metal-0.34.0-cp312-cp312-macosx_15_0_arm64.whl", "browser_download_url": "https://github.com/vllm-project/vllm-metal/releases/download/v0.34.0/vllm_metal-0.34.0-cp312-cp312-macosx_15_0_arm64.whl", "size": 4900000},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	venvDir := filepath.Join(dir, "vllm-venv")
	binDir := filepath.Join(venvDir, "bin")
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(venvDir, "Scripts")
	}
	_ = os.MkdirAll(binDir, 0o755)
	pyName := "python3"
	if runtime.GOOS == "windows" {
		pyName = "python.exe"
	}
	pyPath := filepath.Join(binDir, pyName)
	_ = os.WriteFile(pyPath, []byte("#!/bin/sh\n"), 0o755)
	siteDir := filepath.Join(venvDir, "lib", "python3.12", "site-packages", "vllm-0.31.0+cpu.dist-info")
	_ = os.MkdirAll(siteDir, 0o755)
	_ = os.WriteFile(filepath.Join(siteDir, "METADATA"), []byte("Name: vllm\nVersion: 0.31.0+cpu\n"), 0o644)
	vllmInst := filepath.Join(venvDir, "installation.json")
	rawVllm, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "vllm",
		"executable":        pyPath,
		"working_directory": venvDir,
		"runtime_roots":     []string{venvDir},
		"environment":       map[string]string{},
	})
	_ = os.WriteFile(vllmInst, rawVllm, 0o644)

	wsDir := filepath.Join(dir, "ws")
	llamaDir := filepath.Join(wsDir, "engines", "llama-b11146")
	_ = os.MkdirAll(llamaDir, 0o755)
	llamaBin := filepath.Join(llamaDir, "llama-server")
	if runtime.GOOS == "windows" {
		llamaBin = filepath.Join(llamaDir, "llama-server.exe")
	}
	_ = os.WriteFile(llamaBin, []byte("#!/bin/sh\necho 'version: 11146'\n"), 0o755)
	_ = os.WriteFile(filepath.Join(llamaDir, "engine-prepare.json"), []byte(`{"source_url":"https://github.com/ggml-org/llama.cpp/releases/download/b11146/llama-b11146-bin-macos-arm64.tar.gz"}`), 0o644)

	var gotUpgradeArgs []string
	upgradeDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{
		SettingsPath: filepath.Join(dir, "settings.json"),
		EngineUpdate: EngineUpdateOptions{
			HTTPClient:        srv.Client(),
			GitHubAPIBase:     srv.URL,
			CachePath:         filepath.Join(dir, "cache.json"),
			Platform:          "darwin/arm64",
			AllowInsecureHTTP: true,
			ExecUpgrade: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				gotUpgradeArgs = append([]string(nil), args...)
				_ = os.WriteFile(filepath.Join(siteDir, "METADATA"), []byte("Name: vllm\nVersion: 0.34.0+cpu\n"), 0o644)
				close(upgradeDone)
				return inferyard.Execution{ExitCode: 0}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	svc.settingsMu.Lock()
	svc.settings.WorkspaceRoot = ptr(wsDir)
	svc.settings.EngineInstallations = []string{vllmInst}
	svc.settingsMu.Unlock()

	status, apiErr := svc.CheckEngineUpdates(ctx)
	if apiErr != nil {
		t.Fatalf("CheckEngineUpdates failed: %+v", apiErr)
	}

	var vllmEngineName string
	for name, info := range status.Updates {
		switch info.Family {
		case "llama-cpp":
			if !info.HasUpdate || info.LatestVersion != "b12810" {
				t.Fatalf("expected llama-cpp fallback to b12810 archive release, got %+v", info)
			}
			if strings.Contains(info.AssetName, "kleidiai") || info.AssetName != "llama-b12810-bin-macos-arm64.tar.gz" {
				t.Fatalf("expected standard macos-arm64 archive (not kleidiai), got %+v", info)
			}
		case "vllm":
			vllmEngineName = name
			if !info.HasUpdate || info.LatestVersion != "v0.34.0" || !info.CanOneClickUpdate() || len(info.WheelURLs) != 2 {
				t.Fatalf("expected darwin/arm64 vllm dual-wheel one-click update to v0.34.0, got %+v", info)
			}
		}
	}
	if vllmEngineName == "" {
		t.Fatalf("missing vllm entry in updates: %+v", status.Updates)
	}

	if _, apiErr := svc.UpdateEngine(vllmEngineName); apiErr != nil {
		t.Fatalf("UpdateEngine(vllm) failed: %+v", apiErr)
	}
	select {
	case <-upgradeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for vllm dual-wheel upgrade")
	}
	if !slices.ContainsFunc(gotUpgradeArgs, func(a string) bool { return strings.Contains(a, "vllm-0.34.0") }) ||
		!slices.ContainsFunc(gotUpgradeArgs, func(a string) bool { return strings.Contains(a, "vllm_metal-0.34.0") }) {
		t.Fatalf("expected both vllm and vllm-metal wheels in upgrade args, got %v", gotUpgradeArgs)
	}
}

func TestPiplessVenvUpgradeDownloadsManagedUVWhenMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub test runs on unix")
	}
	dir := t.TempDir()
	fakeUV := filepath.Join(dir, "tools", "uv")
	_ = os.MkdirAll(filepath.Dir(fakeUV), 0o755)
	marker := filepath.Join(dir, "uv-ran.txt")
	_ = os.WriteFile(fakeUV, []byte("#!/bin/sh\necho \"$@\" > "+marker+"\nexit 0\n"), 0o755)
	fakePy := filepath.Join(dir, "venv", "bin", "python3")
	_ = os.MkdirAll(filepath.Dir(fakePy), 0o755)
	// Simulate uv-created venv without pip (returns non-zero on -m pip --version).
	_ = os.WriteFile(fakePy, []byte("#!/bin/sh\nexit 1\n"), 0o755)

	origLook := installLookPath
	origStat := installStat
	origDL := managedEngineDownloadUV
	t.Cleanup(func() {
		installLookPath = origLook
		installStat = origStat
		managedEngineDownloadUV = origDL
	})
	installLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	installStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	dlCalled := false
	managedEngineDownloadUV = func(_ context.Context, _ string) (string, *ApiError) {
		dlCalled = true
		return fakeUV, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(dir, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	out, execErr := svc.executeVenvUpgrade(ctx, fakePy, "mlx-lm==0.32.2")
	if execErr != nil || out.ExitCode != 0 || !dlCalled {
		t.Fatalf("expected managed uv download and exit 0, got err=%v code=%d dlCalled=%v", execErr, out.ExitCode, dlCalled)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(raw), "mlx-lm==0.32.2") {
		t.Fatalf("expected fake uv to execute upgrade, got %q (%v)", string(raw), err)
	}
	if got := parseLlamaServerVersionOutput("version: 0.7.0-dev (build 12232, commit f70247610)\nbuilt with AppleClang"); got != "b12232" {
		t.Fatalf("parseLlamaServerVersionOutput(0.7.0-dev build 12232) = %q, want b12232", got)
	}
}

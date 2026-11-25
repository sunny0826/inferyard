package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseLlamaServerVersionOutput(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"version: 5421 (a1b2c3d)\nbuilt with Apple clang", "b5421"},
		{"llama-server prism-b10743-adfffbe41", "prism-b10743-adfffbe41"},
		{"llama-server v1.2.0", "v1.2.0"},
		{"unknown output", ""},
	}
	for _, tc := range cases {
		if got := parseLlamaServerVersionOutput(tc.raw); got != tc.want {
			t.Fatalf("parseLlamaServerVersionOutput(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestReadVenvPackageVersion(t *testing.T) {
	venvDir := t.TempDir()
	binDir := filepath.Join(venvDir, "bin")
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(venvDir, "Scripts")
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pyName := "python"
	if runtime.GOOS == "windows" {
		pyName = "python.exe"
	}
	pyPath := filepath.Join(binDir, pyName)
	if err := os.WriteFile(pyPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	siteDir := filepath.Join(venvDir, "lib", "python3.12", "site-packages", "mlx_lm-0.31.5.dist-info")
	if runtime.GOOS == "windows" {
		siteDir = filepath.Join(venvDir, "Lib", "site-packages", "mlx_lm-0.31.5.dist-info")
	}
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "METADATA"), []byte("Metadata-Version: 2.1\nName: mlx-lm\nVersion: 0.31.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	instPath := filepath.Join(venvDir, "installation.json")
	decl, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "mlx-lm",
		"executable":        pyPath,
		"working_directory": venvDir,
		"runtime_roots":     []string{venvDir},
		"environment":       map[string]string{},
	})
	if err := os.WriteFile(instPath, decl, 0o644); err != nil {
		t.Fatal(err)
	}

	entry := registryEntry{path: instPath, installation: instPath, family: "mlx-lm"}
	if got := readVenvPackageVersion(entry, "mlx-lm"); got != "0.31.5" {
		t.Fatalf("readVenvPackageVersion = %q, want 0.31.5", got)
	}
	if got := venvPythonPath(entry); filepath.Clean(got) != filepath.Clean(pyPath) {
		t.Fatalf("venvPythonPath = %q, want %q", got, pyPath)
	}
}

func TestMatchLlamaReleaseAsset(t *testing.T) {
	assets := []remoteReleaseAsset{
		{Name: "llama-b8500-bin-macos-arm64.tar.gz", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-macos-arm64.tar.gz", Bytes: 100},
		{Name: "llama-b8500-bin-macos-arm64.zip", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-macos-arm64.zip", Bytes: 105},
		{Name: "cudart-llama-bin-win-cuda-12.4-x64.zip", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/cudart.zip", Bytes: 50},
		{Name: "llama-b8500-bin-win-cpu-x64.zip", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-win-cpu-x64.zip", Bytes: 120},
		{Name: "llama-b8500-bin-win-cuda-12.4-x64.zip", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-win-cuda-12.4-x64.zip", Bytes: 240},
		{Name: "llama-b8500-bin-ubuntu-x64.tar.gz", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b8500/llama-b8500-bin-ubuntu-x64.tar.gz", Bytes: 110},
	}

	mac, ok := matchLlamaReleaseAsset(assets, "darwin/arm64", false)
	if !ok || mac.Name != "llama-b8500-bin-macos-arm64.tar.gz" {
		t.Fatalf("darwin/arm64 asset = (%+v, %v)", mac, ok)
	}
	winCPU, ok := matchLlamaReleaseAsset(assets, "windows/amd64", false)
	if !ok || winCPU.Name != "llama-b8500-bin-win-cpu-x64.zip" {
		t.Fatalf("windows/amd64 CPU asset = (%+v, %v)", winCPU, ok)
	}
	winCUDA, ok := matchLlamaReleaseAsset(assets, "windows/amd64", true)
	if !ok || winCUDA.Name != "llama-b8500-bin-win-cuda-12.4-x64.zip" {
		t.Fatalf("windows/amd64 CUDA asset = (%+v, %v)", winCUDA, ok)
	}
	lin, ok := matchLlamaReleaseAsset(assets, "linux/amd64", false)
	if !ok || lin.Name != "llama-b8500-bin-ubuntu-x64.tar.gz" {
		t.Fatalf("linux/amd64 asset = (%+v, %v)", lin, ok)
	}
}

func TestCheckEngineUpdatesRemoteCacheAndHomebrew(t *testing.T) {
	var ghCalls, pypiCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/ggml-org/llama.cpp/releases/latest":
			ghCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "b12500",
				"html_url": "https://github.com/ggml-org/llama.cpp/releases/tag/b12500",
				"assets": []map[string]any{
					{
						"name":                 "llama-b12500-bin-macos-arm64.tar.gz",
						"browser_download_url": "https://github.com/ggml-org/llama.cpp/releases/download/b12500/llama-b12500-bin-macos-arm64.tar.gz",
						"size":                 12345678,
					},
				},
			})
		case "/repos/simplexai-labs/PolyStrata/releases/latest":
			ghCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.2.21-poly.1",
				"html_url": "https://github.com/simplexai-labs/PolyStrata/releases/tag/v0.2.21-poly.1",
			})
		case "/pypi/mlx-lm/json":
			pypiCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info": map[string]any{"version": "0.35.0"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	// Create mlx-lm venv installation fixture.
	venvDir := filepath.Join(dir, "mlx-venv")
	binDir := filepath.Join(venvDir, "bin")
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(venvDir, "Scripts")
	}
	_ = os.MkdirAll(binDir, 0o755)
	pyName := "python"
	if runtime.GOOS == "windows" {
		pyName = "python.exe"
	}
	pyPath := filepath.Join(binDir, pyName)
	_ = os.WriteFile(pyPath, []byte("#!/bin/sh\n"), 0o755)
	siteDir := filepath.Join(venvDir, "lib", "python3.12", "site-packages", "mlx_lm-0.32.2.dist-info")
	if runtime.GOOS == "windows" {
		siteDir = filepath.Join(venvDir, "Lib", "site-packages", "mlx_lm-0.32.2.dist-info")
	}
	_ = os.MkdirAll(siteDir, 0o755)
	_ = os.WriteFile(filepath.Join(siteDir, "METADATA"), []byte("Name: mlx-lm\nVersion: 0.32.2\n"), 0o644)
	mlxInst := filepath.Join(venvDir, "installation.json")
	rawMlx, _ := json.Marshal(map[string]any{
		"schema_version":    3,
		"definition":        "local-engine-installation.v1",
		"family":            "mlx-lm",
		"executable":        pyPath,
		"working_directory": venvDir,
		"runtime_roots":     []string{venvDir},
		"environment":       map[string]string{},
	})
	_ = os.WriteFile(mlxInst, rawMlx, 0o644)

	// PolyStrata fixture.
	polyDir := filepath.Join(dir, "polystrata-old")
	polyInst := setupPolystrataInstallFixture(t, polyDir, filepath.Join(dir, "poly-data"), "v0.1.45-poly.10", false)

	// Managed llama-cpp binary fixture.
	wsDir := filepath.Join(dir, "ws")
	llamaDir := filepath.Join(wsDir, "engines", "llama-b11146")
	_ = os.MkdirAll(llamaDir, 0o755)
	llamaBinName := "llama-server"
	if runtime.GOOS == "windows" {
		llamaBinName = "llama-server.exe"
	}
	llamaBin := filepath.Join(llamaDir, llamaBinName)
	_ = os.WriteFile(llamaBin, []byte("#!/bin/sh\necho 'version: 11146 (abc)'\n"), 0o755)
	_ = os.WriteFile(filepath.Join(llamaDir, "engine-prepare.json"), []byte(`{"source_url":"https://github.com/ggml-org/llama.cpp/releases/download/b11146/llama-b11146-bin-macos-arm64.tar.gz"}`), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cachePath := filepath.Join(dir, "cache.json")
	svc, err := New(ctx, Options{
		SettingsPath: filepath.Join(dir, "settings.json"),
		EngineUpdate: EngineUpdateOptions{
			HTTPClient:        srv.Client(),
			GitHubAPIBase:     srv.URL,
			PyPIAPIBase:       srv.URL,
			CachePath:         cachePath,
			CacheTTL:          6 * time.Hour,
			Platform:          "darwin/arm64",
			AllowInsecureHTTP: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	svc.settingsMu.Lock()
	svc.settings.WorkspaceRoot = ptr(wsDir)
	svc.settings.EngineInstallations = []string{mlxInst, polyInst}
	svc.settingsMu.Unlock()

	status, apiErr := svc.CheckEngineUpdates(ctx)
	if apiErr != nil {
		t.Fatalf("CheckEngineUpdates failed: %+v", apiErr)
	}
	if status.State != EngineUpdateCheckReady || status.UpdatableCount != 3 {
		t.Fatalf("expected 3 updatable engines in ready state, got state=%q count=%d updates=%+v", status.State, status.UpdatableCount, status.Updates)
	}

	var sawLlama, sawPoly, sawMlx bool
	for _, info := range status.Updates {
		switch info.Family {
		case "llama-cpp":
			sawLlama = true
			if !info.HasUpdate || info.LatestVersion != "b12500" || info.Action != EngineUpdateActionDownload || info.Source != "remote" {
				t.Fatalf("unexpected llama-cpp update: %+v", info)
			}
		case "polystrata":
			sawPoly = true
			if !info.HasUpdate || info.LatestVersion != "v0.2.21-poly.1" || info.Action != EngineUpdateActionManaged {
				t.Fatalf("unexpected polystrata update: %+v", info)
			}
			if !strings.HasSuffix(info.DownloadURL, "/archive/refs/tags/v0.2.21-poly.1.tar.gz") {
				t.Fatalf("unexpected polystrata download URL: %q", info.DownloadURL)
			}
		case "mlx-lm":
			sawMlx = true
			if !info.HasUpdate || info.InstalledVersion != "0.32.2" || info.LatestVersion != "0.35.0" || info.Action != EngineUpdateActionVenv {
				t.Fatalf("unexpected mlx-lm update: %+v", info)
			}
		}
	}
	if !sawLlama || !sawPoly || !sawMlx {
		t.Fatalf("missing expected engines: llama=%v poly=%v mlx=%v", sawLlama, sawPoly, sawMlx)
	}

	// Background check with fresh disk cache should not make additional HTTP calls.
	beforeGH, beforePyPI := ghCalls.Load(), pypiCalls.Load()
	if _, err := svc.checkEngineUpdatesInternal(ctx, false); err != nil {
		t.Fatalf("background cached check failed: %+v", err)
	}
	if ghCalls.Load() != beforeGH || pypiCalls.Load() != beforePyPI {
		t.Fatalf("expected cached background check to skip HTTP requests, got gh=%d->%d pypi=%d->%d",
			beforeGH, ghCalls.Load(), beforePyPI, pypiCalls.Load())
	}

	// Homebrew llama-server should resolve to drawer action with brew upgrade hint.
	hbInfo := EngineUpdateInfo{
		Engine:        "brew-llama",
		Family:        "llama-cpp",
		LatestVersion: "b12500",
		DownloadURL:   "https://github.com/ggml-org/llama.cpp/releases/download/b12500/llama-b12500-bin-macos-arm64.tar.gz",
		HasUpdate:     true,
	}
	act, hint := resolveEngineUpdateAction(hbInfo, registryEntry{path: "/opt/homebrew/bin/llama-server"}, true, "darwin/arm64")
	if act != EngineUpdateActionDrawer || hint != "brew upgrade llama.cpp" {
		t.Fatalf("expected Homebrew llama-cpp to resolve to drawer with brew hint, got action=%q hint=%q", act, hint)
	}
}

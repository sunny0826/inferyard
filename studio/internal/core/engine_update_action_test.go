package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestUpdateEngineVenvInUseGuardAndExecution(t *testing.T) {
	dir := t.TempDir()
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
	siteDir := filepath.Join(venvDir, "lib", "python3.12", "site-packages", "mlx_lm-0.31.0.dist-info")
	if runtime.GOOS == "windows" {
		siteDir = filepath.Join(venvDir, "Lib", "site-packages", "mlx_lm-0.31.0.dist-info")
	}
	_ = os.MkdirAll(siteDir, 0o755)
	_ = os.WriteFile(filepath.Join(siteDir, "METADATA"), []byte("Name: mlx-lm\nVersion: 0.31.0\n"), 0o644)
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

	var gotSpec string
	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{
		SettingsPath: filepath.Join(dir, "settings.json"),
		EngineUpdate: EngineUpdateOptions{
			Platform: "darwin/arm64",
			ExecUpgrade: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
				gotSpec = args[len(args)-1]
				_ = os.WriteFile(filepath.Join(siteDir, "METADATA"), []byte("Name: mlx-lm\nVersion: 0.32.2\n"), 0o644)
				close(doneCh)
				return inferyard.Execution{ExitCode: 0}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	svc.settingsMu.Lock()
	svc.settings.EngineInstallations = []string{mlxInst}
	svc.settingsMu.Unlock()

	reg := svc.Registry()
	if len(reg.Engines) != 1 {
		t.Fatalf("expected 1 registered engine, got %d", len(reg.Engines))
	}
	engName := reg.Engines[0].Name

	// 1a) Guard: block in-place venv upgrade when legacy StartEngine runs with basename-only Executable & cmd.Path.
	svc.mu.Lock()
	svc.engine = &engineEntry{
		cmd:   &exec.Cmd{Path: pyPath},
		state: EngineState{State: "running", Executable: filepath.Base(pyPath)},
	}
	svc.mu.Unlock()
	if _, apiErr := svc.UpdateEngine(engName); apiErr == nil || apiErr.Code != "engine_in_use" {
		t.Fatalf("expected engine_in_use error when legacy candidate engine active via cmd.Path, got %+v", apiErr)
	}
	svc.mu.Lock()
	svc.engine = nil
	// 1b) Guard: block in-place venv upgrade while a standalone service uses this engine.
	svc.services["svc-mlx"] = &engineEntry{
		cmd:   &exec.Cmd{Path: pyPath},
		state: EngineState{State: "running", EngineName: engName, Executable: filepath.Base(pyPath)},
	}
	svc.mu.Unlock()
	if _, apiErr := svc.UpdateEngine(engName); apiErr == nil || apiErr.Code != "engine_in_use" {
		t.Fatalf("expected engine_in_use error when service active, got %+v", apiErr)
	}

	// 2) Clear active service -> upgrade executes cleanly.
	svc.mu.Lock()
	delete(svc.services, "svc-mlx")
	svc.mu.Unlock()

	task, apiErr := svc.UpdateEngine(engName)
	if apiErr != nil {
		t.Fatalf("UpdateEngine failed: %+v", apiErr)
	}
	if task.Kind != "engine_update" || task.State != "downloading" {
		t.Fatalf("unexpected task: %+v", task)
	}

	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for venv upgrade execution")
	}
	if gotSpec != "mlx-lm==0.32.2" {
		t.Fatalf("expected pip spec mlx-lm==0.32.2, got %q", gotSpec)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tasks := svc.Downloads()
		if len(tasks) > 0 && tasks[0].State == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if updates := svc.EngineUpdates(); updates[engName].HasUpdate {
		t.Fatalf("expected mlx-lm to be up to date after upgrade, got %+v", updates[engName])
	}
}

func TestCountUpdatableEngineTargetsSideBySideDedupe(t *testing.T) {
	updates := map[string]EngineUpdateInfo{
		"llama-old": {
			Engine:           "llama-old",
			Family:           "llama-cpp",
			Distribution:     "ggml-org",
			InstalledVersion: "b4800",
			LatestVersion:    "b8500",
			HasUpdate:        true,
		},
		"llama-new": {
			Engine:           "llama-new",
			Family:           "llama-cpp",
			Distribution:     "ggml-org",
			InstalledVersion: "b8500",
			LatestVersion:    "b8500",
			HasUpdate:        false,
		},
	}
	if got := countUpdatableEngineTargets(updates); got != 0 {
		t.Fatalf("expected side-by-side latest copy to suppress group updatable count, got %d", got)
	}
	markSideBySideLatestInstalled(updates)
	if !updates["llama-old"].LatestInstalled || updates["llama-old"].CanOneClickUpdate() {
		t.Fatalf("expected older side-by-side entry to mark LatestInstalled and disable one-click update, got %+v", updates["llama-old"])
	}

	// Independent Python venvs must NOT lose their in-place update action when another venv is already up to date.
	venvUpdates := map[string]EngineUpdateInfo{
		"mlx-a": {
			Engine:           "mlx-a",
			Family:           "mlx-lm",
			Distribution:     "PyPI",
			InstalledVersion: "0.31.0",
			LatestVersion:    "0.32.2",
			Action:           EngineUpdateActionVenv,
			HasUpdate:        true,
		},
		"mlx-b": {
			Engine:           "mlx-b",
			Family:           "mlx-lm",
			Distribution:     "PyPI",
			InstalledVersion: "0.32.2",
			LatestVersion:    "0.32.2",
			HasUpdate:        false,
		},
	}
	markSideBySideLatestInstalled(venvUpdates)
	if venvUpdates["mlx-a"].LatestInstalled || !venvUpdates["mlx-a"].CanOneClickUpdate() {
		t.Fatalf("expected independent outdated venv mlx-a to keep in-place update action, got %+v", venvUpdates["mlx-a"])
	}
}

func TestEngineUpdateReviewFixesRegression(t *testing.T) {
	// 1) Python .postN version comparison.
	if cmp, ok := compareEngineVersions("0.4.6", "0.4.6.post5"); !ok || cmp >= 0 {
		t.Fatalf("expected 0.4.6 < 0.4.6.post5, got (%d, %v)", cmp, ok)
	}
	if cmp, ok := compareEngineVersions("0.4.6.post1", "0.4.6.post5"); !ok || cmp >= 0 {
		t.Fatalf("expected 0.4.6.post1 < 0.4.6.post5, got (%d, %v)", cmp, ok)
	}
	if cmp, ok := compareEngineVersions("0.4.6.post5", "0.4.7"); !ok || cmp >= 0 {
		t.Fatalf("expected 0.4.6.post5 < 0.4.7, got (%d, %v)", cmp, ok)
	}

	// 2) Windows unprofiled remote llama.cpp falls back to drawer (no broken receipt-less one-click download).
	winInfo := EngineUpdateInfo{
		Engine:        "llama-win",
		Family:        "llama-cpp",
		LatestVersion: "b12500",
		DownloadURL:   "https://github.com/ggml-org/llama.cpp/releases/download/b12500/llama-b12500-bin-win-cpu-x64.zip",
		HasUpdate:     true,
	}
	if act, _ := resolveEngineUpdateAction(winInfo, registryEntry{path: `C:\engines\llama-server.exe`}, true, "windows/amd64"); act != EngineUpdateActionDrawer {
		t.Fatalf("expected unprofiled Windows llama.cpp to fall back to drawer, got %q", act)
	}

	// 3) Ineligible check's expected_release (prism-b10743-adfffbe) must not pollute ggml-org distribution/version.
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "ws")
	llamaDir := filepath.Join(wsDir, "engines", "llama-b8400")
	_ = os.MkdirAll(llamaDir, 0o755)
	llamaBinName := "llama-server"
	if runtime.GOOS == "windows" {
		llamaBinName = "llama-server.exe"
	}
	llamaBin := filepath.Join(llamaDir, llamaBinName)
	_ = os.WriteFile(llamaBin, []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(llamaDir, "engine-prepare.json"), []byte(`{"source_url":"https://github.com/ggml-org/llama.cpp/releases/download/b8400/llama-b8400-bin-macos-arm64.tar.gz"}`), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(dir, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())
	svc.settingsMu.Lock()
	svc.settings.WorkspaceRoot = ptr(wsDir)
	svc.settingsMu.Unlock()

	reg := svc.Registry()
	if len(reg.Engines) != 1 {
		t.Fatalf("expected 1 llama engine, got %d", len(reg.Engines))
	}
	engName := reg.Engines[0].Name
	svc.checkMu.Lock()
	svc.checkSupported = true
	svc.checkNames = []engineCheckTarget{{name: engName, path: llamaBin, key: "k"}}
	svc.checks = map[string]engineCheckEntry{
		llamaBin: {
			key:      "k",
			verified: true,
			result:   EngineEligibility{State: EngineCheckIneligible, Family: "llama-cpp", Reason: "engine_version_mismatch", Release: "prism-b10743-adfffbe"},
		},
	}
	svc.checkMu.Unlock()

	info := svc.EngineUpdates()[engName]
	if info.Distribution != "ggml-org" || info.InstalledVersion != "b8400" {
		t.Fatalf("expected ggml-org b8400 without Prism expected_release pollution, got %+v", info)
	}

	// 4) Bidirectional guard: active venv upgrade blocks run admission & service start for that engine.
	svc.mu.Lock()
	svc.downloads = append(svc.downloads, &downloadEntry{
		Task: DownloadTask{
			Kind:    "engine_update",
			State:   "downloading",
			OutName: "mlx-local",
			OutDir:  filepath.Join(dir, "mlx-venv"),
		},
	})
	if runErr := svc.admitRunLocked(); runErr == nil || runErr.Code != "engine_updating" {
		svc.mu.Unlock()
		t.Fatalf("expected admitRunLocked to block during venv upgrade, got %+v", runErr)
	}
	if !svc.isEngineUpgradingLocked("mlx-local", filepath.Join(dir, "mlx-venv", "bin", "python")) {
		svc.mu.Unlock()
		t.Fatal("expected isEngineUpgradingLocked to match active venv upgrade")
	}
	svc.mu.Unlock()
}

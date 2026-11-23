package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestOutdatedCLIPropagationInDeviceAssetsAndPrepLocations(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })
	Version = "0.0.9"

	stubInstall(t)
	cliPath := fakeCLIFile(t)
	installLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	installStat = os.Stat

	modelsDir := t.TempDir()
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
		}
		return inferyard.Execution{}, nil
	})

	patchCLI, _ := json.Marshal(cliPath)
	patchModels, _ := json.Marshal(modelsDir)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": patchCLI,
		"models_root":    patchModels,
	}); apiErr != nil {
		t.Fatal(apiErr)
	}

	wait(t, func() bool {
		return !s.Device().Refreshing && s.Device().LastError != nil && !s.Assets().AssetsRefreshing && s.Assets().AssetsError != nil
	})
	if got := s.Device().LastError; got == nil || got.Code != "cli_outdated" {
		t.Fatalf("Device.LastError = %+v, want cli_outdated", got)
	}
	if got := s.Assets().AssetsError; got == nil || got.Code != "cli_outdated" {
		t.Fatalf("Assets.AssetsError = %+v, want cli_outdated", got)
	}

	locs := s.PreparationLocations()
	if locs.CLIStatus != "outdated" || str(locs.CLI) != cliPath || str(locs.CLIVersion) != "0.0.3" {
		t.Fatalf("PreparationLocations = %+v", locs)
	}
}

func TestSwitchingCLIPathRefreshesDeviceAndAssets(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })
	Version = "0.0.9"

	oldCLI := fakeCLIFile(t)
	newCLI := fakeCLIFile(t)
	modelsDir := t.TempDir()

	s := testService(t, func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			if path == oldCLI {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
			}
			if path == newCLI {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
			}
		}
		if path == newCLI && len(args) > 0 && args[0] == "device-check" {
			return inferyard.Execution{
				ExitCode: 0,
				Stdout:   []byte(`{"command":"device-check","status":"complete","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{"platform":"Darwin"},"models":[],"recommendation":{},"platform_capabilities":{}}}`),
			}, nil
		}
		if path == newCLI && len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			return inferyard.Execution{
				ExitCode: 0,
				Stdout:   []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`),
			}, nil
		}
		return inferyard.Execution{ExitCode: 4}, nil
	})

	patchOld, _ := json.Marshal(oldCLI)
	patchModels, _ := json.Marshal(modelsDir)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": patchOld,
		"models_root":    patchModels,
	}); apiErr != nil {
		t.Fatal(apiErr)
	}
	wait(t, func() bool {
		return !s.Device().Refreshing && s.Device().LastError != nil && !s.Assets().AssetsRefreshing && s.Assets().AssetsError != nil
	})

	patchNew, _ := json.Marshal(newCLI)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": patchNew}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if s.Readiness().CLI.Status != "ok" {
		t.Fatalf("expected Readiness.CLI.Status=ok, got %+v", s.Readiness().CLI)
	}
	wait(t, func() bool {
		return !s.Device().Refreshing && s.Device().Snapshot != nil && !s.Assets().AssetsRefreshing && s.Assets().CapturedAt != nil
	})
	if s.Device().LastError != nil {
		t.Fatalf("Device.LastError after switching to new CLI = %+v", s.Device().LastError)
	}
	if s.Assets().AssetsError != nil {
		t.Fatalf("Assets.AssetsError after switching to new CLI = %+v", s.Assets().AssetsError)
	}
}

func TestInstallCLIDoesNotReuseOutdatedBuildMetadata(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })
	Version = "0.0.9"

	stubInstall(t)
	oldBuild := fakeCLIFile(t)
	binDir := t.TempDir()
	installed := filepath.Join(binDir, cliBinaryName())
	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return "/opt/uv/uv", nil
		}
		return "", errors.New("not found")
	}
	installStat = func(p string) (os.FileInfo, error) {
		if p == "/opt/uv/uv" {
			return fakeInfo{}, nil
		}
		return os.Stat(p)
	}
	installCommandOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
			if err := os.WriteFile(installed, []byte("good"), 0o755); err != nil {
				t.Fatal(err)
			}
			return []byte("Upgraded"), nil
		}
		return []byte(binDir + "\n"), nil
	}

	s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			if p == oldBuild {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3+local source=checkout\n")}, nil
			}
			if p == installed {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
			}
		}
		return inferyard.Execution{}, errors.New("unexpected")
	})
	s.settings.InferyardPath = ptr(oldBuild)
	got, apiErr := s.InstallCLI(context.Background(), nil)
	if apiErr != nil {
		t.Fatalf("InstallCLI: %+v", apiErr)
	}
	if got != installed {
		t.Fatalf("expected 0.0.3+local not to be reused; got %q want %q", got, installed)
	}
}

func TestInPlaceCLIUpgradeInvalidatesInFlightDeviceAndAssets(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })
	Version = "0.0.9"

	stubInstall(t)
	binDir := t.TempDir()
	cliPath := filepath.Join(binDir, cliBinaryName())
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	modelsDir := t.TempDir()

	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return "/opt/uv/uv", nil
		}
		return "", errors.New("not found")
	}
	installStat = func(p string) (os.FileInfo, error) {
		if p == "/opt/uv/uv" {
			return fakeInfo{}, nil
		}
		return os.Stat(p)
	}

	var upgraded atomic.Bool
	var blockOldProbe atomic.Bool
	var blockedCalls atomic.Int32
	oldEntered := make(chan struct{}, 2)
	gate := make(chan struct{})
	var oldDone sync.WaitGroup

	installCommandOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
			if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n# upgraded\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			upgraded.Store(true)
			return []byte("Upgraded"), nil
		}
		return []byte(binDir + "\n"), nil
	}

	s := installService(t, func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" && path == cliPath {
			if blockOldProbe.Load() && blockedCalls.Add(1) <= 2 {
				defer oldDone.Done()
				oldEntered <- struct{}{}
				<-gate
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
			}
			if upgraded.Load() {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
			}
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
		}
		if upgraded.Load() && len(args) > 0 && args[0] == "device-check" {
			return inferyard.Execution{
				ExitCode: 0,
				Stdout:   []byte(`{"command":"device-check","status":"complete","completeness":"complete","limitations":[],"details":{"kind":"device_preflight.v1","hardware":{"platform":"Darwin"},"models":[],"recommendation":{},"platform_capabilities":{}}}`),
			}, nil
		}
		if upgraded.Load() && len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			return inferyard.Execution{
				ExitCode: 0,
				Stdout:   []byte(`{"command":"assets scan","status":"scanned","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`),
			}, nil
		}
		return inferyard.Execution{ExitCode: 4}, nil
	})

	patchCLI, _ := json.Marshal(cliPath)
	patchModels, _ := json.Marshal(modelsDir)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": patchCLI,
		"models_root":    patchModels,
	}); apiErr != nil {
		t.Fatal(apiErr)
	}
	wait(t, func() bool {
		return !s.Device().Refreshing && s.Device().LastError != nil && !s.Assets().AssetsRefreshing && s.Assets().AssetsError != nil
	})

	oldDone.Add(2)
	blockOldProbe.Store(true)
	if apiErr := s.RefreshDevice(); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := s.RefreshAssets(); apiErr != nil {
		t.Fatal(apiErr)
	}
	<-oldEntered
	<-oldEntered
	blockOldProbe.Store(false)

	got, apiErr := s.InstallCLI(context.Background(), nil)
	if apiErr != nil {
		t.Fatalf("InstallCLI: %+v", apiErr)
	}
	if got != cliPath {
		t.Fatalf("InstallCLI = %q, want %q", got, cliPath)
	}

	close(gate)
	oldDone.Wait()

	wait(t, func() bool {
		return !s.Device().Refreshing && s.Device().Snapshot != nil && !s.Assets().AssetsRefreshing && s.Assets().CapturedAt != nil
	})
	if r := s.Readiness(); r.CLI.Status != "ok" || str(r.CLI.Version) != "0.0.9" {
		t.Fatalf("Readiness.CLI = %+v, want ok 0.0.9", r.CLI)
	}
	if s.Device().LastError != nil {
		t.Fatalf("Device.LastError after in-place upgrade = %+v", s.Device().LastError)
	}
	if s.Assets().AssetsError != nil {
		t.Fatalf("Assets.AssetsError after in-place upgrade = %+v", s.Assets().AssetsError)
	}
}

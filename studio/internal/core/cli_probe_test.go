package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func fakeCLIFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), cliBinaryName())
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProbeCLIVersionValidationAndNoOutdatedCache(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })

	cliPath := fakeCLIFile(t)
	reported := "0.0.3"
	calls := 0
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			calls++
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard " + reported + " source=release\n")}, nil
		}
		return inferyard.Execution{}, nil
	})
	s.settings.InferyardPath = ptr(cliPath)
	u := s.userSettings()

	Version = "0.0.9"
	for _, tc := range []struct {
		cliVer string
		want   string
	}{
		{"0.0.3", "outdated"},
		{"0.0.8", "outdated"},
		{"0.0.3+local", "outdated"},
		{"0.0.9-rc.1", "outdated"},
		{"0.0.9.dev0", "outdated"},
		{"0.0.9rc1", "outdated"},
		{"0.0.9", "ok"},
		{"0.0.9+local", "ok"},
		{"0.0.10", "ok"},
		{"0.0.10-rc.1", "ok"},
		{"bogus", "version_failed"},
	} {
		s.clearProbeCache()
		reported = tc.cliVer
		p := s.probe(u)
		if p.Status != tc.want {
			t.Fatalf("probe(studio=%s, cli=%s).Status = %q, want %q", Version, tc.cliVer, p.Status, tc.want)
		}
	}

	s.clearProbeCache()
	Version = "dev"
	for _, cliVer := range []string{"0.0.3", "1.0"} {
		s.clearProbeCache()
		reported = cliVer
		if p := s.probe(u); p.Status != "ok" {
			t.Fatalf("probe(studio=dev, cli=%s).Status = %q, want ok", cliVer, p.Status)
		}
	}

	// Outdated result must not be cached across an in-place upgrade at the same path/size/mtime.
	s.clearProbeCache()
	Version = "0.0.9"
	calls = 0
	reported = "0.0.3"
	if p := s.probe(u); p.Status != "outdated" {
		t.Fatalf("initial probe status = %q, want outdated", p.Status)
	}
	reported = "0.0.9"
	if p := s.probe(u); p.Status != "ok" {
		t.Fatalf("post-upgrade probe status = %q, want ok (cached outdated!)", p.Status)
	}
	if calls != 2 {
		t.Fatalf("expected 2 version probes across outdated->ok transition, got %d", calls)
	}
}

func TestProgressOutdatedAndErrorClassification(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })

	cliPath := fakeCLIFile(t)
	ws := t.TempDir()
	progressCalls := 0
	var nextVersion string
	var nextProgress inferyard.Execution

	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard " + nextVersion + " source=release\n")}, nil
		}
		if len(args) > 0 && args[0] == "progress" {
			progressCalls++
			return nextProgress, nil
		}
		return inferyard.Execution{}, nil
	})
	s.settings.InferyardPath = ptr(cliPath)
	s.settings.WorkspaceRoot = ptr(ws)

	// 1. Outdated CLI caught by probe without executing `progress`
	Version = "0.0.9"
	nextVersion = "0.0.3"
	progressCalls = 0
	if _, apiErr := s.Progress(context.Background()); apiErr == nil || apiErr.Code != "cli_outdated" {
		t.Fatalf("expected cli_outdated from probe, got %+v", apiErr)
	}
	if progressCalls != 0 {
		t.Fatalf("expected progress not to run when probe is outdated, ran %d times", progressCalls)
	}

	// 2. Under dev mode, progress_version: 1 yields cli_outdated
	Version = "dev"
	s.clearProbeCache()
	nextVersion = "0.0.3"
	v1Raw, err := os.ReadFile(filepath.Join("..", "inferyard", "testdata", "progress.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	nextProgress = inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: v1Raw}
	if _, apiErr := s.Progress(context.Background()); apiErr == nil || apiErr.Code != "cli_outdated" {
		t.Fatalf("expected cli_outdated for progress.v1.json, got %+v", apiErr)
	}

	// 3. ExitBlocked without `"command":"progress"` (both compact & spaced JSON) yields cli_outdated
	for _, body := range []string{`{"status":"blocked"}`, `{"command": "other"}`} {
		nextProgress = inferyard.Execution{ExitCode: inferyard.ExitBlocked, Stdout: []byte(body)}
		if _, apiErr := s.Progress(context.Background()); apiErr == nil || apiErr.Code != "cli_outdated" {
			t.Fatalf("expected cli_outdated for ExitBlocked body %s, got %+v", body, apiErr)
		}
	}
	// ExitBlocked with compact `"command":"progress"` is NOT cli_outdated
	nextProgress = inferyard.Execution{ExitCode: inferyard.ExitBlocked, Stdout: []byte(`{"command":"progress"}`)}
	if _, apiErr := s.Progress(context.Background()); apiErr != nil {
		t.Fatalf("expected nil error for compact ExitBlocked progress command, got %+v", apiErr)
	}

	// 4. Malformed JSON, missing progress_version, and future progress_version: 3 yield cli_error
	for _, bad := range [][]byte{
		[]byte(`{not-json`),
		[]byte(`{"kind":"progress","workspace":"/w","device_preflight":{"status":"ok","path":"/w/p"},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"done","next_command":null,"default_scope_complete":true}`),
		[]byte(`{"kind":"progress","progress_version":3,"workspace":"/w","device_preflight":{"status":"ok","path":"/w/p"},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"done","next_command":null,"default_scope_complete":true}`),
	} {
		nextProgress = inferyard.Execution{ExitCode: inferyard.ExitComplete, Stdout: bad}
		if _, apiErr := s.Progress(context.Background()); apiErr == nil || apiErr.Code != "cli_error" {
			t.Fatalf("expected cli_error for %s, got %+v", string(bad), apiErr)
		}
	}
}

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

func TestDiscoverDistinguishesUnavailableAndOutdatedCLI(t *testing.T) {
	origVer := Version
	t.Cleanup(func() { Version = origVer })
	Version = "0.0.9"

	nonExec := filepath.Join(t.TempDir(), cliBinaryName())
	if err := os.WriteFile(nonExec, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := testService(t, func(_ context.Context, _ string, _ ...string) (inferyard.Execution, error) {
		return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
	})
	s.settings.InferyardPath = ptr(nonExec)
	if _, apiErr := s.Discover(context.Background()); apiErr == nil || apiErr.Code != "cli_unavailable" {
		t.Fatalf("expected cli_unavailable for non-executable CLI, got %+v", apiErr)
	}

	outdated := fakeCLIFile(t)
	s.settings.InferyardPath = ptr(outdated)
	s.clearProbeCache()
	if _, apiErr := s.Discover(context.Background()); apiErr == nil || apiErr.Code != "cli_outdated" {
		t.Fatalf("expected cli_outdated for outdated CLI, got %+v", apiErr)
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

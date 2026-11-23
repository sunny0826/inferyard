package core

import (
	"context"
	"encoding/json"
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
		{"0.0.9", "ok"},
		{"0.0.10", "ok"},
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

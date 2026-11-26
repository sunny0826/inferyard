package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		{"0.0.9+local.1", "ok"},
		{"0.0.9+local+broken", "version_failed"},
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

func TestProbeCLIDeduplicatesConcurrentAndPersistsPositiveFingerprint(t *testing.T) {
	origVer := Version
	Version = "0.0.9"
	t.Cleanup(func() { Version = origVer })

	cliPath := fakeCLIFile(t)
	var versionCalls atomic.Int32
	gate := make(chan struct{})
	var closeOnce sync.Once
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			versionCalls.Add(1)
			<-gate
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
		}
		return inferyard.Execution{}, nil
	})
	s.settings.InferyardPath = ptr(cliPath)
	u := s.userSettings()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p := s.probe(u); p.Status != "ok" {
				t.Errorf("concurrent probe status = %q, want ok", p.Status)
			}
		}()
	}
	time.Sleep(15 * time.Millisecond)
	closeOnce.Do(func() { close(gate) })
	wg.Wait()

	if got := versionCalls.Load(); got != 1 {
		t.Fatalf("expected 8 concurrent cold probes to execute --version once, got %d", got)
	}

	// Rewinding probeCheckedAt by 1h keeps positive fingerprint cached with 0 subprocess calls.
	s.probeMu.Lock()
	s.probeCheckedAt = time.Now().Add(-time.Hour)
	s.probeMu.Unlock()
	if p := s.probe(u); p.Status != "ok" {
		t.Fatalf("cached probe status = %q, want ok", p.Status)
	}
	if got := versionCalls.Load(); got != 1 {
		t.Fatalf("expected positive probe cache to survive TTL without binary change, got %d calls", got)
	}

	// Changing binary size invalidates fingerprint and triggers a fresh probe.
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n# updated\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p := s.probe(u); p.Status != "ok" {
		t.Fatalf("updated binary probe status = %q, want ok", p.Status)
	}
	if got := versionCalls.Load(); got != 2 {
		t.Fatalf("expected binary change to re-probe once, got %d total calls", got)
	}
}

func TestProgressCachedInvalidatesOnWorkspaceArtifactChanges(t *testing.T) {
	workspace := t.TempDir()
	for _, sub := range []string{"bundles", "preflight", "candidates", "bound", "results", "reports"} {
		if err := os.MkdirAll(filepath.Join(workspace, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cliPath := fakeCLIFile(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace, InferyardPath: &cliPath})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var progressCalls atomic.Int32
	s, err := New(context.Background(), Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				n := progressCalls.Add(1)
				action := "device_check"
				if n > 1 {
					action = "prepare_candidate"
				}
				body := `{"kind":"progress","progress_version":2,"workspace":` + mustJSONString(t, workspace) + `,"device_preflight":{"status":"ok","path":null},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"` + action + `","next_command":null,"default_scope_complete":false}`
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(body)}, nil
			}
			return inferyard.Execution{ExitCode: 0}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	r1, apiErr := s.ProgressCached(context.Background())
	if apiErr != nil || r1.Progress == nil || r1.Progress.NextAction != "device_check" {
		t.Fatalf("1st ProgressCached failed: resp=%+v err=%+v", r1, apiErr)
	}
	r2, apiErr := s.ProgressCached(context.Background())
	if apiErr != nil || r2.Progress == nil || r2.Progress.NextAction != "device_check" {
		t.Fatalf("2nd ProgressCached failed: resp=%+v err=%+v", r2, apiErr)
	}
	if got := progressCalls.Load(); got != 1 {
		t.Fatalf("expected cached ProgressCached to execute CLI once, got %d", got)
	}

	// Creating a nested preflight file invalidates workspace signature and refreshes progress.
	nestedPre := filepath.Join(workspace, "preflight", "lab", "day1", "check2")
	if err := os.MkdirAll(nestedPre, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedPre, "device-preflight.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r3, apiErr := s.ProgressCached(context.Background())
	if apiErr != nil || r3.Progress == nil || r3.Progress.NextAction != "prepare_candidate" {
		t.Fatalf("ProgressCached after nested preflight change failed: resp=%+v err=%+v", r3, apiErr)
	}
	if got := progressCalls.Load(); got != 2 {
		t.Fatalf("expected nested preflight change to trigger 2nd progress call, got %d", got)
	}
}

func TestProgressCachedSkipsStoringWhenWorkspaceMutatesInFlight(t *testing.T) {
	workspace := t.TempDir()
	for _, sub := range []string{"bundles", "preflight", "candidates", "bound", "results", "reports"} {
		if err := os.MkdirAll(filepath.Join(workspace, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cliPath := fakeCLIFile(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace, InferyardPath: &cliPath})
	if err := os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var progressCalls atomic.Int32
	s, err := New(context.Background(), Options{
		SettingsPath: settingsPath,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				n := progressCalls.Add(1)
				action := "device_check"
				if n == 1 {
					// Simulate a concurrent preflight landing mid-flight after CLI started.
					preDir := filepath.Join(workspace, "preflight", "concurrent-1")
					_ = os.MkdirAll(preDir, 0o700)
					_ = os.WriteFile(filepath.Join(preDir, "device-preflight.json"), []byte(`{}`), 0o600)
				} else {
					action = "prepare_candidate"
				}
				body := `{"kind":"progress","progress_version":2,"workspace":` + mustJSONString(t, workspace) + `,"device_preflight":{"status":"ok","path":null},"bundles":[],"report":{"status":"missing","path":null,"runs":[]},"next_action":"` + action + `","next_command":null,"default_scope_complete":false}`
				return inferyard.Execution{ExitCode: 0, Stdout: []byte(body)}, nil
			}
			return inferyard.Execution{ExitCode: 0}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	r1, err1 := s.ProgressCached(context.Background())
	if err1 != nil || r1.Progress == nil || r1.Progress.NextAction != "device_check" {
		t.Fatalf("1st ProgressCached failed: %+v err=%+v", r1, err1)
	}
	// Must NOT have stored stale "device_check" under the post-mutation signature.
	r2, err2 := s.ProgressCached(context.Background())
	if err2 != nil || r2.Progress == nil || r2.Progress.NextAction != "prepare_candidate" {
		t.Fatalf("expected 2nd ProgressCached to recompute after mid-flight mutation, got %+v err=%+v", r2, err2)
	}
	if got := progressCalls.Load(); got != 2 {
		t.Fatalf("expected 2 CLI calls across mid-flight mutation, got %d", got)
	}
}

func mustJSONString(t *testing.T, v string) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

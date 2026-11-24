package core

import (
	"context"
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

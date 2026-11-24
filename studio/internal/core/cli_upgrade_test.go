package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

func waitForCLIUpgrade(t *testing.T, s *Service, want func(CLIUpgradeState) bool) CLIUpgradeState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := s.CLIUpgradeStatus()
		if want(st) {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := s.CLIUpgradeStatus()
	t.Fatalf("timed out waiting for CLI upgrade state; last=%+v", st)
	return st
}

func TestStartAutoCLIUpgradeUpgradesOutdatedCLISilently(t *testing.T) {
	stubInstall(t)
	oldCLI := fakeCLIFile(t)
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
		if len(args) == 1 && args[0] == "--version" {
			return []byte("uv " + uvRelease + "\n"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
			if err := os.WriteFile(installed, []byte("new-bin"), 0755); err != nil {
				t.Fatal(err)
			}
			return []byte("Upgraded"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "dir" {
			return []byte(binDir + "\n"), nil
		}
		return nil, errors.New("unexpected command")
	}

	Version = "0.0.12"
	t.Cleanup(func() { Version = "dev" })

	s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			if p == oldCLI {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.11 source=release\n")}, nil
			}
			if p == installed {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.12 source=release\n")}, nil
			}
		}
		return inferyard.Execution{}, errors.New("unexpected")
	})
	s.settings.InferyardPath = ptr(oldCLI)

	_, _, cursor := s.Bus.Subscribe()
	s.StartAutoCLIUpgrade()

	st := waitForCLIUpgrade(t, s, func(st CLIUpgradeState) bool {
		return st.State == CLIUpgradeIdle && s.Readiness().CLI.Status == "ok"
	})
	if st.Error != nil {
		t.Fatalf("expected nil error after auto upgrade, got %+v", st.Error)
	}
	if textOf(s.Settings().Settings.InferyardPath) != installed {
		t.Fatalf("inferyard_path = %q, want %q", textOf(s.Settings().Settings.InferyardPath), installed)
	}
	if r := s.Readiness(); r.CLI.Status != "ok" || str(r.CLI.Version) != "0.0.12" {
		t.Fatalf("Readiness.CLI = %+v, want ok 0.0.12", r.CLI)
	}
	events, _, _ := s.Bus.After(cursor)
	sawUpgrade, sawReset := false, false
	for _, ev := range events {
		if ev.Type == "cli.upgrade" {
			sawUpgrade = true
		}
		if ev.Type == "sync.reset" {
			sawReset = true
		}
	}
	if !sawUpgrade || !sawReset {
		t.Fatalf("expected cli.upgrade and sync.reset events, got upgrade=%v reset=%v", sawUpgrade, sawReset)
	}
}

func TestStartAutoCLIUpgradeRecordsFailureAndAllowsRetryOrManualFix(t *testing.T) {
	stubInstall(t)
	oldCLI := fakeCLIFile(t)
	manualCLI := fakeCLIFile(t)
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
	failInstall := true
	installCommandOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return []byte("uv " + uvRelease + "\n"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
			if failInstall {
				return []byte("error: Failed to fetch package from PyPI"), errors.New("exit status 1")
			}
			if err := os.WriteFile(installed, []byte("new-bin"), 0755); err != nil {
				t.Fatal(err)
			}
			return []byte("Upgraded"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "dir" {
			return []byte(binDir + "\n"), nil
		}
		return nil, errors.New("unexpected command")
	}

	Version = "0.0.12"
	t.Cleanup(func() { Version = "dev" })

	s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			switch p {
			case oldCLI:
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.11 source=release\n")}, nil
			case installed, manualCLI:
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.12 source=release\n")}, nil
			}
		}
		return inferyard.Execution{}, errors.New("unexpected")
	})
	s.settings.InferyardPath = ptr(oldCLI)

	s.StartAutoCLIUpgrade()
	failed := waitForCLIUpgrade(t, s, func(st CLIUpgradeState) bool {
		return st.State == CLIUpgradeFailed
	})
	if failed.Error == nil || failed.Error.Code != "install_failed" {
		t.Fatalf("expected install_failed error, got %+v", failed.Error)
	}
	if tail, _ := failed.Error.Details["output_tail"].(string); !strings.Contains(tail, "PyPI") {
		t.Fatalf("expected output_tail with uv stderr, got %+v", failed.Error.Details)
	}

	// Retrying succeeds and clears the failure state.
	failInstall = false
	got, apiErr := s.UpgradeCLI(context.Background(), nil)
	if apiErr != nil || got != installed {
		t.Fatalf("UpgradeCLI retry = (%q, %+v), want %q", got, apiErr, installed)
	}
	if st := s.CLIUpgradeStatus(); st.State != CLIUpgradeIdle || st.Error != nil {
		t.Fatalf("expected idle after retry success, got %+v", st)
	}

	// Switching back to oldCLI, failing again, then manually setting a compatible CLI in Settings clears error.
	oldPatch, _ := json.Marshal(oldCLI)
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": oldPatch}); e != nil {
		t.Fatal(e)
	}
	failInstall = true
	if _, e := s.UpgradeCLI(context.Background(), nil); e == nil {
		t.Fatal("expected second UpgradeCLI to fail")
	}
	if st := s.CLIUpgradeStatus(); st.State != CLIUpgradeFailed || st.Error == nil {
		t.Fatalf("expected failed state before manual fix, got %+v", st)
	}
	manualPatch, _ := json.Marshal(manualCLI)
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": manualPatch}); e != nil {
		t.Fatal(e)
	}
	if st := s.CLIUpgradeStatus(); st.State != CLIUpgradeIdle || st.Error != nil {
		t.Fatalf("expected manual settings fix to clear failed state, got %+v", st)
	}
}

func TestStartAutoCLIUpgradeSkipsFreshInstallAndUpToDate(t *testing.T) {
	t.Run("fresh install not_found", func(t *testing.T) {
		stubInstall(t)
		installLookPath = func(string) (string, error) { return "", errors.New("not found") }
		installStat = os.Stat
		installCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("uv must not run for fresh install without existing CLI")
			return nil, nil
		}
		Version = "0.0.12"
		t.Cleanup(func() { Version = "dev" })

		s := installService(t, func(_ context.Context, _ string, _ ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, errors.New("not found")
		})
		s.StartAutoCLIUpgrade()
		st := waitForCLIUpgrade(t, s, func(st CLIUpgradeState) bool {
			return st.State == CLIUpgradeIdle
		})
		if st.Error != nil {
			t.Fatalf("expected nil error for fresh install, got %+v", st.Error)
		}
	})

	t.Run("already compatible", func(t *testing.T) {
		stubInstall(t)
		goodCLI := fakeCLIFile(t)
		installLookPath = func(string) (string, error) { return "", errors.New("not found") }
		installStat = os.Stat
		installCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("uv must not run when CLI is already compatible")
			return nil, nil
		}
		Version = "0.0.12"
		t.Cleanup(func() { Version = "dev" })

		s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
			if p == goodCLI && len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.12 source=release\n")}, nil
			}
			return inferyard.Execution{}, errors.New("unexpected")
		})
		s.settings.InferyardPath = ptr(goodCLI)
		s.StartAutoCLIUpgrade()
		st := waitForCLIUpgrade(t, s, func(st CLIUpgradeState) bool {
			return st.State == CLIUpgradeIdle
		})
		if st.Error != nil {
			t.Fatalf("expected nil error for compatible CLI, got %+v", st.Error)
		}
	})
}

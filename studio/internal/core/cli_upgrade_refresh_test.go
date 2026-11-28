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

func TestUpgradeCLIWarmsReadinessServeAndResetsEngineChecksBeforeIdle(t *testing.T) {
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
			if err := os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			return []byte("Upgraded"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "dir" {
			return []byte(binDir + "\n"), nil
		}
		return nil, errors.New("unexpected command")
	}

	Version = "0.0.24"
	t.Cleanup(func() { Version = "dev" })

	serveSchema, err := os.ReadFile(filepath.Join("..", "..", "..", "cli", "schemas", "serve_plan.schema.json"))
	if err != nil {
		t.Fatalf("read serve_plan schema: %v", err)
	}
	s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			if p == oldCLI {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.23 source=release\n")}, nil
			}
			if p == installed {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.24 source=release\n")}, nil
			}
		}
		if p == installed && len(args) == 2 && args[0] == "--schema" && args[1] == "serve_plan" {
			return inferyard.Execution{ExitCode: 0, Stdout: serveSchema}, nil
		}
		return inferyard.Execution{}, errors.New("unexpected")
	})
	s.settings.InferyardPath = ptr(oldCLI)

	// Simulate stale engine check state recorded while CLI was outdated.
	s.checkMu.Lock()
	s.checkCLI = oldCLI + "|0.0.23|outdated"
	s.checkSupported = false
	s.checkNames = []engineCheckTarget{{name: "mlx-lm", path: "/engines/mlx-lm", key: "k1"}}
	s.checkMu.Unlock()
	if chk := s.EngineChecks(); chk.Supported || chk.Engines["mlx-lm"].Reason != "cli_outdated" {
		t.Fatalf("expected initial outdated engine check state, got %+v", chk)
	}

	_, _, cursor := s.Bus.Subscribe()
	got, apiErr := s.UpgradeCLI(context.Background(), nil)
	if apiErr != nil || got != installed {
		t.Fatalf("UpgradeCLI = (%q, %+v), want %q", got, apiErr, installed)
	}

	if chk := s.EngineChecks(); !chk.Supported || chk.Engines["mlx-lm"].Reason == "cli_outdated" {
		t.Fatalf("expected EngineChecks to clear cli_outdated immediately after upgrade, got %+v", chk)
	}
	if r := s.Readiness(); r.CLI.Status != "ok" || str(r.CLI.Version) != "0.0.24" {
		t.Fatalf("expected warmed Readiness ok 0.0.24, got %+v", r.CLI)
	}
	if !s.ServeSupported(context.Background()) {
		t.Fatal("expected ServeSupported to be pre-warmed after CLI upgrade")
	}

	// Verify sync.reset is published before cli.upgrade transitions back to idle.
	events, _, _ := s.Bus.After(cursor)
	resetIdx, idleIdx := -1, -1
	for i, ev := range events {
		switch ev.Type {
		case "sync.reset":
			resetIdx = i
		case "cli.upgrade":
			var payload struct {
				CLIUpgrade CLIUpgradeState `json:"cli_upgrade"`
			}
			if json.Unmarshal(ev.Data, &payload) == nil && payload.CLIUpgrade.State == CLIUpgradeIdle {
				idleIdx = i
			}
		}
	}
	if resetIdx < 0 || idleIdx < 0 || resetIdx > idleIdx {
		t.Fatalf("expected sync.reset (%d) before cli.upgrade idle (%d), events=%d", resetIdx, idleIdx, len(events))
	}
}

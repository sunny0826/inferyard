package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The engine verification pipeline otherwise runs only inside the
// event-driven refresh loop; on a quiet workspace no bus event arrives after
// the user opens the models page, so engines stayed "unverified" (and
// unselectable) forever. Opening the page must kick the verification itself.
func TestModelsPageSelectionTriggersEngineVerification(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "engines", "prism")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "llama-server"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A pre-engine-check CLI: --version works, engine check exits 2 with
	// command "unknown" — the reviewed cli_outdated degradation.
	service, err := core.New(context.Background(), core.Options{
		SettingsPath: writeNavSettings(t, root),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 0.0.3")}, nil
			}
			return inferyard.Execution{
				ExitCode: 2,
				Stdout:   []byte(`{"command":"unknown","status":"blocked","limitations":["invalid_input"]}`),
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())

	s := testState()
	s.service = service
	if got := len(service.EngineChecks().Engines); got != 0 {
		t.Fatal("engines verified before the models page was ever opened", got)
	}
	click(t, ui.NewTester(s.view, 1120, 800), "模型")
	if s.selected != "models" {
		t.Fatal("nav click missed, selected:", s.selected)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		converged := false
		for _, e := range service.EngineChecks().Engines {
			if e.State == core.EngineCheckUnknown && e.Reason == "cli_outdated" {
				converged = true // verification ran and hit the reviewed degradation
			}
		}
		if converged {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("opening the models page never verified the engine")
}

func writeNavSettings(t *testing.T, workspace string) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(core.UserSettings{InferyardPath: &binary, WorkspaceRoot: &workspace})
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return settings
} 

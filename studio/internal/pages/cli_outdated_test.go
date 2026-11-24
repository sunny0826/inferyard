package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestSettingsDraftSavePreservesUpgradedCLIPath(t *testing.T) {
	origVer := core.Version
	core.Version = "0.0.9"
	t.Cleanup(func() { core.Version = origVer })

	root := t.TempDir()
	oldCLI := filepath.Join(root, "old-inferyard")
	newCLI := filepath.Join(root, "new-inferyard")
	for _, p := range []string{oldCLI, newCLI} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ws := filepath.Join(root, "ws")
	oldResults := filepath.Join(root, "results-old")
	newResults := filepath.Join(root, "results-new")
	modelsDir := filepath.Join(root, "models")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc, err := core.New(ctx, core.Options{
		SettingsPath:  filepath.Join(root, "settings.json"),
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				if path == oldCLI {
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
				}
				if path == newCLI {
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
				}
			}
			return inferyard.Execution{ExitCode: 4}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	initPatch := map[string]json.RawMessage{}
	for k, v := range map[string]string{
		"inferyard_path": oldCLI,
		"workspace_root": ws,
		"results_root":   oldResults,
		"models_root":    modelsDir,
	} {
		initPatch[k], _ = json.Marshal(v)
	}
	if _, apiErr := svc.UpdateSettings(initPatch); apiErr != nil {
		t.Fatal(apiErr)
	}

	s := testState()
	s.selected = "settings"
	s.settingsLoaded = false
	s.service = svc
	updates := make(chan func(), 16)
	s.updateWindow = func(fn func()) { updates <- fn }
	s.applySettings(svc.Settings())

	// User has an unsaved results_root draft while clicking "升级 CLI".
	s.results = newResults
	if !s.settingsEdited() || !s.canSavePaths() {
		t.Fatalf("expected results draft to enable save: edited=%v canSave=%v", s.settingsEdited(), s.canSavePaths())
	}

	upgradedPatch, _ := json.Marshal(newCLI)
	upgradedSettings, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"inferyard_path": upgradedPatch})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	s.applySettings(upgradedSettings)

	if s.results != newResults {
		t.Fatalf("applySettings overwrote unsaved results draft: got %q want %q", s.results, newResults)
	}
	if s.cliPath != newCLI {
		t.Fatalf("applySettings did not sync untouched cliPath to upgraded CLI: got %q want %q", s.cliPath, newCLI)
	}

	tt := settingsTester(&s)
	click(t, tt, "保存")

	deadline := time.After(3 * time.Second)
	for s.busy {
		select {
		case fn := <-updates:
			fn()
		case <-deadline:
			t.Fatal("timed out waiting for settings save")
		}
	}

	saved := svc.Settings().Settings
	if saved.ResultsRoot == nil || *saved.ResultsRoot != newResults {
		t.Fatalf("saved ResultsRoot = %v, want %q", saved.ResultsRoot, newResults)
	}
	if saved.InferyardPath == nil || *saved.InferyardPath != newCLI {
		t.Fatalf("saving results draft reverted InferyardPath to %v, want %q", saved.InferyardPath, newCLI)
	}
}

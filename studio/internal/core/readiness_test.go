package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func quoteWizardArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestDefaultWorkspaceAndResultsLocations(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)

	s, err := New(context.Background(), Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	wantBase := filepath.Join(home, ".inferyard")
	wantWorkspace := filepath.Join(wantBase, "workspace")
	wantResults := filepath.Join(wantBase, "results")
	wantModels := filepath.Join(wantBase, "models")
	wantReports := filepath.Join(wantBase, "reports")

	if got := str(s.Settings().Effective.WorkspaceRoot); got != wantWorkspace {
		t.Fatalf("Effective.WorkspaceRoot = %q, want %q", got, wantWorkspace)
	}

	provisioned, apiErr := s.Provision()
	if apiErr != nil {
		t.Fatalf("Provision failed: %+v", apiErr)
	}
	if str(provisioned.Settings.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot = %q, want %q", str(provisioned.Settings.WorkspaceRoot), wantWorkspace)
	}
	if str(provisioned.Settings.ResultsRoot) != wantResults {
		t.Fatalf("ResultsRoot = %q, want %q", str(provisioned.Settings.ResultsRoot), wantResults)
	}
	if str(provisioned.Settings.ModelsRoot) != wantModels {
		t.Fatalf("ModelsRoot = %q, want %q", str(provisioned.Settings.ModelsRoot), wantModels)
	}
	if str(provisioned.Effective.ReportsRoot) != wantReports {
		t.Fatalf("ReportsRoot = %q, want %q", str(provisioned.Effective.ReportsRoot), wantReports)
	}
	// Workspace must NOT be created by Provision so `inferyard init --out` initializes it cleanly.
	if _, statErr := os.Stat(wantWorkspace); !os.IsNotExist(statErr) {
		t.Fatalf("Provision created workspace dir prematurely: %v", statErr)
	}
	for _, dir := range []string{wantBase, wantResults, wantModels} {
		st, statErr := os.Stat(dir)
		if statErr != nil || !st.IsDir() {
			t.Fatalf("Provision did not create %q: %v", dir, statErr)
		}
	}
	r := s.Readiness()
	if r.Workspace.Exists || r.Workspace.Initialized {
		t.Fatalf("Readiness.Workspace after Provision = %+v, want not exists/not initialized", r.Workspace)
	}
	if !r.Results.Exists || !r.Models.Exists {
		t.Fatalf("Readiness after Provision = %+v, want results and models to exist", r)
	}
}

func TestWizardInitPreservesConfiguredResultsAndConfigCreateInjectsResults(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	base := filepath.Join(home, ".inferyard")
	wantWorkspace := filepath.Join(base, "workspace")
	wantResults := filepath.Join(base, "results")
	wantReports := filepath.Join(base, "reports")

	var captured [][]string
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 9.9.9")}, nil
			}
			captured = append(captured, append([]string(nil), args...))
			if len(args) > 0 && args[0] == "init" {
				out := args[2]
				_ = os.MkdirAll(out, 0700)
				_ = os.WriteFile(filepath.Join(out, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600)
				return inferyard.Execution{}, nil
			}
			if len(args) > 0 && args[0] == "progress" {
				raw, _ := json.Marshal(inferyard.WorkspaceProgress{
					Kind:            "progress",
					ProgressVersion: 2,
					Workspace:       wantWorkspace,
					DevicePreflight: inferyard.ProgressPreflight{Status: "missing"},
					Report:          inferyard.ProgressReport{Status: "missing", Runs: []string{}},
					NextAction:      "device_check",
					Bundles:         []inferyard.BundleProgress{},
				})
				return inferyard.Execution{Stdout: raw}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	rawCLI, _ := json.Marshal(exe)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": rawCLI}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := s.Provision(); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard init --out " + quoteWizardArg(wantWorkspace)}); apiErr != nil {
		t.Fatalf("WizardExecute init: %+v", apiErr)
	}
	got := s.Settings().Settings
	if str(got.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot after init = %q, want %q", str(got.WorkspaceRoot), wantWorkspace)
	}
	if str(got.ResultsRoot) != wantResults {
		t.Fatalf("ResultsRoot after init = %q, want %q (must not be clobbered)", str(got.ResultsRoot), wantResults)
	}

	// config create without --results must inject configured ResultsRoot.
	candOut := filepath.Join(wantWorkspace, "candidates", "zh-core-1")
	if _, apiErr := s.WizardExecute(context.Background(), WizardRequest{
		Command: "inferyard config create --workspace " + quoteWizardArg(wantWorkspace) + " --bundle zh-core --out " + quoteWizardArg(candOut),
	}); apiErr != nil {
		t.Fatalf("WizardExecute config create: %+v", apiErr)
	}
	last := captured[len(captured)-1]
	foundResults := ""
	for i := 0; i+1 < len(last); i++ {
		if last[i] == "--results" {
			foundResults = last[i+1]
		}
	}
	if foundResults != wantResults {
		t.Fatalf("config create args %v did not inject --results %q", last, wantResults)
	}

	// Progress must pass --results and --reports when they live outside workspace.
	if _, apiErr := s.Progress(context.Background()); apiErr != nil {
		t.Fatalf("Progress failed: %+v", apiErr)
	}
	progArgs := captured[len(captured)-1]
	var gotRes, gotRep string
	for i := 0; i+1 < len(progArgs); i++ {
		if progArgs[i] == "--results" {
			gotRes = progArgs[i+1]
		}
		if progArgs[i] == "--reports" {
			gotRep = progArgs[i+1]
		}
	}
	if gotRes != wantResults || gotRep != wantReports {
		t.Fatalf("progress args %v: got results=%q reports=%q, want %q and %q", progArgs, gotRes, gotRep, wantResults, wantReports)
	}
}

package pages

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestPrepareEnvironmentUsesDefaultWorkspaceAndResultsWithoutTimestamp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var initOut string
	service, err := core.New(context.Background(), core.Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 9.9.9")}, nil
			}
			if len(args) >= 3 && args[0] == "init" && args[1] == "--out" {
				initOut = args[2]
				_ = os.MkdirAll(initOut, 0700)
				_ = os.WriteFile(filepath.Join(initOut, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600)
				return inferyard.Execution{}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())

	rawCLI, _ := json.Marshal(exe)
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{"inferyard_path": rawCLI}); apiErr != nil {
		t.Fatal(apiErr)
	}

	apiErr := prepareEnvironmentSteps(
		context.Background(),
		service.Readiness,
		func(context.Context) (string, *core.ApiError) {
			t.Fatal("CLI already ok; install should not be called")
			return "", nil
		},
		func() *core.ApiError {
			_, e := service.Provision()
			return e
		},
		service.WizardExecute,
	)
	if apiErr != nil {
		t.Fatalf("prepareEnvironmentSteps failed: %+v", apiErr)
	}

	wantBase := filepath.Join(home, ".inferyard")
	wantWorkspace := filepath.Join(wantBase, "workspace")
	wantResults := filepath.Join(wantBase, "results")
	wantModels := filepath.Join(wantBase, "models")

	if initOut != wantWorkspace {
		t.Fatalf("init --out = %q, want %q", initOut, wantWorkspace)
	}
	saved := service.Settings().Settings
	if emptyText(saved.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot = %q, want %q", emptyText(saved.WorkspaceRoot), wantWorkspace)
	}
	if emptyText(saved.ResultsRoot) != wantResults {
		t.Fatalf("ResultsRoot = %q, want %q", emptyText(saved.ResultsRoot), wantResults)
	}
	if emptyText(saved.ModelsRoot) != wantModels {
		t.Fatalf("ModelsRoot = %q, want %q", emptyText(saved.ModelsRoot), wantModels)
	}
}

func TestFinishBenchmarkReportWritesToExternalReportsRoot(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "workspace")
	reports := filepath.Join(base, "reports")
	runPath := filepath.Join(base, "results", "run-1")
	if err := os.MkdirAll(reports, 0700); err != nil {
		t.Fatal(err)
	}
	f := &taskBackendFixture{
		p: &inferyard.WorkspaceProgress{
			Workspace: workspace,
			Bundles: []inferyard.BundleProgress{{
				Bundle: "zh-core",
				Runs:   []inferyard.ProgressRun{{Path: runPath, Complete: true, Sealed: true}},
			}},
		},
	}
	var gotReport string
	if e := finishBenchmarkReport(context.Background(), f, workspace, reports, []string{"zh-core"}, []string{runPath}, func(string) {}, func(_ []string, out string) {
		gotReport = out
	}); e != nil {
		t.Fatalf("finishBenchmarkReport failed: %+v", e)
	}
	wantReport := filepath.Join(reports, "summary-1")
	if filepath.Clean(gotReport) != filepath.Clean(wantReport) {
		t.Fatalf("report out = %q, want %q", gotReport, wantReport)
	}
}

func TestPrepareEnvironmentCustomNestedDirsDoNotCreateTimestampWorkspace(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace := filepath.Join(base, "bench")
	wantResults := filepath.Join(wantWorkspace, "results")
	wantModels := filepath.Join(wantWorkspace, "models")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var initOut string
	service, err := core.New(context.Background(), core.Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 9.9.9")}, nil
			}
			if len(args) >= 3 && args[0] == "init" && args[1] == "--out" {
				initOut = args[2]
				_ = os.MkdirAll(filepath.Join(initOut, "results"), 0700)
				_ = os.WriteFile(filepath.Join(initOut, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600)
				return inferyard.Execution{}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())

	mustJSON := func(v any) json.RawMessage {
		raw, _ := json.Marshal(v)
		return raw
	}
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": mustJSON(exe),
		"workspace_root": mustJSON(wantWorkspace),
		"results_root":   mustJSON(wantResults),
		"models_root":    mustJSON(wantModels),
	}); apiErr != nil {
		t.Fatal(apiErr)
	}

	apiErr := prepareEnvironmentSteps(
		context.Background(),
		service.Readiness,
		func(context.Context) (string, *core.ApiError) {
			t.Fatal("CLI already ok; install should not be called")
			return "", nil
		},
		func() *core.ApiError {
			_, e := service.Provision()
			return e
		},
		service.WizardExecute,
	)
	if apiErr != nil {
		t.Fatalf("prepareEnvironmentSteps failed: %+v", apiErr)
	}
	if initOut != wantWorkspace {
		t.Fatalf("init --out = %q, want %q", initOut, wantWorkspace)
	}
	saved := service.Settings().Settings
	if emptyText(saved.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot = %q, want %q", emptyText(saved.WorkspaceRoot), wantWorkspace)
	}
	if emptyText(saved.ResultsRoot) != wantResults {
		t.Fatalf("ResultsRoot = %q, want %q", emptyText(saved.ResultsRoot), wantResults)
	}
	if emptyText(saved.ModelsRoot) != wantModels {
		t.Fatalf("ModelsRoot = %q, want %q", emptyText(saved.ModelsRoot), wantModels)
	}
}

func TestPrepareEnvironmentCustomWorkspaceWithDefaultResultsMatchesPreview(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	customBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace := filepath.Join(customBase, "bench")
	wantResults := filepath.Join(home, ".inferyard", "results")
	wantModels := filepath.Join(home, ".inferyard", "models")
	wantReports := filepath.Join(home, ".inferyard", "reports")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	service, err := core.New(context.Background(), core.Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard 9.9.9")}, nil
			}
			if len(args) >= 3 && args[0] == "init" && args[1] == "--out" {
				out := args[2]
				_ = os.MkdirAll(out, 0700)
				_ = os.WriteFile(filepath.Join(out, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600)
				return inferyard.Execution{}, nil
			}
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())

	mustJSON := func(v any) json.RawMessage {
		raw, _ := json.Marshal(v)
		return raw
	}
	if _, apiErr := service.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": mustJSON(exe),
		"workspace_root": mustJSON(wantWorkspace),
	}); apiErr != nil {
		t.Fatal(apiErr)
	}

	preview := service.PreparationLocations()
	if emptyText(preview.Workspace) != wantWorkspace || emptyText(preview.Results) != wantResults || emptyText(preview.Models) != wantModels {
		t.Fatalf("PreparationLocations = (%q, %q, %q), want (%q, %q, %q)",
			emptyText(preview.Workspace), emptyText(preview.Results), emptyText(preview.Models),
			wantWorkspace, wantResults, wantModels)
	}

	apiErr := prepareEnvironmentSteps(
		context.Background(),
		service.Readiness,
		func(context.Context) (string, *core.ApiError) {
			t.Fatal("CLI already ok; install should not be called")
			return "", nil
		},
		func() *core.ApiError {
			_, e := service.Provision()
			return e
		},
		service.WizardExecute,
	)
	if apiErr != nil {
		t.Fatalf("prepareEnvironmentSteps failed: %+v", apiErr)
	}

	resp := service.Settings()
	if emptyText(resp.Settings.WorkspaceRoot) != wantWorkspace {
		t.Fatalf("WorkspaceRoot = %q, want %q", emptyText(resp.Settings.WorkspaceRoot), wantWorkspace)
	}
	if emptyText(resp.Settings.ResultsRoot) != wantResults {
		t.Fatalf("ResultsRoot = %q, want %q", emptyText(resp.Settings.ResultsRoot), wantResults)
	}
	if emptyText(resp.Settings.ModelsRoot) != wantModels {
		t.Fatalf("ModelsRoot = %q, want %q", emptyText(resp.Settings.ModelsRoot), wantModels)
	}
	if emptyText(resp.Effective.ReportsRoot) != wantReports {
		t.Fatalf("ReportsRoot = %q, want %q", emptyText(resp.Effective.ReportsRoot), wantReports)
	}
}

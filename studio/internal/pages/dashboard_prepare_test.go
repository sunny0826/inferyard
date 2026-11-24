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

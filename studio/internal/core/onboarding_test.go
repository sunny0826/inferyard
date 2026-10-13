package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func wizardService(t *testing.T, execute Executor) *Service {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	root := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(UserSettings{InferyardPath: &binary, WorkspaceRoot: &root})
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, Options{SettingsPath: settings, Execute: execute})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestWizardExitMappingAndReportVerify(t *testing.T) {
	for _, code := range []int{0, 2, 3, 4, 130} {
		var calls [][]string
		s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if args[0] == "--version" {
				return inferyard.Execution{Stdout: []byte("inferyard fixture")}, nil
			}
			calls = append(calls, append([]string{}, args...))
			return inferyard.Execution{ExitCode: code, Stdout: []byte("secret stdout"), Stderr: []byte("secret stderr")}, nil
		})
		out := filepath.Join(t.TempDir(), "new-report")
		result, err := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard report --runs '/tmp/core' '/tmp/pelican' --out '" + out + "'", ReportVerify: true})
		if err != nil || result.Status != inferyard.MapExitCode(code) || result.ExitCode != code {
			t.Fatalf("%+v %v", result, err)
		}
		want := 1
		if code == 0 {
			want = 2
		}
		if len(calls) != want {
			t.Fatalf("calls: %v", calls)
		}
		if code == 0 && !reflect.DeepEqual(calls[1], []string{"verify", "--path", out}) {
			t.Fatal(calls)
		}
	}
}
func TestWizardKeepsOriginalOutputsAndFrozenStartup(t *testing.T) {
	s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] != "--version" {
			t.Fatal("executed despite existing output")
		}
		return inferyard.Execution{Stdout: []byte("inferyard fixture")}, nil
	})
	root := *workspaceRoot(s.userSettings())
	_, err := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard init --out '" + root + "'"})
	if err == nil || err.Code != "conflict" {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "candidate")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, "candidate.toml"), []byte("[engine]\nbinary_path = \"/engine/server\"\nstartup_args = [\"-m\", \"/models/space model.gguf\", \"--port\", \"48857\"]\n"), 0600)
	frozen, e := s.Startup(dir)
	if e != nil || frozen.Path != "/engine/server" || !reflect.DeepEqual(frozen.Args, []string{"-m", "/models/space model.gguf", "--port", "48857"}) {
		t.Fatalf("%+v %v", frozen, e)
	}
	if _, e := s.Startup(filepath.Join(root, "..", "outside")); e == nil {
		t.Fatal("outside workspace")
	}
}
func TestProgressUsesInjectedExecutor(t *testing.T) {
	s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard fixture")}, nil
		}
		if len(args) != 3 || args[0] != "progress" || args[1] != "--workspace" {
			t.Fatal(args)
		}
		return inferyard.Execution{ExitCode: 2, Stdout: []byte("{\"command\": \"progress\", \"status\": \"blocked\"}")}, nil
	})
	result, err := s.Progress(context.Background())
	if err != nil || result.Status != inferyard.Blocked || result.Progress != nil {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestWizardDeviceCommandUsesConfiguredModelRoot(t *testing.T) {
	var executed []string
	s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard fixture")}, nil
		}
		executed = append([]string{}, args...)
		return inferyard.Execution{}, nil
	})
	models := filepath.Join(t.TempDir(), "models")
	raw, _ := json.Marshal(models)
	if _, err := s.UpdateSettings(map[string]json.RawMessage{"models_root": raw}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "new-preflight")
	result, err := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard device-check --out '" + out + "'"})
	if err != nil || result.ExitCode != 0 || !reflect.DeepEqual(executed, []string{"device-check", "--out", out, "--models-root", models, "--json"}) {
		t.Fatalf("%+v %v %v", result, executed, err)
	}
}

func TestWizardInitHandoff(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new-workspace")
	s := wizardService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard fixture")}, nil
		}
		if !reflect.DeepEqual(args, []string{"init", "--out", root}) {
			t.Fatal(args)
		}
		if err := os.MkdirAll(filepath.Join(root, "results"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600); err != nil {
			t.Fatal(err)
		}
		return inferyard.Execution{}, nil
	})
	result, err := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard init --out '" + root + "'"})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	settings := s.userSettings()
	if str(settings.WorkspaceRoot) != root || str(settings.ResultsRoot) != filepath.Join(root, "results") || !s.Readiness().Workspace.Initialized {
		t.Fatalf("%+v", settings)
	}
}

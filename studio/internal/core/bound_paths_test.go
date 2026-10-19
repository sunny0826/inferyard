package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func boundTestService(t *testing.T, check func(context.Context) error) (*Service, *BoundPaths) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &BoundPaths{CLI: filepath.Join(root, "cli"), Workspace: filepath.Join(root, "workspace"), Results: filepath.Join(root, "workspace", "results"), Models: filepath.Join(root, "assets", "models"), Engines: filepath.Join(root, "assets", "engines"), Settings: filepath.Join(root, "settings.json"), Source: "test checkout", WindowTitle: "开发环境"}
	if err := os.WriteFile(p.CLI, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	stale := UserSettings{InferyardPath: ptr("/old/cli"), WorkspaceRoot: ptr("/old/workspace"), ResultsRoot: ptr("/old/results"), ModelsRoot: ptr("/old/models"), ModelDownloadName: "kept"}
	raw, _ := json.Marshal(stale)
	if err := os.WriteFile(p.Settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{Paths: p, ValidateCLI: check, Execute: func(_ context.Context, path string, args ...string) (inferyard.Execution, error) {
		if path != p.CLI {
			return inferyard.Execution{}, errors.New("wrong CLI")
		}
		if v, ok := version(args); ok {
			return v, nil
		}
		if args[0] == "init" {
			if err := os.MkdirAll(filepath.Join(p.Workspace, "results"), 0700); err != nil {
				return inferyard.Execution{}, err
			}
			if err := os.WriteFile(filepath.Join(p.Workspace, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600); err != nil {
				return inferyard.Execution{}, err
			}
		}
		return result(0), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return s, p
}

func TestBoundPathsInitUsesFixedWorkspace(t *testing.T) {
	s, p := boundTestService(t, func(context.Context) error { return nil })
	if _, e := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard init --out '" + filepath.Join(filepath.Dir(p.Workspace), "other") + "'"}); e == nil || e.Details["reason"] != "paths_bound" {
		t.Fatal(e)
	}
	result, e := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard init --out '" + p.Workspace + "'"})
	if e != nil || result.ExitCode != 0 || !s.Readiness().Workspace.Initialized {
		t.Fatal(result, e, s.Readiness())
	}
	if _, e := s.Provision(); e != nil {
		t.Fatal(e)
	}
	if !p.matches(s.userSettings()) {
		t.Fatal(s.userSettings())
	}
}

func TestBoundPathsOverrideOldSettingsAndRejectChanges(t *testing.T) {
	s, p := boundTestService(t, func(context.Context) error { return nil })
	if !p.matches(s.userSettings()) || s.userSettings().ModelDownloadName != "kept" {
		t.Fatal(s.userSettings())
	}
	if s.hostStatePath != "" {
		t.Fatal("host state unexpectedly isolated")
	}
	for _, key := range []string{"inferyard_path", "workspace_root", "results_root", "models_root"} {
		if _, e := s.UpdateSettings(map[string]json.RawMessage{key: []byte(`"/other"`)}); e == nil || e.Details["reason"] != "paths_bound" {
			t.Fatal(key, e)
		}
	}
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"engine_download_name": []byte(`"custom"`)}); e != nil {
		t.Fatal(e)
	}
	if !p.matches(s.userSettings()) || s.userSettings().EngineDownloadName != "custom" {
		t.Fatal(s.userSettings())
	}
	u := s.userSettings()
	u.WorkspaceRoot = ptr("/other")
	if _, e := s.saveSettings(u); e == nil {
		t.Fatal("internal handoff changed bound workspace")
	}
	copy := s.BoundPaths()
	copy.CLI = "/wrong"
	if s.BoundPaths().CLI != p.CLI {
		t.Fatal("external mutation changed binding")
	}
}

func TestBoundPathsInstallNeverFallsBack(t *testing.T) {
	var validations atomic.Int32
	s, p := boundTestService(t, func(context.Context) error {
		validations.Add(1)
		return nil
	})
	stubInstall(t)
	installLookPath = func(string) (string, error) { t.Error("installer searched PATH"); return "", os.ErrNotExist }
	installCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		t.Error("installer executed")
		return nil, nil
	}
	if got, e := s.InstallCLI(context.Background(), func(string) {}); e != nil || got != p.CLI {
		t.Fatal(got, e)
	}
	for range 3 {
		s.Settings()
		s.Status()
		s.Readiness()
	}
	if validations.Load() != 1 {
		t.Fatal("full validation repeated", validations.Load())
	}
	if err := os.Remove(p.CLI); err != nil {
		t.Fatal(err)
	}
	if _, e := s.InstallCLI(context.Background(), func(string) {}); e == nil {
		t.Fatal("mismatched CLI accepted")
	}
	if got := s.probe(s.userSettings()); got.Status != "not_found" {
		t.Fatal(got)
	}
}

func TestBoundPathsStartupValidationRejectsBeforeService(t *testing.T) {
	s, p := boundTestService(t, func(context.Context) error { return nil })
	s.Shutdown(context.Background())
	want := errors.New("invalid installed artifact")
	service, err := New(context.Background(), Options{Paths: p, ValidateCLI: func(context.Context) error { return want }})
	if service != nil || !errors.Is(err, want) {
		t.Fatal(service, err)
	}
	if service, err := New(context.Background(), Options{Paths: p}); service != nil || err == nil {
		t.Fatal("missing startup validator accepted")
	}
}

func TestBoundPathsAssetRoots(t *testing.T) {
	s, p := boundTestService(t, func(context.Context) error { return nil })
	roots := s.registryRoots(p.Workspace)
	if len(roots) != 1 || roots[0] != p.Engines {
		t.Fatal(roots)
	}
	old := installHTTPDo
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) { return httpBody(500, "fixture"), nil })
	t.Cleanup(func() { installHTTPDo = old })
	task, e := s.CreateEngineDownload(DefaultEngineURL(), "")
	if e != nil {
		t.Fatal(e)
	}
	if filepath.Dir(task.OutDir) != p.Engines {
		t.Fatal(task.OutDir)
	}
	s.Shutdown(context.Background())
}

package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestDownloadSourceSettingPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "settings.json")
	svc, err := New(context.Background(), Options{SettingsPath: settingsFile})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	if got := svc.DownloadSource(); got != DownloadSourceModelScope {
		t.Fatalf("default DownloadSource() = %q, want %q", got, DownloadSourceModelScope)
	}
	if got := svc.Settings().Effective.DownloadSource; got != DownloadSourceModelScope {
		t.Fatalf("default Effective.DownloadSource = %q, want %q", got, DownloadSourceModelScope)
	}

	rawHF, _ := json.Marshal(" HuggingFace ")
	resp, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"download_source": rawHF})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(huggingface): %+v", apiErr)
	}
	if resp.Settings.DownloadSource != DownloadSourceHuggingFace || resp.Effective.DownloadSource != DownloadSourceHuggingFace {
		t.Fatalf("unexpected huggingface response: %+v", resp)
	}
	if got := svc.DownloadSource(); got != DownloadSourceHuggingFace {
		t.Fatalf("svc.DownloadSource() = %q, want %q", got, DownloadSourceHuggingFace)
	}

	reloaded, err := New(context.Background(), Options{SettingsPath: settingsFile})
	if err != nil {
		t.Fatalf("reload New: %v", err)
	}
	t.Cleanup(func() { _ = reloaded.Shutdown(context.Background()) })
	if got := reloaded.DownloadSource(); got != DownloadSourceHuggingFace {
		t.Fatalf("reloaded DownloadSource() = %q, want %q", got, DownloadSourceHuggingFace)
	}

	rawBad, _ := json.Marshal("github")
	if _, apiErr := reloaded.UpdateSettings(map[string]json.RawMessage{"download_source": rawBad}); apiErr == nil || apiErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for unsupported download source, got %+v", apiErr)
	}

	resp, apiErr = reloaded.UpdateSettings(map[string]json.RawMessage{"download_source": json.RawMessage("null")})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(null): %+v", apiErr)
	}
	if resp.Settings.DownloadSource != DownloadSourceModelScope || resp.Effective.DownloadSource != DownloadSourceModelScope {
		t.Fatalf("expected modelscope reset, got %+v", resp)
	}
}

func TestDownloadSourceAllowedWithBoundPaths(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cliPath, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(root, "ws")
	bound := &BoundPaths{
		Source:    "dev",
		CLI:       cliPath,
		Workspace: ws,
		Results:   filepath.Join(ws, "results"),
		Models:    filepath.Join(ws, "models"),
		Engines:   filepath.Join(ws, "engines"),
		Settings:  filepath.Join(root, "settings.json"),
	}
	svc, err := New(context.Background(), Options{
		SettingsPath: bound.Settings,
		Paths:        bound,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	rawHF, _ := json.Marshal(DownloadSourceHuggingFace)
	resp, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"download_source": rawHF})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(download_source) under bound paths failed: %+v", apiErr)
	}
	if resp.Effective.DownloadSource != DownloadSourceHuggingFace {
		t.Fatalf("Effective.DownloadSource = %q, want %q", resp.Effective.DownloadSource, DownloadSourceHuggingFace)
	}
}

func TestPolystrataSetupInheritsDefaultDownloadSource(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	for _, dir := range []string{ws, filepath.Join(ws, "models"), filepath.Join(ws, "results"), filepath.Join(ws, "engines")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	settingsPath := filepath.Join(root, "settings.json")
	rawSettings, _ := json.Marshal(UserSettings{
		WorkspaceRoot: &ws,
		InferyardPath: ptr(os.Args[0]),
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	svc, err := New(context.Background(), Options{
		SettingsPath:  settingsPath,
		HostStatePath: filepath.Join(root, "host.json"),
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, nil
			}
			return inferyard.Execution{ExitCode: 0}, nil
		},
		ExecStream: func(ctx context.Context, _ func(string), _ string, _ ...string) (inferyard.Execution, error) {
			select {
			case <-done:
			case <-ctx.Done():
			}
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		close(done)
		_ = svc.Shutdown(context.Background())
	})

	task, apiErr := svc.CreatePolystrataSetup(PolystrataSetupRequest{
		Family:  "qwen3",
		Model:   "35b",
		Context: 4096,
		Backend: "metal",
	})
	if apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}
	if task.ModelSource != DownloadSourceModelScope {
		t.Fatalf("task.ModelSource = %q, want %q", task.ModelSource, DownloadSourceModelScope)
	}
}

package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func initTestWorkspaceMarker(t *testing.T, ws string) {
	t.Helper()
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "preparation.json"), []byte(`{"kind":"community_init.v1","schema_version":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCleanWorkspaceArtifactsRemovesArtifactsAndPreservesAssets(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	initTestWorkspaceMarker(t, ws)
	for _, dir := range []string{
		filepath.Join(ws, "candidates", "zh-core-1"),
		filepath.Join(ws, "bound", "zh-core-1"),
		filepath.Join(ws, "results", "run-1"),
		filepath.Join(ws, "reports", "summary-1"),
		filepath.Join(ws, "logs", "context-speed-tasks"),
		filepath.Join(ws, "logs", "other"),
		filepath.Join(ws, "bundles"),
		filepath.Join(ws, "preflight", "device-1"),
		filepath.Join(ws, "models", "qwen"),
		filepath.Join(ws, "engines", "llama"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{
		filepath.Join(ws, "bundles", "zh-core.json"),
		filepath.Join(ws, "preflight", "device-1", "device-preflight.json"),
		filepath.Join(ws, "models", "qwen", "model.gguf"),
		filepath.Join(ws, "engines", "llama", "llama-server"),
		filepath.Join(ws, "candidates", "zh-core-1", "preparation.json"),
		filepath.Join(ws, "bound", "zh-core-1", "config.toml"),
		filepath.Join(ws, "results", "run-1", "inputs.json"),
		filepath.Join(ws, "reports", "summary-1", "studio.json"),
		filepath.Join(ws, "logs", "context-speed-tasks", "t1.json"),
		filepath.Join(ws, "logs", "other", "keep.log"),
		filepath.Join(ws, exclusivitySidecarName),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	svc, err := New(context.Background(), Options{SettingsPath: filepath.Join(root, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()
	rawWS, _ := json.Marshal(ws)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawWS}); apiErr != nil {
		t.Fatalf("UpdateSettings failed: %+v", apiErr)
	}
	svc.mu.Lock()
	svc.runs = []*runEntry{{Task: RunTask{TaskId: "done-1", State: "done"}}}
	svc.mu.Unlock()

	res, apiErr := svc.CleanWorkspaceArtifacts()
	if apiErr != nil {
		t.Fatalf("CleanWorkspaceArtifacts failed: %+v", apiErr)
	}
	if len(res.Removed) != 6 {
		t.Fatalf("expected 6 removed entries, got %d: %v", len(res.Removed), res.Removed)
	}

	for _, gone := range []string{
		filepath.Join(ws, "candidates"),
		filepath.Join(ws, "bound"),
		filepath.Join(ws, "results", "run-1"),
		filepath.Join(ws, "reports"),
		filepath.Join(ws, "logs", "context-speed-tasks"),
		filepath.Join(ws, exclusivitySidecarName),
	} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", gone, err)
		}
	}
	// Empty results directory is preserved so workspace stays ready.
	if st, err := os.Lstat(filepath.Join(ws, "results")); err != nil || !st.IsDir() {
		t.Fatalf("expected empty results dir to be preserved, err=%v", err)
	}
	for _, kept := range []string{
		filepath.Join(ws, "preparation.json"),
		filepath.Join(ws, "bundles", "zh-core.json"),
		filepath.Join(ws, "preflight", "device-1", "device-preflight.json"),
		filepath.Join(ws, "models", "qwen", "model.gguf"),
		filepath.Join(ws, "engines", "llama", "llama-server"),
		filepath.Join(ws, "logs", "other", "keep.log"),
	} {
		if _, err := os.Lstat(kept); err != nil {
			t.Fatalf("expected %s to be preserved, err=%v", kept, err)
		}
	}
	if tasks := svc.RunTasks(); len(tasks) != 0 {
		t.Fatalf("expected terminal run tasks to be cleared, got %d", len(tasks))
	}
}

func TestCleanWorkspaceArtifactsRejectsUninitializedDirectory(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "plain-dir")
	if err := os.MkdirAll(filepath.Join(ws, "results", "user-data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "results", "user-data", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := New(context.Background(), Options{SettingsPath: filepath.Join(root, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()
	rawWS, _ := json.Marshal(ws)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawWS}); apiErr != nil {
		t.Fatal(apiErr)
	}

	if _, apiErr := svc.CleanWorkspaceArtifacts(); apiErr == nil || apiErr.Code != "settings_incomplete" {
		t.Fatalf("expected settings_incomplete for uninitialized directory, got %+v", apiErr)
	}
	if _, err := os.Stat(filepath.Join(ws, "results", "user-data", "keep.txt")); err != nil {
		t.Fatalf("expected plain directory contents to remain untouched: %v", err)
	}
}

func TestCleanWorkspaceArtifactsRejectsActiveRun(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	initTestWorkspaceMarker(t, ws)
	if err := os.MkdirAll(filepath.Join(ws, "results", "run-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc, err := New(context.Background(), Options{SettingsPath: filepath.Join(root, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()
	rawWS, _ := json.Marshal(ws)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawWS}); apiErr != nil {
		t.Fatal(apiErr)
	}

	svc.mu.Lock()
	svc.runs = []*runEntry{{Task: RunTask{TaskId: "r1", State: "running"}}}
	svc.mu.Unlock()

	if _, apiErr := svc.CleanWorkspaceArtifacts(); apiErr == nil || apiErr.Code != "run_active" {
		t.Fatalf("expected run_active, got %+v", apiErr)
	}
	if _, err := os.Stat(filepath.Join(ws, "results", "run-1")); err != nil {
		t.Fatalf("expected results to remain untouched when run_active: %v", err)
	}
}

func TestCleanWorkspaceArtifactsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	initTestWorkspaceMarker(t, ws)
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "results")); err != nil {
		t.Fatal(err)
	}

	svc, err := New(context.Background(), Options{SettingsPath: filepath.Join(root, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()
	rawWS, _ := json.Marshal(ws)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": rawWS}); apiErr != nil {
		t.Fatal(apiErr)
	}

	if _, apiErr := svc.CleanWorkspaceArtifacts(); apiErr == nil {
		t.Fatalf("expected error when results is a symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("expected symlink target to remain intact: %v", err)
	}
}

func TestCleanWorkspaceArtifactsRejectsProtectedOverlap(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	initTestWorkspaceMarker(t, ws)
	resultsDir := filepath.Join(ws, "results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resultsDir, "model.gguf"), []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := New(context.Background(), Options{SettingsPath: filepath.Join(root, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()
	rawWS, _ := json.Marshal(ws)
	rawExtra, _ := json.Marshal([]string{resultsDir})
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{
		"workspace_root":     rawWS,
		"extra_models_roots": rawExtra,
	}); apiErr != nil {
		t.Fatal(apiErr)
	}

	if _, apiErr := svc.CleanWorkspaceArtifacts(); apiErr == nil {
		t.Fatalf("expected error when results overlaps model root")
	}
	if _, err := os.Stat(filepath.Join(resultsDir, "model.gguf")); err != nil {
		t.Fatalf("expected overlapping model file to remain intact: %v", err)
	}
}

func TestCleanWorkspaceArtifactsRejectsRegisteredEngineOverlap(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	initTestWorkspaceMarker(t, ws)
	engineDir := filepath.Join(ws, "results", "custom-engine")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(engineDir, "server")
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	instPath := filepath.Join(root, "installation.json")
	decl, _ := json.Marshal(map[string]any{
		"executable":        exe,
		"working_directory": engineDir,
	})
	if err := os.WriteFile(instPath, decl, 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := New(context.Background(), Options{SettingsPath: filepath.Join(root, "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()
	svc.settingsMu.Lock()
	svc.settings.WorkspaceRoot = ptr(ws)
	svc.settings.EngineInstallations = []string{instPath}
	svc.settingsMu.Unlock()

	if _, apiErr := svc.CleanWorkspaceArtifacts(); apiErr == nil {
		t.Fatalf("expected error when results contains a registered local engine")
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("expected registered engine binary to remain intact: %v", err)
	}
}

func TestCleanWorkspaceArtifactsCleansExternalResultsWhenWorkspaceMissing(t *testing.T) {
	root, err := canonical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	missingWS := filepath.Join(root, "missing-workspace")
	extResults := filepath.Join(root, "external-results")
	modelsDir := filepath.Join(root, "models")
	enginesDir := filepath.Join(root, "engines")
	cliPath := filepath.Join(root, "inferyard")
	for _, d := range []string{filepath.Join(extResults, "run-1"), modelsDir, enginesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extResults, "run-1", "inputs.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := New(context.Background(), Options{
		Paths: &BoundPaths{
			CLI:       cliPath,
			Workspace: missingWS,
			Results:   extResults,
			Models:    modelsDir,
			Engines:   enginesDir,
			Settings:  filepath.Join(root, "settings.json"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Shutdown(context.Background()) }()

	res, apiErr := svc.CleanWorkspaceArtifacts()
	if apiErr != nil {
		t.Fatalf("CleanWorkspaceArtifacts failed: %+v", apiErr)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("expected 1 removed entry in external results, got %v", res.Removed)
	}
	if _, err := os.Stat(filepath.Join(extResults, "run-1")); !os.IsNotExist(err) {
		t.Fatalf("expected external run-1 to be removed, err=%v", err)
	}
	if st, err := os.Stat(extResults); err != nil || !st.IsDir() {
		t.Fatalf("expected external results root dir to remain, err=%v", err)
	}
}

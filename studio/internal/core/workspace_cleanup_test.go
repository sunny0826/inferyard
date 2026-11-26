package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanWorkspaceArtifactsRemovesArtifactsAndPreservesAssets(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	for _, dir := range []string{
		filepath.Join(ws, "candidates", "zh-core-1"),
		filepath.Join(ws, "bound", "zh-core-1"),
		filepath.Join(ws, "results", "run-1"),
		filepath.Join(ws, "reports", "summary-1"),
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
		filepath.Join(ws, "preparation.json"),
		filepath.Join(ws, "bundles", "zh-core.json"),
		filepath.Join(ws, "preflight", "device-1", "device-preflight.json"),
		filepath.Join(ws, "models", "qwen", "model.gguf"),
		filepath.Join(ws, "engines", "llama", "llama-server"),
		filepath.Join(ws, "candidates", "zh-core-1", "preparation.json"),
		filepath.Join(ws, "bound", "zh-core-1", "config.toml"),
		filepath.Join(ws, "results", "run-1", "inputs.json"),
		filepath.Join(ws, "reports", "summary-1", "studio.json"),
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

	res, apiErr := svc.CleanWorkspaceArtifacts()
	if apiErr != nil {
		t.Fatalf("CleanWorkspaceArtifacts failed: %+v", apiErr)
	}
	if len(res.Removed) != 5 {
		t.Fatalf("expected 5 removed entries, got %d: %v", len(res.Removed), res.Removed)
	}

	for _, gone := range []string{
		filepath.Join(ws, "candidates"),
		filepath.Join(ws, "bound"),
		filepath.Join(ws, "results"),
		filepath.Join(ws, "reports"),
		filepath.Join(ws, exclusivitySidecarName),
	} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", gone, err)
		}
	}
	for _, kept := range []string{
		filepath.Join(ws, "preparation.json"),
		filepath.Join(ws, "bundles", "zh-core.json"),
		filepath.Join(ws, "preflight", "device-1", "device-preflight.json"),
		filepath.Join(ws, "models", "qwen", "model.gguf"),
		filepath.Join(ws, "engines", "llama", "llama-server"),
	} {
		if _, err := os.Lstat(kept); err != nil {
			t.Fatalf("expected %s to be preserved, err=%v", kept, err)
		}
	}
}

func TestCleanWorkspaceArtifactsRejectsActiveRun(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
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
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
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

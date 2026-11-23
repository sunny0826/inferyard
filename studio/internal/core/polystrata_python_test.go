package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestPolystrataSetupExposesStudioUVDir(t *testing.T) {
	stubInstall(t)
	ws := t.TempDir()
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	rawSettings, _ := json.Marshal(map[string]any{
		"workspace_root": ws,
		"results_root":   filepath.Join(ws, "results"),
	})
	if err := os.WriteFile(settingsPath, rawSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	toolsDir := filepath.Join(settingsDir, "tools")
	if err := os.MkdirAll(toolsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	uvBin := filepath.Join(toolsDir, uvBinaryName())
	if err := os.WriteFile(uvBin, []byte("fake-uv"), 0o755); err != nil {
		t.Fatal(err)
	}

	installLookPath = func(string) (string, error) {
		return "", os.ErrNotExist
	}
	t.Setenv("PATH", t.TempDir())

	var seenExtraPaths []string
	var execDone sync.WaitGroup
	execDone.Add(1)
	s, err := New(t.Context(), Options{
		SettingsPath: settingsPath,
		Execute: func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
			if out, ok := version(args); ok {
				return out, nil
			}
			return inferyard.Execution{}, nil
		},
		ExecStream: func(ctx context.Context, onLine func(string), path string, args ...string) (inferyard.Execution, error) {
			defer execDone.Done()
			seenExtraPaths = inferyard.PrependedPaths(ctx)
			return inferyard.Execution{ExitCode: inferyard.ExitBlocked}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.settings.InferyardPath = ptr(os.Args[0])
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	if _, apiErr := s.CreatePolystrataSetup(PolystrataSetupRequest{Family: "qwen36"}); apiErr != nil {
		t.Fatalf("CreatePolystrataSetup failed: %+v", apiErr)
	}
	execDone.Wait()
	if !slices.Equal(seenExtraPaths, []string{toolsDir}) {
		t.Fatalf("expected studio tools dir %q in PrependedPaths, got %v", toolsDir, seenExtraPaths)
	}

	// When uv is already on PATH, no extra PATH prefix is needed
	pathDir := t.TempDir()
	pathUV := filepath.Join(pathDir, uvBinaryName())
	if err := os.WriteFile(pathUV, []byte("fake-uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return pathUV, nil
		}
		return "", os.ErrNotExist
	}
	t.Setenv("PATH", pathDir)
	if got := s.polystrataUVDir(); got != "" {
		t.Fatalf("expected empty polystrataUVDir when uv already on PATH, got %q", got)
	}
}

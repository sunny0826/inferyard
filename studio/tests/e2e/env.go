package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// Env is an isolated Studio environment rooted in temporary directories.
type Env struct {
	Workspace string
	Results   string
	Models    string
	Settings  string
	CLIPath   string
	Recorder  *Recorder
	Options   core.Options
}

// NewEnv builds isolated settings/host-state paths and wraps CLI access in a
// Recorder so every chain test produces a blame transcript by default.
func NewEnv(t *testing.T, cliPath string) *Env {
	t.Helper()
	root := t.TempDir()
	// INFERYARD_E2E_ROOT swaps the throwaway root for a caller-managed one so
	// the workspace and blame transcript survive for post-mortem inspection.
	if keep := os.Getenv("INFERYARD_E2E_ROOT"); keep != "" {
		root = keep
	}
	// The CLI resolves config paths with Path.resolve(); keep every fixture path
	// symlink-free so model_path comparisons see identical strings on macOS
	// (/var/folders is a symlink to /private/var/folders).
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	env := &Env{
		Workspace: filepath.Join(root, "workspace"),
		Results:   filepath.Join(root, "results"),
		Models:    filepath.Join(root, "models"),
		Settings:  filepath.Join(root, "settings.json"),
		CLIPath:   cliPath,
	}
	real := func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		return (inferyard.Client{Path: path}).Exec(ctx, args...)
	}
	env.Recorder = NewRecorder(t, filepath.Join(root, "transcript"), real)
	env.Options = core.Options{
		SettingsPath:       env.Settings,
		HostStatePath:      filepath.Join(root, "host-state.json"),
		Execute:            env.Recorder.Exec,
		EngineStartTimeout: 30 * time.Second,
	}
	return env
}

// repoRoot locates the repository root from this source file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve source path")
	}
	// studio/tests/e2e/env.go -> repo root is three levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// ResolveCLI finds a usable inferyard CLI: explicit override, PATH, then a
// uv wrapper against the in-repo cli/ project. Tests skip when none works.
func ResolveCLI(t *testing.T) string {
	t.Helper()
	if override := os.Getenv("INFERYARD_E2E_CLI"); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			t.Fatal(err)
		}
		return abs
	}
	if path, err := resolvePath("inferyard"); err == nil {
		return path
	}
	uv, err := resolvePath("uv")
	if err != nil {
		t.Skip("inferyard CLI not available (set INFERYARD_E2E_CLI)")
	}
	project := filepath.Join(repoRoot(t), "cli")
	if _, err := os.Stat(filepath.Join(project, "pyproject.toml")); err != nil {
		t.Skip("in-repo cli project not found")
	}
	wrapper := filepath.Join(t.TempDir(), "inferyard-e2e")
	script := "#!/bin/sh\nexec " + uv + " run --frozen --project " + shellQuote(project) + " inferyard \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func resolvePath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func shellQuote(s string) string {
	return "'" + s + "'"
}

package e2e

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type Env struct {
	Workspace string
	Results   string
	Models    string
	Settings  string
	CLIPath   string
	Recorder  *Recorder
	Options   core.Options
}

//go:embed cli_bootstrap.py
var cliBootstrap []byte

func NewEnv(t *testing.T, cliPath string) *Env {
	t.Helper()
	root := t.TempDir()
	if keep := os.Getenv("INFERYARD_E2E_ROOT"); keep != "" {
		root = keep
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	hostRoot, err := os.MkdirTemp(root, "host-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostRoot, "sitecustomize.py"), cliBootstrap, 0o600); err != nil {
		t.Fatal(err)
	}
	pythonPath := hostRoot
	if inherited := os.Getenv("PYTHONPATH"); inherited != "" {
		pythonPath += string(os.PathListSeparator) + inherited
	}
	t.Setenv("PYTHONPATH", pythonPath)
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
		HostStatePath:      filepath.Join(hostRoot, "host-state.json"),
		Execute:            env.Recorder.Exec,
		EngineStartTimeout: 30 * time.Second,
	}
	return env
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func ResolveCLI(t *testing.T) string {
	t.Helper()
	if override := os.Getenv("INFERYARD_E2E_CLI"); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			t.Fatal(err)
		}
		return abs
	}
	project := filepath.Join(repoRoot(t), "cli")
	for _, rel := range []string{
		filepath.Join(".venv", "bin", "inferyard"),
		filepath.Join(".venv", "Scripts", "inferyard.exe"),
	} {
		candidate := filepath.Join(project, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := resolvePath("inferyard"); err == nil {
		if _, helpErr := exec.Command(path, "config", "describe", "--help").CombinedOutput(); helpErr == nil {
			return path
		}
	}
	uv, err := resolvePath("uv")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("inferyard CLI not available (set INFERYARD_E2E_CLI or uv sync cli/)")
	}
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

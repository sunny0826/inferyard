//go:build !release

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
)

type devStatusReporter struct {
	path string
}

func (r devStatusReporter) report(state, errMsg string) {
	if r.path == "" {
		return
	}
	payload := map[string]any{"state": state, "pid": os.Getpid()}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	if err := atomicWriteJSONNoSymlink(r.path, payload); err != nil {
		log.Printf("dev status write failed: %v", err)
	}
}

func registerDevelopment(flags *flag.FlagSet, o *startupOptions) {
	flags.StringVar(&o.devProfile, "dev-profile", "", "Isolated development profile directory")
	flags.StringVar(&o.devAssets, "dev-assets", "", "Shared development assets directory for models and engines")
	flags.StringVar(&o.devCLI, "dev-cli", "", "Path to the checkout inferyard CLI executable")
	flags.StringVar(&o.devLabel, "dev-label", "", "Development label shown in window title and sidebar")
	flags.StringVar(&o.devStatus, "dev-status", "", "Status JSON path for launcher readiness and shutdown handoff")
}

func developmentBindings(o startupOptions) (*core.BoundPaths, mygo.WindowStateStore, devStatusReporter, error) {
	if o.devProfile == "" {
		return nil, nil, devStatusReporter{}, nil
	}
	profile, err := resolveDevPath(o.devProfile, true)
	if err != nil {
		return nil, nil, devStatusReporter{}, fmt.Errorf("invalid --dev-profile: %w", err)
	}
	assetsInput := o.devAssets
	if assetsInput == "" {
		assetsInput = filepath.Join(profile, "assets")
	}
	assets, err := resolveDevPath(assetsInput, true)
	if err != nil {
		return nil, nil, devStatusReporter{}, fmt.Errorf("invalid --dev-assets: %w", err)
	}
	cli, err := resolveDevPath(o.devCLI, false)
	if err != nil {
		return nil, nil, devStatusReporter{}, fmt.Errorf("invalid --dev-cli: %w", err)
	}
	var statusPath string
	if o.devStatus != "" {
		parent, err := resolveDevPath(filepath.Dir(o.devStatus), true)
		if err != nil {
			return nil, nil, devStatusReporter{}, fmt.Errorf("invalid --dev-status: %w", err)
		}
		statusPath = filepath.Join(parent, filepath.Base(o.devStatus))
	}
	models := filepath.Join(assets, "models")
	engines := filepath.Join(assets, "engines")
	workspace := filepath.Join(profile, "workspace")
	for _, dir := range []string{models, engines} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, devStatusReporter{}, err
		}
	}
	label := o.devLabel
	if label == "" {
		label = "dev"
	}
	paths := &core.BoundPaths{
		CLI:         cli,
		Workspace:   workspace,
		Results:     filepath.Join(workspace, "results"),
		Models:      models,
		Engines:     engines,
		Settings:    filepath.Join(profile, "settings.json"),
		Source:      "开发环境 · " + label,
		WindowTitle: "InferYard Studio · DEV · " + label,
		SourceLabel: label,
	}
	return paths, devWindowStates{filepath.Join(profile, "window-state.json")}, devStatusReporter{path: statusPath}, nil
}

func resolveDevPath(raw string, mkdir bool) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	if mkdir {
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", err
		}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func atomicWriteJSONNoSymlink(path string, value any) error {
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

type devWindowStates struct{ path string }

func (s devWindowStates) LoadWindowStates() (map[string]mygo.WindowState, error) {
	if st, err := os.Lstat(s.path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to read symlink window state: %s", s.path)
	}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var states map[string]mygo.WindowState
	return states, json.Unmarshal(raw, &states)
}

func (s devWindowStates) SaveWindowStates(states map[string]mygo.WindowState) error {
	return atomicWriteJSONNoSymlink(s.path, states)
}

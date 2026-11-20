//go:build !release

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
)

func registerDevelopment(flags *flag.FlagSet, o *startupOptions) {
	flags.StringVar(&o.devProfile, "dev-profile", "", "Isolated development profile directory")
	flags.StringVar(&o.devAssets, "dev-assets", "", "Shared development assets directory for models and engines")
	flags.StringVar(&o.devCLI, "dev-cli", "", "Path to the checkout inferyard CLI executable")
	flags.StringVar(&o.devLabel, "dev-label", "", "Development label shown in window title and sidebar")
}

func developmentBindings(o startupOptions) (*core.BoundPaths, mygo.WindowStateStore, error) {
	if o.devProfile == "" {
		return nil, nil, nil
	}
	profile, err := resolveDevPath(o.devProfile, true)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --dev-profile: %w", err)
	}
	assetsInput := o.devAssets
	if assetsInput == "" {
		assetsInput = filepath.Join(profile, "assets")
	}
	assets, err := resolveDevPath(assetsInput, true)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --dev-assets: %w", err)
	}
	cli, err := resolveDevPath(o.devCLI, false)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --dev-cli: %w", err)
	}
	models := filepath.Join(assets, "models")
	engines := filepath.Join(assets, "engines")
	workspace := filepath.Join(profile, "workspace")
	for _, dir := range []string{models, engines} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
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
	return paths, devWindowStates{filepath.Join(profile, "window-state.json")}, nil
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

type devWindowStates struct{ path string }

func (s devWindowStates) LoadWindowStates() (map[string]mygo.WindowState, error) {
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
	raw, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o600)
}

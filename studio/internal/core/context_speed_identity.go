package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Frozen labels are an index hint only. ReadContextSpeed authenticates via CLI.
func (s *Service) speedFrozenIdentity(batch string, task *ContextSpeedTask) {
	path, err := Resolve(batch, filepath.Join(batch, "frozen-plan", "config.json"))
	if err != nil {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var config struct {
		Model struct {
			Name string `json:"display_name"`
			Path string `json:"local_path"`
		} `json:"model"`
		Engine struct {
			Release string `json:"release"`
			Path    string `json:"binary_path"`
		} `json:"engine"`
		Conditions struct {
			Context int `json:"context_size"`
		} `json:"conditions"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return
	}
	task.DisplayModel, task.DisplayEngine = config.Model.Name, config.Engine.Release
	task.Context = config.Conditions.Context
	registry := s.registry()
	for _, entry := range registry.models {
		a, e := canonical(config.Model.Path)
		b, f := canonical(entry.path)
		if e == nil && f == nil && a == b {
			task.Model = entry.model.Name
			break
		}
	}
	for _, entry := range registry.engines {
		a, e := canonical(config.Engine.Path)
		b, f := canonical(entry.path)
		if e == nil && f == nil && a == b {
			task.Engine = entry.engine.Name
			break
		}
	}
}

func (s *Service) speedExecutionBoundary(t *runEntry) error {
	u := s.userSettings()
	w := workspaceRoot(u)
	if w == nil || u.ResultsRoot == nil || t.Task.ContextSpeed == nil || t.Task.ContextSpeed.Plan == nil {
		return os.ErrInvalid
	}
	root, err := Resolve(*u.ResultsRoot, t.root)
	if err != nil || root != t.root {
		return os.ErrInvalid
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		return os.ErrExist
	}
	plan, err := Resolve(*w, t.Task.ContextSpeed.Plan.Path)
	if err != nil || plan != t.Task.ContextSpeed.Plan.Path {
		return os.ErrInvalid
	}
	ref, e := readSpeedPlan(plan)
	if e != nil || ref.SHA256 != t.Task.ContextSpeed.Plan.SHA256 {
		return os.ErrInvalid
	}
	return nil
}

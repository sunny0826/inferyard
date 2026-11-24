package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type DirectoryReadiness struct {
	Path        *string `json:"path"`
	Configured  bool    `json:"configured"`
	Exists      bool    `json:"exists"`
	Initialized bool    `json:"initialized"`
}
type Readiness struct {
	CLI       InferyardProbe     `json:"cli"`
	Workspace DirectoryReadiness `json:"workspace"`
	Results   DirectoryReadiness `json:"results"`
	Models    DirectoryReadiness `json:"models"`
}

func inferyardBaseDir() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".inferyard"), true
}

func workspaceRoot(u UserSettings) *string {
	if u.WorkspaceRoot != nil {
		return u.WorkspaceRoot
	}
	base, ok := inferyardBaseDir()
	if !ok {
		return nil
	}
	return ptr(filepath.Join(base, "workspace"))
}

func defaultResultsRoot(u UserSettings) *string {
	if u.ResultsRoot != nil {
		return u.ResultsRoot
	}
	if base, ok := inferyardBaseDir(); ok {
		return ptr(filepath.Join(base, "results"))
	}
	if w := workspaceRoot(u); w != nil {
		return ptr(filepath.Join(*w, "results"))
	}
	return nil
}

func defaultModelsRoot(u UserSettings) *string {
	if u.ModelsRoot != nil {
		return u.ModelsRoot
	}
	if base, ok := inferyardBaseDir(); ok {
		return ptr(filepath.Join(base, "models"))
	}
	if w := workspaceRoot(u); w != nil {
		return ptr(filepath.Join(*w, "models"))
	}
	return nil
}
func directoryReadiness(path *string, configured bool) DirectoryReadiness {
	r := DirectoryReadiness{Path: path, Configured: configured}
	if path != nil {
		st, err := os.Stat(*path)
		r.Exists = err == nil && st.IsDir()
	}
	return r
}
func (s *Service) Readiness() Readiness {
	u := s.userSettings()
	w := directoryReadiness(workspaceRoot(u), u.WorkspaceRoot != nil)
	if w.Exists {
		raw, err := os.ReadFile(filepath.Join(*w.Path, "preparation.json"))
		var prep struct {
			Kind string `json:"kind"`
		}
		w.Initialized = err == nil && json.Unmarshal(raw, &prep) == nil && prep.Kind == "community_init.v1"
	}
	return Readiness{CLI: s.probe(u), Workspace: w, Results: directoryReadiness(u.ResultsRoot, u.ResultsRoot != nil), Models: directoryReadiness(u.ModelsRoot, u.ModelsRoot != nil)}
}

func (s *Service) FirstRun() bool {
	return firstRun(s.userSettings())
}

func firstRun(u UserSettings) bool {
	return u.WorkspaceRoot == nil && u.ResultsRoot == nil && u.ModelsRoot == nil
}
func (s *Service) Provision() (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	u.WorkspaceRoot = workspaceRoot(u)
	if u.WorkspaceRoot == nil {
		return SettingsResponse{}, Err("internal_error")
	}
	u.ResultsRoot = defaultResultsRoot(u)
	if u.ResultsRoot == nil {
		return SettingsResponse{}, Err("internal_error")
	}
	u.ModelsRoot = defaultModelsRoot(u)
	if u.ModelsRoot == nil {
		return SettingsResponse{}, Err("internal_error")
	}
	for _, path := range []string{filepath.Dir(*u.WorkspaceRoot), *u.ResultsRoot, *u.ModelsRoot} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return SettingsResponse{}, Err("internal_error")
		}
	}
	result, err := s.saveSettings(u)
	if err == nil {
		s.Bus.Publish("sync.reset", map[string]string{"reason": "settings_changed"})
	}
	return result, err
}

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
	FirstRun  bool               `json:"first_run"`
}

func workspaceRoot(u UserSettings) *string {
	if u.WorkspaceRoot != nil {
		return u.WorkspaceRoot
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return ptr(filepath.Join(home, ".inferyard"))
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
	return Readiness{CLI: s.probe(u), Workspace: w, Results: directoryReadiness(u.ResultsRoot, u.ResultsRoot != nil), Models: directoryReadiness(u.ModelsRoot, u.ModelsRoot != nil), FirstRun: u.WorkspaceRoot == nil && u.ResultsRoot == nil && u.ModelsRoot == nil}
}
// NeedsOnboarding reports a first-run startup without touching the CLI binary:
// settings with no configured CLI path open the wizard window only, so the app
// does not start with two windows at once.
func (s *Service) NeedsOnboarding() bool {
	u := s.userSettings()
	return str(u.InferyardPath) == ""
}
func (s *Service) Provision() (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	u.WorkspaceRoot = workspaceRoot(u)
	if u.WorkspaceRoot == nil {
		return SettingsResponse{}, Err("internal_error")
	}
	if u.ResultsRoot == nil {
		u.ResultsRoot = ptr(filepath.Join(*u.WorkspaceRoot, "results"))
	}
	if u.ModelsRoot == nil {
		u.ModelsRoot = ptr(filepath.Join(*u.WorkspaceRoot, "models"))
	}
	for _, path := range []*string{u.WorkspaceRoot, u.ResultsRoot, u.ModelsRoot} {
		if err := os.MkdirAll(*path, 0700); err != nil {
			return SettingsResponse{}, Err("internal_error")
		}
	}
	result, err := s.saveSettings(u)
	if err == nil {
		s.Bus.Publish("sync.reset", map[string]string{"reason": "settings_changed"})
	}
	return result, err
}

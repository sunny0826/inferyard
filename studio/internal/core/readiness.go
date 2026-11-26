package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func derivedResultsRoot(u UserSettings, paths *BoundPaths) *string {
	if paths != nil && strings.TrimSpace(paths.Results) != "" {
		return ptr(filepath.Clean(paths.Results))
	}
	if u.WorkspaceRoot == nil || strings.TrimSpace(*u.WorkspaceRoot) == "" {
		return nil
	}
	return ptr(filepath.Join(filepath.Clean(*u.WorkspaceRoot), "results"))
}

func derivedModelsRoot(u UserSettings, paths *BoundPaths) *string {
	if paths != nil && strings.TrimSpace(paths.Models) != "" {
		return ptr(filepath.Clean(paths.Models))
	}
	if u.WorkspaceRoot == nil || strings.TrimSpace(*u.WorkspaceRoot) == "" {
		return nil
	}
	return ptr(filepath.Join(filepath.Clean(*u.WorkspaceRoot), "models"))
}

func derivedReportsRoot(u UserSettings, paths *BoundPaths) *string {
	if paths != nil && strings.TrimSpace(paths.Workspace) != "" {
		return ptr(filepath.Join(filepath.Clean(paths.Workspace), "reports"))
	}
	if u.WorkspaceRoot == nil || strings.TrimSpace(*u.WorkspaceRoot) == "" {
		return nil
	}
	return ptr(filepath.Join(filepath.Clean(*u.WorkspaceRoot), "reports"))
}

func (s *Service) resultsRoot(u UserSettings) *string { return derivedResultsRoot(u, s.paths) }
func (s *Service) modelsRoot(u UserSettings) *string  { return derivedModelsRoot(u, s.paths) }
func (s *Service) reportsRoot(u UserSettings) *string { return derivedReportsRoot(u, s.paths) }

func directoryReadiness(path *string, configured bool) DirectoryReadiness {
	r := DirectoryReadiness{Path: path, Configured: configured}
	if path != nil {
		st, err := os.Stat(*path)
		r.Exists = err == nil && st.IsDir()
	}
	return r
}
func isInitializedWorkspace(wsPath string) bool {
	if strings.TrimSpace(wsPath) == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(wsPath, "preparation.json"))
	if err != nil {
		return false
	}
	var prep struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal(raw, &prep) == nil && prep.Kind == "community_init.v1"
}

func (s *Service) Readiness() Readiness {
	u := s.userSettings()
	w := directoryReadiness(workspaceRoot(u), u.WorkspaceRoot != nil)
	if w.Exists {
		w.Initialized = isInitializedWorkspace(*w.Path)
	}
	results := s.resultsRoot(u)
	models := s.modelsRoot(u)
	return Readiness{
		CLI:       s.probe(u),
		Workspace: w,
		Results:   directoryReadiness(results, results != nil),
		Models:    directoryReadiness(models, models != nil),
	}
}

func (s *Service) FirstRun() bool {
	return firstRun(s.userSettings())
}

func firstRun(u UserSettings) bool {
	return u.WorkspaceRoot == nil
}
func (s *Service) Provision() (SettingsResponse, *ApiError) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	if s.paths == nil {
		u.WorkspaceRoot = workspaceRoot(u)
		if u.WorkspaceRoot == nil {
			return SettingsResponse{}, Err("internal_error")
		}
	}
	results := s.resultsRoot(u)
	models := s.modelsRoot(u)
	if results == nil || models == nil {
		return SettingsResponse{}, Err("internal_error")
	}
	dirs := []string{*results, *models}
	if engines := workspaceEnginesRoot(u, s.paths); engines != "" {
		dirs = append(dirs, engines)
	}
	for _, path := range dirs {
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

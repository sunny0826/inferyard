package core

import (
	"os"
	"path/filepath"
	"strings"
)

type PreparationLocations struct {
	ToolsDir                   string
	ManagedPython              string
	Workspace, Results, Models *string
	CLI                        *string
	UV                         *string
	Bound                      bool
}

func (s *Service) PreparationLocations() PreparationLocations {
	u := s.userSettings()
	if s.paths != nil {
		return PreparationLocations{
			Workspace: ptr(s.paths.Workspace), Results: ptr(s.paths.Results), Models: ptr(s.paths.Models),
			CLI: ptr(s.paths.CLI), ManagedPython: installPython, Bound: true,
		}
	}
	tools := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	l := PreparationLocations{
		ToolsDir:      tools,
		ManagedPython: installPython,
		Workspace:     workspaceRoot(u),
		Results:       u.ResultsRoot,
		Models:        u.ModelsRoot,
	}
	if existing := strings.TrimSpace(str(u.InferyardPath)); existing != "" {
		if st, err := os.Stat(existing); err == nil && !st.IsDir() {
			l.CLI = ptr(existing)
		}
	}
	if l.CLI == nil {
		if found := FindCLI(); found != "" {
			l.CLI = ptr(found)
		}
	}
	if uv := FindUV(tools); uv != "" {
		l.UV = ptr(uv)
	}
	return l
}

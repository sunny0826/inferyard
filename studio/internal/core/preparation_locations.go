package core

import (
	"context"
	"path/filepath"
)

type PreparationLocations struct {
	ToolsDir                   string
	ManagedPython              string
	Workspace, Results, Models *string
	CLI                        *string
	CLIStatus                  string
	CLIVersion                 *string
	UV                         *string
	Bound                      bool
}

func (s *Service) PreparationLocations() PreparationLocations {
	u := s.userSettings()
	if s.paths != nil {
		p := s.probe(u)
		return PreparationLocations{
			Workspace: ptr(s.paths.Workspace), Results: ptr(s.paths.Results), Models: ptr(s.paths.Models),
			CLI: ptr(s.paths.CLI), CLIStatus: p.Status, CLIVersion: p.Version, ManagedPython: installPython, Bound: true,
		}
	}
	tools := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	l := PreparationLocations{
		ToolsDir:      tools,
		ManagedPython: installPython,
		Workspace:     workspaceRoot(u),
		Results:       defaultResultsRoot(u),
		Models:        defaultModelsRoot(u),
	}
	reusable, outdated := s.inspectCLIForInstall(u)
	if reusable != "" {
		l.CLI = ptr(reusable)
		l.CLIStatus = "ok"
	} else if outdated != nil {
		l.CLI = outdated.Path
		l.CLIStatus = "outdated"
		l.CLIVersion = outdated.Version
	}
	if uv, _ := findCompatibleUV(context.Background(), tools); uv != "" {
		l.UV = ptr(uv)
	}
	return l
}

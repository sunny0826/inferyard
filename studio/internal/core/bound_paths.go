package core

import (
	"errors"
	"path/filepath"
)

// BoundPaths is an immutable launch policy, separate from editable user settings.
type BoundPaths struct {
	CLI, Workspace, Results, Models, Engines, Settings string
	Source, WindowTitle, SourceLabel                   string
}

func (p *BoundPaths) validate() error {
	for _, path := range []string{p.CLI, p.Workspace, p.Results, p.Models, p.Engines, p.Settings} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("bound paths must be absolute and clean")
		}
		resolved, err := canonical(path)
		if err != nil || resolved != path {
			return errors.New("bound paths must be canonical")
		}
	}
	return nil
}

func (p *BoundPaths) bind(u UserSettings) UserSettings {
	u.InferyardPath, u.WorkspaceRoot = ptr(p.CLI), ptr(p.Workspace)
	u.ResultsRoot, u.ModelsRoot = ptr(p.Results), ptr(p.Models)
	return u
}

func (p *BoundPaths) matches(u UserSettings) bool {
	return str(u.InferyardPath) == p.CLI && str(u.WorkspaceRoot) == p.Workspace && str(u.ResultsRoot) == p.Results && str(u.ModelsRoot) == p.Models
}

func (s *Service) BoundPaths() *BoundPaths {
	if s.paths == nil {
		return nil
	}
	p := *s.paths
	return &p
}

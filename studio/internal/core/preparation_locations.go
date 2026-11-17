package core

import (
	"os"
	"path/filepath"
	"strings"
)

// PreparationLocations is a read-only projection of where the preparation step
// writes. It reads settings and the filesystem only; it never installs,
// downloads or creates anything. Sizes and timings are deliberately absent:
// they depend on components already present on the machine.
type PreparationLocations struct {
	// ToolsDir receives a standalone uv build when none is found.
	ToolsDir string
	// ManagedPython is the Python build the CLI install requires.
	ManagedPython string
	// Workspace, Results and Models are the configured targets.
	Workspace, Results, Models *string
	// CLI is the configured or discovered executable, when one already exists.
	CLI *string
	// UV is the standalone uv build already present in ToolsDir.
	UV *string
	// Bound marks the development environment, whose paths are fixed.
	Bound bool
}

// PreparationLocations reports the current targets without side effects.
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

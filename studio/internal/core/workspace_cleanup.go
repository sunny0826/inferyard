package core

import (
	"os"
	"path/filepath"
	"strings"
)

var workspaceArtifactDirs = []string{"candidates", "bound", "results", "reports"}

type WorkspaceCleanupResult struct {
	Removed []string `json:"removed"`
}

func (s *Service) CleanWorkspaceArtifacts() (WorkspaceCleanupResult, *ApiError) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return WorkspaceCleanupResult{}, Err("service_closing")
	}
	if s.runsActiveLocked() || s.deletingAssets > 0 {
		s.mu.Unlock()
		return WorkspaceCleanupResult{}, Err("run_active")
	}
	s.deletingAssets++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.deletingAssets--
		s.mu.Unlock()
	}()

	u := s.userSettings()
	paths := s.BoundPaths()
	ws := workspaceRoot(u)
	if paths != nil && strings.TrimSpace(paths.Workspace) != "" {
		ws = ptr(paths.Workspace)
	}
	if ws == nil || strings.TrimSpace(*ws) == "" {
		return WorkspaceCleanupResult{}, Err("settings_incomplete")
	}
	wsPath := filepath.Clean(strings.TrimSpace(*ws))
	if isSymlinkPath(wsPath) {
		return WorkspaceCleanupResult{}, Err("io_error")
	}
	info, err := os.Lstat(wsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return WorkspaceCleanupResult{Removed: []string{}}, nil
		}
		return WorkspaceCleanupResult{}, Err("io_error")
	}
	if !info.IsDir() {
		return WorkspaceCleanupResult{}, Err("io_error")
	}
	canonWS, err := canonical(wsPath)
	if err != nil || !filepath.IsAbs(canonWS) {
		return WorkspaceCleanupResult{}, Err("io_error")
	}

	protectedRoots := canonicalProtectedRoots(u, paths)

	type cleanupTarget struct {
		path         string
		contentsOnly bool
	}
	targets := make([]cleanupTarget, 0, len(workspaceArtifactDirs)+2)
	seen := map[string]bool{}

	for _, name := range workspaceArtifactDirs {
		target := filepath.Join(wsPath, name)
		st, statErr := os.Lstat(target)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil || st.Mode()&os.ModeSymlink != 0 {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		canonTarget, cErr := canonical(target)
		if cErr != nil || canonTarget == canonWS || !withinCanonicalDir(canonWS, canonTarget) {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		if overlapsAnyRoot(canonTarget, protectedRoots) {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		seen[canonTarget] = true
		targets = append(targets, cleanupTarget{path: target})
	}

	sidecar := filepath.Join(wsPath, exclusivitySidecarName)
	if st, statErr := os.Lstat(sidecar); statErr == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		canonSidecar, cErr := canonical(sidecar)
		if cErr != nil || canonSidecar == canonWS || !withinCanonicalDir(canonWS, canonSidecar) {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		targets = append(targets, cleanupTarget{path: sidecar})
	} else if !os.IsNotExist(statErr) {
		return WorkspaceCleanupResult{}, Err("io_error")
	}

	if extResults := s.resultsRoot(u); extResults != nil && strings.TrimSpace(*extResults) != "" {
		resPath := filepath.Clean(strings.TrimSpace(*extResults))
		if !isSymlinkPath(resPath) {
			if st, statErr := os.Lstat(resPath); statErr == nil && st.IsDir() {
				canonRes, cErr := canonical(resPath)
				if cErr != nil || !filepath.IsAbs(canonRes) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				if !seen[canonRes] && canonRes != canonWS {
					if overlapsAnyRoot(canonRes, protectedRoots) {
						return WorkspaceCleanupResult{}, Err("io_error")
					}
					targets = append(targets, cleanupTarget{path: resPath, contentsOnly: true})
				}
			} else if statErr != nil && !os.IsNotExist(statErr) {
				return WorkspaceCleanupResult{}, Err("io_error")
			}
		} else {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
	}

	removed := []string{}
	for _, t := range targets {
		if t.contentsOnly {
			entries, readErr := os.ReadDir(t.path)
			if readErr != nil {
				return WorkspaceCleanupResult{}, Err("io_error")
			}
			for _, entry := range entries {
				child := filepath.Join(t.path, entry.Name())
				if isSymlinkPath(child) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				if rmErr := os.RemoveAll(child); rmErr != nil {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				removed = append(removed, child)
			}
			continue
		}
		if rmErr := os.RemoveAll(t.path); rmErr != nil {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		removed = append(removed, t.path)
	}

	s.caches.clearAll()
	s.mu.Lock()
	s.history = RunHistoryResponse{}
	s.candidateOrigins = map[string]localCandidateOrigin{}
	s.mu.Unlock()

	return WorkspaceCleanupResult{Removed: removed}, nil
}

func canonicalProtectedRoots(u UserSettings, paths *BoundPaths) []string {
	raw := activeModelRoots(u, paths)
	if eng := workspaceEnginesRoot(u, paths); eng != "" {
		raw = append(raw, eng)
	}
	roots := make([]string, 0, len(raw))
	for _, r := range raw {
		if strings.TrimSpace(r) == "" {
			continue
		}
		if c, err := canonical(r); err == nil && filepath.IsAbs(c) {
			roots = append(roots, c)
		}
	}
	return roots
}

func overlapsAnyRoot(canonTarget string, roots []string) bool {
	for _, root := range roots {
		if canonTarget == root || withinCanonicalDir(root, canonTarget) || withinCanonicalDir(canonTarget, root) {
			return true
		}
	}
	return false
}

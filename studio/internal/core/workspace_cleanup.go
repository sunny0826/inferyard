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
	if s.orphan != nil || len(s.orphanedServices) > 0 || len(s.preparingServices) > 0 || s.deletingAssets > 0 || s.runsActiveLocked() {
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
	extResults := s.resultsRoot(u)
	hasWS := ws != nil && strings.TrimSpace(*ws) != ""
	hasExtResults := extResults != nil && strings.TrimSpace(*extResults) != ""
	if !hasWS && !hasExtResults {
		return WorkspaceCleanupResult{}, Err("settings_incomplete")
	}

	protectedRoots := canonicalProtectedRoots(u, paths)

	type cleanupTarget struct {
		path         string
		contentsOnly bool
	}
	targets := make([]cleanupTarget, 0, len(workspaceArtifactDirs)+3)
	seen := []string{}
	markSeen := func(canonPath string) bool {
		for _, prev := range seen {
			if sameFileOrPath(prev, canonPath) {
				return false
			}
		}
		seen = append(seen, canonPath)
		return true
	}

	var canonWS string
	wsPresent := false
	if hasWS {
		wsPath := filepath.Clean(strings.TrimSpace(*ws))
		if isSymlinkPath(wsPath) {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		info, err := os.Lstat(wsPath)
		if err != nil && !os.IsNotExist(err) {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		if err == nil {
			if !info.IsDir() {
				return WorkspaceCleanupResult{}, Err("io_error")
			}
			if !isInitializedWorkspace(wsPath) {
				return WorkspaceCleanupResult{}, Err("settings_incomplete")
			}
			var cErr error
			canonWS, cErr = canonical(wsPath)
			if cErr != nil || !filepath.IsAbs(canonWS) {
				return WorkspaceCleanupResult{}, Err("io_error")
			}
			wsPresent = true

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
				if cErr != nil || sameFileOrPath(canonTarget, canonWS) || !pathWithinDirSameFS(canonWS, canonTarget) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				if overlapsAnyRoot(canonTarget, protectedRoots) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				if markSeen(canonTarget) {
					targets = append(targets, cleanupTarget{
						path:         target,
						contentsOnly: st.IsDir(),
					})
				}
			}

			speedIndexDir := filepath.Join(wsPath, "logs", "context-speed-tasks")
			if st, statErr := os.Lstat(speedIndexDir); statErr == nil {
				if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				canonSpeed, cErr := canonical(speedIndexDir)
				if cErr != nil || sameFileOrPath(canonSpeed, canonWS) || !pathWithinDirSameFS(canonWS, canonSpeed) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				if overlapsAnyRoot(canonSpeed, protectedRoots) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				if markSeen(canonSpeed) {
					targets = append(targets, cleanupTarget{path: speedIndexDir})
				}
			} else if !os.IsNotExist(statErr) {
				return WorkspaceCleanupResult{}, Err("io_error")
			}

			sidecar := filepath.Join(wsPath, exclusivitySidecarName)
			if st, statErr := os.Lstat(sidecar); statErr == nil {
				if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				canonSidecar, cErr := canonical(sidecar)
				if cErr != nil || sameFileOrPath(canonSidecar, canonWS) || !pathWithinDirSameFS(canonWS, canonSidecar) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				targets = append(targets, cleanupTarget{path: sidecar})
			} else if !os.IsNotExist(statErr) {
				return WorkspaceCleanupResult{}, Err("io_error")
			}
		}
	}

	if hasExtResults {
		resPath := filepath.Clean(strings.TrimSpace(*extResults))
		if isSymlinkPath(resPath) {
			return WorkspaceCleanupResult{}, Err("io_error")
		}
		if st, statErr := os.Lstat(resPath); statErr == nil && st.IsDir() {
			canonRes, cErr := canonical(resPath)
			if cErr != nil || !filepath.IsAbs(canonRes) {
				return WorkspaceCleanupResult{}, Err("io_error")
			}
			if (!wsPresent || !sameFileOrPath(canonRes, canonWS)) && markSeen(canonRes) {
				if overlapsAnyRoot(canonRes, protectedRoots) {
					return WorkspaceCleanupResult{}, Err("io_error")
				}
				targets = append(targets, cleanupTarget{path: resPath, contentsOnly: true})
			}
		} else if statErr != nil && !os.IsNotExist(statErr) {
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
	keptRuns := s.runs[:0]
	for _, r := range s.runs {
		if r != nil && !terminal(r.Task.State) {
			keptRuns = append(keptRuns, r)
		}
	}
	s.runs = keptRuns
	s.mu.Unlock()

	return WorkspaceCleanupResult{Removed: removed}, nil
}

func canonicalProtectedRoots(u UserSettings, paths *BoundPaths) []string {
	wsEngines := workspaceEnginesRoot(u, paths)
	raw := activeModelRoots(u, paths)
	if wsEngines != "" {
		raw = append(raw, wsEngines)
	}
	raw = append(raw, registeredInstallationProtectedPaths(u, paths, wsEngines)...)
	roots := make([]string, 0, len(raw))
	for _, r := range raw {
		if strings.TrimSpace(r) == "" {
			continue
		}
		resolved := filepath.Clean(r)
		if c, err := canonical(r); err == nil && filepath.IsAbs(c) {
			resolved = c
		}
		if !filepath.IsAbs(resolved) {
			continue
		}
		dup := false
		for _, prev := range roots {
			if sameFileOrPath(prev, resolved) {
				dup = true
				break
			}
		}
		if !dup {
			roots = append(roots, resolved)
		}
	}
	return roots
}

func overlapsAnyRoot(canonTarget string, roots []string) bool {
	for _, root := range roots {
		if sameFileOrPath(canonTarget, root) || pathWithinDirSameFS(root, canonTarget) || pathWithinDirSameFS(canonTarget, root) {
			return true
		}
	}
	return false
}

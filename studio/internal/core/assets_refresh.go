package core

import (
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
)

type AssetsState struct {
	Snapshot         json.RawMessage      `json:"snapshot"`
	Parsed           *inferyard.AssetScan `json:"-"`
	CapturedAt       *string              `json:"captured_at"`
	AssetsRefreshing bool                 `json:"refreshing"`
	AssetsError      *ApiError            `json:"last_error"`
}

func cloneAssetsState(a AssetsState) AssetsState {
	out := AssetsState{
		Snapshot:         append(json.RawMessage(nil), a.Snapshot...),
		AssetsRefreshing: a.AssetsRefreshing,
		AssetsError:      clone(a.AssetsError),
	}
	if a.CapturedAt != nil {
		out.CapturedAt = ptr(*a.CapturedAt)
	}
	if a.Parsed != nil {
		copied := clone(*a.Parsed)
		out.Parsed = &copied
	}
	return out
}

func (s *Service) Assets() AssetsState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAssetsState(s.assets)
}

func (s *Service) RefreshAssets() *ApiError {
	s.mu.Lock()
	if s.assets.AssetsRefreshing {
		s.mu.Unlock()
		return Conflict("refresh_in_progress")
	}
	u := clone(s.settings)
	roots := allModelsRoots(u)
	if len(roots) == 0 {
		s.mu.Unlock()
		return Conflict("settings_incomplete")
	}
	s.assets.AssetsRefreshing = true
	gen := s.assetsGen
	s.mu.Unlock()
	go func() {
		p := s.probe(u)
		var failure *ApiError
		var snapshot json.RawMessage
		var parsed *inferyard.AssetScan
		outdated := false
		if p.Status != "ok" {
			failure = Err("cli_unavailable")
		} else {
			scanArgs := append([]string{"assets", "scan"}, modelsRootCLIArgs(roots)...)
			out, e := s.exec(s.ctx, *p.Path, scanArgs...)
			if e != nil {
				if errors.Is(e, os.ErrNotExist) || errors.Is(e, exec.ErrNotFound) {
					outdated = true
				} else {
					failure = Err("internal_error")
				}
			} else if out.ExitCode == inferyard.ExitBlocked {
				r, parseErr := inferyard.Parse(out.Stdout)
				if parseErr != nil || r.Command != "assets scan" {
					outdated = true
				} else if scan, scanErr := inferyard.ParseAssetScan(r.Details); scanErr != nil {
					outdated = true
				} else {
					snapshot = r.Details
					parsed = &scan
				}
			} else if out.ExitCode != 0 {
				failure = exitError(out, nil)
				if failure == nil {
					failure = Err("cli_error")
				}
			} else {
				r, parseErr := inferyard.Parse(out.Stdout)
				if parseErr != nil || r.Command != "assets scan" {
					outdated = true
				} else if scan, scanErr := inferyard.ParseAssetScan(r.Details); scanErr != nil {
					failure = Err("internal_error")
				} else {
					snapshot = r.Details
					parsed = &scan
				}
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if gen != s.assetsGen {
			return
		}
		s.assets.AssetsRefreshing = false
		if outdated {
			s.assets.Snapshot = nil
			s.assets.Parsed = nil
			s.assets.CapturedAt = nil
			s.assets.AssetsError = Err("cli_outdated")
		} else if failure == nil {
			s.assets.Snapshot = snapshot
			s.assets.Parsed = parsed
			s.assets.CapturedAt = ptr(now())
			s.assets.AssetsError = nil
		} else {
			s.assets.AssetsError = failure
		}
		s.Bus.Publish("assets.updated", map[string]any{"snapshot": s.assets.Snapshot, "captured_at": s.assets.CapturedAt, "error": s.assets.AssetsError})
	}()
	return nil
}

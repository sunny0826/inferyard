package core

import (
	"context"
	"strings"
)

const (
	CLIUpgradeIdle      = "idle"
	CLIUpgradeChecking  = "checking"
	CLIUpgradeUpgrading = "upgrading"
	CLIUpgradeFailed    = "failed"
)

type CLIUpgradeState struct {
	Seq           uint64    `json:"seq,omitempty"`
	State         string    `json:"state"`
	TargetVersion string    `json:"target_version,omitempty"`
	Message       string    `json:"message,omitempty"`
	Error         *ApiError `json:"error,omitempty"`
}

func cloneApiError(e *ApiError) *ApiError {
	if e == nil {
		return nil
	}
	cp := clone(*e)
	return &cp
}

func (s *Service) cloneCLIUpgradeLocked() CLIUpgradeState {
	out := s.cliUpgrade
	if out.State == "" {
		out.State = CLIUpgradeIdle
	}
	if out.Error != nil {
		out.Error = cloneApiError(out.Error)
	}
	return out
}

func (s *Service) publishCLIUpgradeLocked() {
	s.cliUpgrade.Seq++
	if s.Bus == nil {
		return
	}
	snap := s.cloneCLIUpgradeLocked()
	_ = s.Bus.Publish("cli.upgrade", map[string]any{"cli_upgrade": snap})
}

func (s *Service) CLIUpgradeStatus() CLIUpgradeState {
	if s == nil {
		return CLIUpgradeState{State: CLIUpgradeIdle}
	}
	s.cliUpgradeMu.Lock()
	defer s.cliUpgradeMu.Unlock()
	return s.cloneCLIUpgradeLocked()
}

func (s *Service) clearCLIUpgradeState() {
	if s == nil {
		return
	}
	s.cliUpgradeMu.Lock()
	defer s.cliUpgradeMu.Unlock()
	if s.cliUpgrade.State == "" || (s.cliUpgrade.State == CLIUpgradeIdle && s.cliUpgrade.Error == nil && s.cliUpgrade.Message == "" && s.cliUpgrade.TargetVersion == "") {
		return
	}
	s.cliUpgrade.State = CLIUpgradeIdle
	s.cliUpgrade.TargetVersion = ""
	s.cliUpgrade.Message = ""
	s.cliUpgrade.Error = nil
	s.publishCLIUpgradeLocked()
}

func (s *Service) StartAutoCLIUpgrade() {
	if s == nil || s.paths != nil {
		return
	}
	targetVersion := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if _, ok := parseSemver(targetVersion); !ok {
		return
	}
	s.cliUpgradeOnce.Do(func() {
		s.cliUpgradeMu.Lock()
		if s.cliUpgrade.State == "" || s.cliUpgrade.State == CLIUpgradeIdle {
			s.cliUpgrade.State = CLIUpgradeChecking
			s.cliUpgrade.TargetVersion = targetVersion
			s.cliUpgrade.Message = ""
			s.cliUpgrade.Error = nil
			s.publishCLIUpgradeLocked()
		}
		s.cliUpgradeMu.Unlock()
		go func() {
			if s.ctx.Err() != nil {
				s.clearCLIUpgradeState()
				return
			}
			p := s.probe(s.userSettings())
			if p.Status != "outdated" {
				s.clearCLIUpgradeState()
				return
			}
			_, _ = s.UpgradeCLI(s.ctx, nil)
		}()
	})
}

func (s *Service) UpgradeCLI(ctx context.Context, onStatus func(string)) (string, *ApiError) {
	if ctx == nil {
		if s.ctx != nil {
			ctx = s.ctx
		} else {
			ctx = context.Background()
		}
	}
	s.cliInstallMu.Lock()
	defer s.cliInstallMu.Unlock()

	if p := s.probe(s.userSettings()); p.Status == "ok" && p.Path != nil {
		s.clearCLIUpgradeState()
		return *p.Path, nil
	}

	targetVersion := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if _, ok := parseSemver(targetVersion); !ok {
		targetVersion = ""
	}
	s.cliUpgradeMu.Lock()
	s.cliUpgrade.State = CLIUpgradeUpgrading
	s.cliUpgrade.TargetVersion = targetVersion
	s.cliUpgrade.Message = ""
	s.cliUpgrade.Error = nil
	s.publishCLIUpgradeLocked()
	s.cliUpgradeMu.Unlock()

	path, apiErr := s.installCLILocked(ctx, func(message string) {
		s.cliUpgradeMu.Lock()
		if s.cliUpgrade.State == CLIUpgradeUpgrading {
			s.cliUpgrade.Message = message
			s.publishCLIUpgradeLocked()
		}
		s.cliUpgradeMu.Unlock()
		if onStatus != nil {
			onStatus(message)
		}
	})
	if apiErr != nil {
		if ctx.Err() != nil {
			s.clearCLIUpgradeState()
			return "", apiErr
		}
		s.cliUpgradeMu.Lock()
		s.cliUpgrade.State = CLIUpgradeFailed
		s.cliUpgrade.TargetVersion = targetVersion
		s.cliUpgrade.Message = ""
		s.cliUpgrade.Error = cloneApiError(apiErr)
		s.publishCLIUpgradeLocked()
		s.cliUpgradeMu.Unlock()
		return "", apiErr
	}

	s.clearCLIUpgradeState()
	if s.Bus != nil {
		_ = s.Bus.Publish("sync.reset", map[string]string{"reason": "cli_upgraded"})
	}
	return path, nil
}

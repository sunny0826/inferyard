package core

import (
	"context"
	"strings"
	"time"
)

var autoCLIUpgradeRetryDelays = []time.Duration{
	3 * time.Second,
	6 * time.Second,
	12 * time.Second,
}

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
	cancel := s.cliUpgradeCancel
	s.cliUpgradeCancel = nil
	unchanged := s.cliUpgrade.State == "" ||
		(s.cliUpgrade.State == CLIUpgradeIdle && s.cliUpgrade.Error == nil && s.cliUpgrade.Message == "" && s.cliUpgrade.TargetVersion == "")
	if !unchanged {
		s.cliUpgrade.State = CLIUpgradeIdle
		s.cliUpgrade.TargetVersion = ""
		s.cliUpgrade.Message = ""
		s.cliUpgrade.Error = nil
		s.publishCLIUpgradeLocked()
	}
	s.cliUpgradeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) warmCompatibleCLI(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = s.ctx
	}
	if ctx == nil || ctx.Err() != nil {
		return
	}
	u := s.userSettings()
	if p := s.probe(u); p.Status == "ok" && p.Path != nil && *p.Path != "" {
		_ = s.checkServeSupported(ctx, *p.Path)
	}
}

func (s *Service) publishCLIUpgradedAndClear() {
	if s == nil {
		return
	}
	if s.Bus != nil {
		_ = s.Bus.Publish("sync.reset", map[string]string{"reason": "cli_upgraded"})
	}
	s.clearCLIUpgradeState()
}

func (s *Service) finishDirectCLIInstall(ctx context.Context) {
	if s == nil {
		return
	}
	s.evictProgressCache()
	s.warmCompatibleCLI(ctx)
	s.publishCLIUpgradedAndClear()
}

func (s *Service) recoverAfterCLICompatible(u UserSettings) {
	s.clearProbeCache()
	s.evictProgressCache()
	s.mu.Lock()
	needRefresh := (s.device.LastError != nil && s.device.LastError.Code == "cli_outdated") ||
		(s.assets.AssetsError != nil && s.assets.AssetsError.Code == "cli_outdated")
	if needRefresh {
		s.deviceGeneration++
		s.device.Snapshot = nil
		s.device.CapturedAt = nil
		s.device.LastError = nil
		s.device.Refreshing = false
		s.assetsGen++
		s.assets = AssetsState{}
	}
	s.mu.Unlock()
	s.warmCompatibleCLI(s.ctx)
	if needRefresh && len(s.allModelsRoots(u)) > 0 {
		s.RefreshAssets()
		s.RefreshDevice()
	}
	s.publishCLIUpgradedAndClear()
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
			_, _ = s.upgradeCLI(s.ctx, nil, autoCLIUpgradeRetryDelays)
		}()
	})
}

func isRetryableCLIUpgradeError(e *ApiError) bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case "cli_upgrade_failed", "install_failed", "download_failed":
		return true
	default:
		return false
	}
}

func (s *Service) UpgradeCLI(ctx context.Context, onStatus func(string)) (string, *ApiError) {
	return s.upgradeCLI(ctx, onStatus, nil)
}

func (s *Service) upgradeCLI(ctx context.Context, onStatus func(string), retryDelays []time.Duration) (string, *ApiError) {
	if ctx == nil {
		if s.ctx != nil {
			ctx = s.ctx
		} else {
			ctx = context.Background()
		}
	}
	s.cliInstallMu.Lock()
	defer s.cliInstallMu.Unlock()

	u := s.userSettings()
	if p := s.probe(u); p.Status == "ok" && p.Path != nil {
		s.recoverAfterCLICompatible(u)
		return *p.Path, nil
	}

	targetVersion := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if _, ok := parseSemver(targetVersion); !ok {
		targetVersion = ""
	}
	upgradeCtx, cancel := context.WithCancel(ctx)
	s.cliUpgradeMu.Lock()
	s.cliUpgradeCancel = cancel
	s.cliUpgrade.State = CLIUpgradeUpgrading
	s.cliUpgrade.TargetVersion = targetVersion
	s.cliUpgrade.Message = ""
	s.cliUpgrade.Error = nil
	s.publishCLIUpgradeLocked()
	s.cliUpgradeMu.Unlock()
	defer func() {
		s.cliUpgradeMu.Lock()
		s.cliUpgradeCancel = nil
		s.cliUpgradeMu.Unlock()
		cancel()
	}()

	reportStatus := func(message string) {
		s.cliUpgradeMu.Lock()
		if s.cliUpgrade.State == CLIUpgradeUpgrading {
			s.cliUpgrade.Message = message
			s.publishCLIUpgradeLocked()
		}
		s.cliUpgradeMu.Unlock()
		if onStatus != nil {
			onStatus(message)
		}
	}

	var path string
	var apiErr *ApiError
	for attempt := 0; ; attempt++ {
		path, apiErr = s.installCLILocked(upgradeCtx, reportStatus)
		if apiErr == nil {
			break
		}
		if upgradeCtx.Err() != nil {
			s.clearCLIUpgradeState()
			return "", apiErr
		}
		if cur := s.userSettings(); s.probe(cur).Status == "ok" {
			s.recoverAfterCLICompatible(cur)
			return "", nil
		}
		if attempt >= len(retryDelays) || !isRetryableCLIUpgradeError(apiErr) {
			s.cliUpgradeMu.Lock()
			s.cliUpgrade.State = CLIUpgradeFailed
			s.cliUpgrade.TargetVersion = targetVersion
			s.cliUpgrade.Message = ""
			s.cliUpgrade.Error = cloneApiError(apiErr)
			s.publishCLIUpgradeLocked()
			s.cliUpgradeMu.Unlock()
			return "", apiErr
		}
		delay := retryDelays[attempt]
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-upgradeCtx.Done():
				timer.Stop()
				s.clearCLIUpgradeState()
				return "", apiErr
			case <-timer.C:
			}
		}
		if cur := s.userSettings(); s.probe(cur).Status == "ok" {
			s.recoverAfterCLICompatible(cur)
			return "", nil
		}
	}

	s.evictProgressCache()
	s.warmCompatibleCLI(upgradeCtx)
	s.publishCLIUpgradedAndClear()
	return path, nil
}

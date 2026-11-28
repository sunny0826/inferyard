package pages

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type cliRecoverySnapshot struct {
	valid          bool
	cliGen         uint64
	ready          core.Readiness
	settings       core.SettingsResponse
	cliUpgrade     core.CLIUpgradeState
	device         core.DeviceResponse
	registry       core.Registry
	checks         core.EngineCheckOverview
	serveSupported bool
}

func eventsMayRecoverCLI(events []core.Event, reset bool) bool {
	if reset {
		return true
	}
	for _, ev := range events {
		switch ev.Type {
		case "sync.reset":
			return true
		case "cli.upgrade":
			var payload struct {
				CLIUpgrade core.CLIUpgradeState `json:"cli_upgrade"`
			}
			if json.Unmarshal(ev.Data, &payload) == nil && payload.CLIUpgrade.State == core.CLIUpgradeIdle {
				return true
			}
		}
	}
	return false
}

func (s *viewState) captureCLIRecoverySnapshot() cliRecoverySnapshot {
	if s == nil || s.preview != nil || s.service == nil {
		return cliRecoverySnapshot{}
	}
	if s.ctx != nil && s.ctx.Err() != nil {
		return cliRecoverySnapshot{}
	}
	cliGen := s.service.CLIGeneration()
	ready := s.service.Readiness()
	if ready.CLI.Status != "ok" {
		return cliRecoverySnapshot{}
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return cliRecoverySnapshot{
		valid:          true,
		cliGen:         cliGen,
		ready:          ready,
		settings:       s.service.Settings(),
		cliUpgrade:     s.service.CLIUpgradeStatus(),
		device:         s.service.Device(),
		registry:       s.service.Registry(),
		checks:         s.service.EngineChecks(),
		serveSupported: s.service.ServeSupported(ctx),
	}
}

func (s *viewState) applyCLIRecoverySnapshot(snap cliRecoverySnapshot) {
	if !snap.valid || (s.ctx != nil && s.ctx.Err() != nil) {
		return
	}
	if snap.cliGen > s.appliedCLIGen {
		s.appliedCLIGen = snap.cliGen
	}
	wasNotOK := s.ready.CLI.Status != "ok"
	s.ready = snap.ready
	s.applySettings(snap.settings)
	if wasNotOK || (s.device.LastError != nil && s.device.LastError.Code == "cli_outdated") {
		s.device = snap.device
	}
	if wasNotOK || (s.registry.ModelError != nil && s.registry.ModelError.Code == "cli_outdated") {
		prevEngineName, prevModelName := s.engine, s.model
		prevEnginePath := s.selectedEnginePath()
		prevModelPath := s.selectedModelPath()
		s.registry = snap.registry
		if !s.applyPendingPolystrataSelection(prevEngineName, prevModelName) {
			s.reconcileSelectedEngine(prevEnginePath)
			s.reconcileSelectedModel(prevModelPath)
			if s.engine == "" {
				s.model = ""
			}
		}
	}
	if wasNotOK || !s.checks.Supported {
		s.checks = snap.checks
	}
	s.serveSupported = snap.serveSupported
	s.serveProbing = false
	s.applyCLIUpgradeState(snap.cliUpgrade)
	if wasNotOK {
		if s.failure != nil && s.failure.Code == "cli_outdated" {
			s.failure = nil
			s.failurePage = ""
		}
		if s.win != nil && s.progress == nil && s.benchFlowOpen {
			s.hydrating = true
		}
		cliKey := emptyText(snap.ready.CLI.Path) + "|" + emptyText(snap.ready.CLI.Version) + "|" + snap.ready.CLI.Status
		if cliKey != s.discoveryCLI {
			s.discoveryCLI, s.discoveryLoaded = cliKey, false
		}
	}
}

func cliOutdatedSummary(version *string) string {
	cur := strings.TrimSpace(emptyText(version))
	req := strings.TrimPrefix(strings.TrimSpace(core.Version), "v")
	if req == "dev" {
		req = ""
	}
	switch {
	case cur != "" && req != "":
		return l10n.TRf("onboarding.cli.outdated_cur_req", "current", cur, "required", req)
	case cur != "":
		return l10n.TRf("onboarding.cli.outdated_cur", "current", cur)
	default:
		return l10n.TR("onboarding.cli.outdated")
	}
}

func (s *viewState) upgradeCLI() {
	if s.simulate(previewPrepareEnvironment) {
		return
	}
	if s.service == nil || s.cliAutoUpgradeActive() {
		return
	}
	s.cliInstallStatus = ""
	s.cliUpgradeDismissedSeq = 0
	s.workAs("upgrade-cli", func() (string, *core.ApiError) {
		defer s.updateView(func() { s.cliInstallStatus = "" })
		_, e := s.service.UpgradeCLI(s.ctx, func(message string) {
			s.updateView(func() { s.cliInstallStatus = message })
		})
		if e != nil {
			st := s.service.CLIUpgradeStatus()
			s.updateView(func() { s.applyCLIUpgradeState(st) })
			return "", nil
		}
		snap := s.captureCLIRecoverySnapshot()
		s.updateView(func() { s.applyCLIRecoverySnapshot(snap) })
		return l10n.TR("onboarding.cli.upgraded"), nil
	})
}

func (s *viewState) effectiveCLIUpgradeMessage(c *ui.Context) string {
	if msg := strings.TrimSpace(s.cliUpgrade.Message); msg != "" {
		return msg
	}
	if msg := strings.TrimSpace(s.cliInstallStatus); msg != "" {
		return msg
	}
	return tr(c, "onboarding.cli.upgrading_status")
}

func (s *viewState) setupGuide(c *ui.Context) {
	onlyCLIOutdated := s.ready.CLI.Status == "outdated" && s.ready.Workspace.Initialized && s.ready.Results.Exists && s.ready.Models.Exists
	if onlyCLIOutdated {
		bound := s.service != nil && s.service.BoundPaths() != nil
		if !bound && s.cliAutoUpgradeActive() {
			card(c, tr(c, "onboarding.cli.upgrading_card"), func() {
				host := ui.Column(c.Key("auto-upgrade-cli")).FillWidth().Gap(tokens(c).Density.Row)
				_, elapsed := delayedElapsed(c, host, "auto-upgrade-cli", true)
				host.Children(func() {
					ui.Text(c, s.effectiveCLIUpgradeMessage(c)).TextColor(c.Theme().TextMuted)
					progressBar(c, -1, tr(c, "onboarding.cli.upgrade_progress"))
					if elapsed >= loadingSlowThreshold {
						ui.Text(c, tr(c, "onboarding.setup.slow_notice")).TextColor(c.Theme().TextMuted)
					}
				})
			})
			return
		}
		upgrading := s.actionBusy("upgrade-cli")
		card(c, tr(c, "onboarding.cli.upgrade_card"), func() {
			ui.Text(c, cliOutdatedSummary(s.ready.CLI.Version)).TextColor(c.Theme().TextMuted)
			if bound {
				ui.Text(c, tr(c, "onboarding.cli.bound_notice")).TextColor(c.Theme().TextMuted)
			} else {
				row := ui.Row(c.Key("upgrade-cli-actions")).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row)
				showProgress, elapsed := delayedElapsed(c, row, "upgrade-cli", upgrading)
				row.Children(func() {
					if primaryActionButton(c, tr(c, "onboarding.cli.upgrade_button"), upgrading).Disabled(s.busy).Clicked() {
						s.upgradeCLI()
					}
					button(c, tr(c, "onboarding.setup.open_settings")).Disabled(s.busy).OnClick(func() { s.openGeneralSettings() })
				})
				if showProgress {
					progressBar(c, -1, tr(c, "onboarding.cli.upgrade_progress"))
					if elapsed >= loadingSlowThreshold {
						ui.Text(c, tr(c, "onboarding.setup.slow_notice")).TextColor(c.Theme().TextMuted)
					}
				}
			}
			if s.cliInstallStatus != "" {
				ui.Text(c, s.cliInstallStatus)
			}
		})
		return
	}
	card(c, tr(c, "onboarding.setup.title"), func() {
		ui.Text(c, tr(c, "onboarding.setup.steps_summary")).TextColor(c.Theme().TextMuted)
		if s.ready.CLI.Status == "outdated" && !s.cliAutoUpgradeActive() && !s.cliUpgradeErrorVisible() {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: cliOutdatedSummary(s.ready.CLI.Version),
			})
		}
		preparing := s.actionBusy("prepare-env")
		row := ui.Row(c.Key("setup-actions")).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row)
		showProgress, elapsed := delayedElapsed(c, row, "prepare-env", preparing)
		row.Children(func() {
			if primaryActionButton(c, tr(c, "onboarding.setup.prepare_button"), preparing).Disabled(s.busy).Clicked() {
				s.prepareEnvironment()
			}
			button(c, tr(c, "onboarding.setup.open_settings")).Disabled(s.busy).OnClick(func() { s.openGeneralSettings() })
		})
		if showProgress {
			progressBar(c, -1, tr(c, "onboarding.setup.progress_label"))
			if elapsed >= loadingSlowThreshold {
				ui.Text(c, tr(c, "onboarding.setup.slow_notice")).TextColor(c.Theme().TextMuted)
			}
		}
		if s.cliInstallStatus != "" {
			ui.Text(c, s.cliInstallStatus)
		}
	})
}

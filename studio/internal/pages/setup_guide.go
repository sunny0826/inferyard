package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

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
		st := s.service.CLIUpgradeStatus()
		s.updateView(func() { s.applyCLIUpgradeState(st) })
		if e != nil {
			return "", nil
		}
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
					button(c, tr(c, "onboarding.setup.open_settings")).Disabled(s.busy).OnClick(func() { s.selected = "settings" })
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
			button(c, tr(c, "onboarding.setup.open_settings")).Disabled(s.busy).OnClick(func() { s.selected = "settings" })
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

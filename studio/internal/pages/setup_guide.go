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
	if s.service == nil {
		return
	}
	s.cliInstallStatus = ""
	s.guideWorkAs("upgrade-cli", func() (string, *core.ApiError) {
		defer s.updateView(func() { s.cliInstallStatus = "" })
		if _, e := s.service.InstallCLI(s.ctx, func(message string) {
			s.updateView(func() { s.cliInstallStatus = message })
		}); e != nil {
			return "", e
		}
		return l10n.TR("onboarding.cli.upgraded"), nil
	})
}

func (s *viewState) setupGuide(c *ui.Context) {
	onlyCLIOutdated := s.ready.CLI.Status == "outdated" && s.ready.Workspace.Initialized && s.ready.Results.Exists && s.ready.Models.Exists
	if onlyCLIOutdated {
		upgrading := s.actionBusy("upgrade-cli")
		bound := s.service != nil && s.service.BoundPaths() != nil
		card(c, tr(c, "onboarding.cli.upgrade_card"), func() {
			ui.Text(c, cliOutdatedSummary(s.ready.CLI.Version)).TextColor(c.Theme().TextMuted)
			locations, known := s.preparationLocations()
			componentFold(c, tr(c, "onboarding.setup.details_fold"), func() {
				setupActions(c, locations, known, s.ready.CLI)
				if known {
					setupTargets(c, locations)
				}
			})
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
		if s.ready.CLI.Status == "outdated" {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: cliOutdatedSummary(s.ready.CLI.Version),
			})
		}
		locations, known := s.preparationLocations()
		componentFold(c, tr(c, "onboarding.setup.details_fold"), func() {
			setupActions(c, locations, known, s.ready.CLI)
			if known {
				setupTargets(c, locations)
			}
		})
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

func setupActions(c *ui.Context, locations core.PreparationLocations, known bool, cli core.InferyardProbe) {
	py := managedPythonLabel(known, locations)
	switch {
	case known && locations.CLI != nil && locations.CLIStatus == "ok":
		ui.Text(c, trf(c, "onboarding.setup.cli_reuse", "path", displayName(*locations.CLI)))
	case (known && locations.CLIStatus == "outdated") || cli.Status == "outdated":
		oldVer := emptyText(cli.Version)
		if known && emptyText(locations.CLIVersion) != "" {
			oldVer = emptyText(locations.CLIVersion)
		}
		wantVer := strings.TrimPrefix(strings.TrimSpace(core.Version), "v")
		if wantVer == "dev" {
			wantVer = ""
		}
		if oldVer != "" && wantVer != "" {
			ui.Text(c, trf(c, "onboarding.setup.cli_upgrade_ver", "from", oldVer, "to", wantVer, "python", py))
		} else {
			ui.Text(c, trf(c, "onboarding.setup.cli_upgrade", "python", py))
		}
	default:
		ui.Text(c, trf(c, "onboarding.setup.cli_install", "python", py))
	}
	if known && locations.UV != nil {
		ui.Text(c, trf(c, "onboarding.setup.uv_reuse", "path", displayName(*locations.UV)))
	} else {
		ui.Text(c, tr(c, "onboarding.setup.uv_download"))
	}
}

func setupTargets(c *ui.Context, locations core.PreparationLocations) {
	deviceRow(c, tr(c, "onboarding.target.workspace"), configuredPath(locations.Workspace))
	deviceRow(c, tr(c, "onboarding.target.results"), configuredPath(locations.Results))
	deviceRow(c, tr(c, "onboarding.target.models"), configuredPath(locations.Models))
	if locations.ToolsDir != "" {
		deviceRow(c, tr(c, "onboarding.target.uv_tools"), configuredPath(ptrOrNil(locations.ToolsDir)))
	}
	if locations.CLI != nil {
		deviceRow(c, "CLI", displayName(*locations.CLI))
	}
	if locations.Bound {
		ui.Text(c, tr(c, "onboarding.target.bound_notice")).TextColor(c.Theme().TextMuted)
	}
}

func (s *viewState) preparationLocations() (core.PreparationLocations, bool) {
	if s.prepLocationsLoaded {
		return s.prepLocations, true
	}
	if s.service == nil {
		return core.PreparationLocations{}, false
	}
	return s.service.PreparationLocations(), true
}

func managedPythonLabel(known bool, locations core.PreparationLocations) string {
	if known && locations.ManagedPython != "" {
		return locations.ManagedPython
	}
	return l10n.TR("onboarding.setup.python_runtime")
}

func configuredPath(value *string) string {
	if value == nil || *value == "" {
		return l10n.TR("onboarding.target.default_path")
	}
	return displayName(*value)
}

func ptrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

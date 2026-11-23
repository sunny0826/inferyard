package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func cliOutdatedSummary(version *string) string {
	cur := strings.TrimSpace(emptyText(version))
	req := strings.TrimPrefix(strings.TrimSpace(core.Version), "v")
	if req == "dev" {
		req = ""
	}
	switch {
	case cur != "" && req != "":
		return fmt.Sprintf("CLI 版本过旧（当前 %s，需要 %s），请升级 CLI。", cur, req)
	case cur != "":
		return fmt.Sprintf("CLI 版本过旧（当前 %s），请升级 CLI。", cur)
	default:
		return "CLI 版本过旧，请升级 CLI。"
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
		return "CLI 已升级", nil
	})
}

func (s *viewState) setupGuide(c *ui.Context) {
	onlyCLIOutdated := s.ready.CLI.Status == "outdated" && s.ready.Workspace.Initialized && s.ready.Results.Exists && s.ready.Models.Exists
	if onlyCLIOutdated {
		upgrading := s.actionBusy("upgrade-cli")
		bound := s.service != nil && s.service.BoundPaths() != nil
		card(c, "CLI 需要升级", func() {
			ui.Text(c, cliOutdatedSummary(s.ready.CLI.Version)).TextColor(c.Theme().TextMuted)
			locations, known := s.preparationLocations()
			componentFold(c, "查看准备详情", func() {
				setupActions(c, locations, known, s.ready.CLI)
				if known {
					setupTargets(c, locations)
				}
			})
			if bound {
				ui.Text(c, "当前为开发环境绑定模式，请同步更新本地 CLI 后刷新。").TextColor(c.Theme().TextMuted)
			} else {
				row := ui.Row(c.Key("upgrade-cli-actions")).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row)
				showProgress, elapsed := delayedElapsed(c, row, "upgrade-cli", upgrading)
				row.Children(func() {
					if primaryActionButton(c, "升级 CLI", upgrading).Disabled(s.busy).Clicked() {
						s.upgradeCLI()
					}
					button(c, "前往设置").Disabled(s.busy).OnClick(func() { s.selected = "settings" })
				})
				if showProgress {
					progressBar(c, -1, "CLI 升级进度")
					if elapsed >= loadingSlowThreshold {
						ui.Text(c, "网络下载或环境初始化耗时较长，仍在继续…").TextColor(c.Theme().TextMuted)
					}
				}
			}
			if s.cliInstallStatus != "" {
				ui.Text(c, s.cliInstallStatus)
			}
		})
		return
	}
	card(c, "环境准备", func() {
		ui.Text(c, "首次使用流程：1. 准备运行环境 → 2. 获取引擎与模型 → 3. 开始首次测评").TextColor(c.Theme().TextMuted)
		if s.ready.CLI.Status == "outdated" {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: cliOutdatedSummary(s.ready.CLI.Version),
			})
		}
		locations, known := s.preparationLocations()
		componentFold(c, "查看准备详情", func() {
			setupActions(c, locations, known, s.ready.CLI)
			if known {
				setupTargets(c, locations)
			}
		})
		preparing := s.actionBusy("prepare-env")
		row := ui.Row(c.Key("setup-actions")).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row)
		showProgress, elapsed := delayedElapsed(c, row, "prepare-env", preparing)
		row.Children(func() {
			if primaryActionButton(c, "准备环境", preparing).Disabled(s.busy).Clicked() {
				s.prepareEnvironment()
			}
			button(c, "前往设置").Disabled(s.busy).OnClick(func() { s.selected = "settings" })
		})
		if showProgress {
			progressBar(c, -1, "环境准备进度")
			if elapsed >= loadingSlowThreshold {
				ui.Text(c, "网络下载或环境初始化耗时较长，仍在继续…").TextColor(c.Theme().TextMuted)
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
		ui.Text(c, "CLI：复用 "+displayName(*locations.CLI))
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
			ui.Text(c, fmt.Sprintf("CLI：从 %s 升级至 %s（Python %s）", oldVer, wantVer, py))
		} else {
			ui.Text(c, "CLI：升级至配套版本（Python "+py+"）")
		}
	default:
		ui.Text(c, "CLI：安装配套版本（Python "+py+"）")
	}
	if known && locations.UV != nil {
		ui.Text(c, "uv：复用 "+displayName(*locations.UV))
	} else {
		ui.Text(c, "uv：按需下载独立版本")
	}
}

func setupTargets(c *ui.Context, locations core.PreparationLocations) {
	deviceRow(c, "工作区", configuredPath(locations.Workspace))
	deviceRow(c, "结果目录", configuredPath(locations.Results))
	deviceRow(c, "模型目录", configuredPath(locations.Models))
	if locations.ToolsDir != "" {
		deviceRow(c, "uv 工具", configuredPath(ptrOrNil(locations.ToolsDir)))
	}
	if locations.CLI != nil {
		deviceRow(c, "CLI", displayName(*locations.CLI))
	}
	if locations.Bound {
		ui.Text(c, "开发环境下以上路径固定。").TextColor(c.Theme().TextMuted)
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
	return "运行时"
}

func configuredPath(value *string) string {
	if value == nil || *value == "" {
		return "默认路径"
	}
	return displayName(*value)
}

func ptrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

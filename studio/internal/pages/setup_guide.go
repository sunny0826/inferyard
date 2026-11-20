package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) setupGuide(c *ui.Context) {
	card(c, "环境准备", func() {
		ui.Text(c, "首次使用流程：1. 准备运行环境 → 2. 获取引擎与模型 → 3. 开始首次测评").TextColor(c.Theme().TextMuted)
		ui.Text(c, "自动安装或复用本地 CLI、uv 与工作区目录，已完成步骤将跳过。")
		locations, known := s.preparationLocations()
		setupActions(c, locations, known)
		if known {
			setupTargets(c, locations)
		}
		ui.Text(c, "首次准备需联网下载组件，耗时视网络环境而定；也可在设置页手动指定 CLI 路径。").TextColor(c.Theme().TextMuted)
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

func setupActions(c *ui.Context, locations core.PreparationLocations, known bool) {
	if known && locations.CLI != nil {
		ui.Text(c, "CLI：复用 "+displayName(*locations.CLI))
	} else {
		ui.Text(c, "CLI：安装配套版本（Python "+managedPythonLabel(known, locations)+"，不修改 shell 配置）")
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
		return "尚未配置（准备时使用默认值）"
	}
	return displayName(*value)
}

func ptrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

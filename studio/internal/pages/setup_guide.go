package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// setupGuide explains what preparation does before the user starts it: whether
// it needs the network, what it writes and where, and that sizes cannot be
// predicted. Installed components are reused; the same idempotent chain runs on
// retry.
func (s *viewState) setupGuide(c *ui.Context) {
	card(c, "环境准备", func() {
		ui.Text(c, "自动安装或复用本地 CLI、uv 与工作区目录，已完成步骤将跳过。")
		locations, known := s.preparationLocations()
		setupActions(c, locations, known)
		if known {
			setupTargets(c, locations)
		}
		ui.Text(c, "首次准备需联网下载组件，耗时视网络环境而定；也可在设置页手动指定 CLI 路径。").TextColor(c.Theme().TextMuted)
		if primaryButton(c, "准备环境").Disabled(s.busy).Clicked() {
			s.prepareEnvironment()
		}
		if s.cliInstallStatus != "" {
			ui.Text(c, s.cliInstallStatus)
		}
	})
}

// setupActions states what preparation reuses and downloads.
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

// setupTargets names every write target by directory or file name, never by a
// full path; the settings page remains the only place that shows those.
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

// preparationLocations reads the core-owned path projection. The preview has no
// service and therefore no path facts; it shows the explanation without paths.
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

// configuredPath names a target without exposing the full path: the settings
// page stays the only place that shows absolute paths.
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

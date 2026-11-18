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
		ui.Text(c, "欢迎使用 InferYard Studio。先准备环境，再获取模型与引擎，最后开始测评。")
		ui.Text(c, "已完成的步骤会自动跳过；失败后可重试，处理顺序不变。")
		locations, known := s.preparationLocations()
		ui.Text(c, "这一步会做什么").Bold()
		setupActions(c, locations, known)
		ui.Text(c, "会写到哪里").Bold()
		setupTargets(c, locations, known)
		ui.Text(c, "下载量与耗时").Bold()
		ui.Text(c, "下载量取决于本机已有的 CLI、uv、Python 和依赖，无法可靠预估，因此不显示估算大小或进度百分比。")
		ui.Text(c, "首次可能需要几分钟；CLI 安装子步骤有 15 分钟超时，这不是整个准备过程的预计耗时。")
		ui.Text(c, "需要手动指定 CLI 时可到设置页填写路径。").TextColor(c.Theme().TextMuted)
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
		ui.Text(c, "复用已找到的 CLI："+displayName(*locations.CLI)+"。")
	} else {
		ui.Text(c, "安装与 Studio 同版本的 CLI；首次可能需要联网下载组件并安装。")
	}
	if known && locations.UV != nil {
		ui.Text(c, "复用已找到的 uv："+displayName(*locations.UV)+"。")
	} else {
		ui.Text(c, "若本机没有 uv，会下载固定版本的独立 uv 工具。")
	}
	ui.Text(c, "会把依赖装进 uv 的用户工具目录，并安装 Python "+managedPythonLabel(known, locations)+"；不会修改 shell 配置。")
	ui.Text(c, "随后建立或复用工作区、结果目录和模型目录。")
}

// setupTargets names every write target by directory or file name, never by a
// full path; the settings page remains the only place that shows those.
func setupTargets(c *ui.Context, locations core.PreparationLocations, known bool) {
	if !known {
		ui.Text(c, "路径以设置页为准；安装完成后显示实际的 CLI 路径。").TextColor(c.Theme().TextMuted)
		return
	}
	ui.Text(c, "工作区："+configuredPath(locations.Workspace))
	ui.Text(c, "结果目录："+configuredPath(locations.Results))
	ui.Text(c, "模型目录："+configuredPath(locations.Models))
	if locations.ToolsDir != "" {
		ui.Text(c, "uv 工具："+configuredPath(ptrOrNil(locations.ToolsDir)))
	}
	if locations.CLI != nil {
		ui.Text(c, "CLI 可执行文件："+displayName(*locations.CLI))
	} else {
		ui.Text(c, "CLI 路径由 uv 的用户工具目录决定，安装完成后显示实际路径。")
	}
	if locations.Bound {
		ui.Text(c, "开发环境下这些路径固定，不读取设置。").TextColor(c.Theme().TextMuted)
	}
	ui.Text(c, "完整路径见设置页。").TextColor(c.Theme().TextMuted)
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

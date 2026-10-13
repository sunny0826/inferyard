package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func human(code string) string {
	switch code {
	case "unsupported_platform":
		return "当前平台不支持此候选流程，请检查平台支持范围。"
	case "device_preflight_blocked":
		return "设备检查未给出可运行推荐，请检查设备报告后重试。"
	case "blocked", "preflight_missing":
		return "设备检查尚未通过，请检查准备条件后重试。"
	case "incomplete", "run_incomplete":
		return "运行尚未封存，请检查停止原因后继续运行。"
	case "cancelled":
		return "操作已取消，可检查进度后重试。"
	case "output_exists":
		return "输出目录已存在，请选择新目录。"
	case "settings_incomplete":
		return "目录配置缺项，请先配置目录。"
	case "cli_unavailable":
		return "CLI 尚未就绪，请在主窗口设置路径。"
	case "install_unsupported":
		return "当前平台没有可用的自动安装包，请按安装指南手动安装。"
	case "download_failed":
		return "下载失败，请检查网络后重试。"
	case "install_checksum_failed":
		return "下载内容未通过校验，已中止安装，请重试。"
	case "install_unpack_failed":
		return "安装包解包失败，请重试或按安装指南手动安装。"
	case "install_failed":
		return "CLI 自动安装失败，可重试或改用「手动指定路径」。"
	case "install_version_unknown":
		return "无法确定要安装的 CLI 版本，请按安装指南手动安装。"
	case "engine_unpack_failed":
		return "引擎归档解包失败，请检查来源后重试。"
	case "engine_binary_missing":
		return "引擎归档内未找到 llama-server，请检查引擎来源。"
	case "cli_outdated":
		return "CLI 版本过旧，缺少 progress 命令。请升级 CLI 后重试。"
	case "cli_error":
		return "CLI 执行失败，请检查主窗口诊断后重试。"
	case "invalid_request":
		return "输入不符合要求，请检查路径、URL 和环境变量名。"
	case "not_found":
		return "所需文件或记录不存在，请检查工作区状态后重试。"
	case "conflict":
		return "操作与当前状态冲突，请刷新进度后重试。"
	case "internal_error":
		return "Studio 内部错误，请查看主窗口诊断后重试。"
	case "error":
		return "CLI 报告工具或证据错误，请检查主窗口诊断与工作区证据后重试。"
	case "unknown":
		return "CLI 异常退出，请检查主窗口诊断后重试。"
	default:
		return "当前步骤未能完成，请检查主窗口诊断与工作区证据后重试。"
	}
}

func (s *viewState) guideWork(fn func() (string, *core.ApiError)) {
	if s.busy {
		return
	}
	s.work(func() (string, *core.ApiError) {
		message, e := fn()
		if e != nil {
			mapped := *e
			mapped.Message = human(e.Code)
			return "", &mapped
		}
		return message, nil
	})
}
func (s *viewState) feedback(c *ui.Context) {
	if s.failure != nil {
		errorView(c, s.failure)
	} else if s.refreshFailure != "" {
		ui.Text(c, s.refreshFailure).TextColor(c.Theme().Danger)
	}
}

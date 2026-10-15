package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func human(code string) string {
	switch code {
	case "service_closing":
		return "Studio 正在关闭，无法接收新任务。"
	case "run_active":
		return "测评正在进行，请先取消并等待收尾，再停止引擎。"
	case "engine_orphaned":
		return "请先处理工作区 logs/engine-owner.json 对应的遗留服务或无效记录，再启动引擎。"
	case "engine_owner_invalid":
		return "工作区 logs/engine-owner.json 无效；可仅删除记录（不终止进程）。"
	case "engine_owner_write_failed":
		return "无法保存工作区 logs/engine-owner.json，已中止启动。"
	case "engine_owner_remove_failed":
		return "工作区 logs/engine-owner.json 未能清除；可重试仅删除记录（不终止进程）。"
	case "engine_stop_failed":
		return "遗留引擎停止失败，所有权记录已保留。"
	case "engine_stopping":
		return "引擎正在停止，请等待回收后再运行。"
	case "workspace_busy":
		return "当前工作区仍有运行或托管引擎，请先结束任务并停止引擎。"
	case "engine_host_not_loopback":
		return "引擎监听地址必须是 127.0.0.1、::1 或 localhost。"
	case "engine_credentials_forbidden":
		return "启动参数含凭据，只接受凭据环境变量引用，请重新创建候选。"
	case "engine_not_executable":
		return "引擎文件不存在或不可执行，请检查引擎路径和权限。"
	case "service_identity_unreadable":
		return "无法读取绑定进程身份，请检查进程权限后刷新。"
	case "unsupported_platform":
		return "当前平台不支持此候选流程，请检查平台支持范围。"
	case "device_preflight_blocked":
		return "设备检查未给出可运行推荐，请检查设备报告后重试。"
	case "blocked", "preflight_missing":
		return "设备检查尚未通过，请检查准备条件后重试。"
	case "incomplete", "run_incomplete":
		return "运行尚未封存，请检查停止原因后继续运行。"
	case "single_run_resume_not_supported":
		return "单次运行不支持断点续跑；请重新运行该配置。"
	case "duration_resume_requires_new_window":
		return "时长型任务不支持续跑；请按冻结计划重新开窗运行。"
	case "batch_plan_not_finished":
		return "批计划尚有未完成题目；请按冻结计划继续运行。"
	case "cancelled":
		return "操作已取消，可检查进度后重试。"
	case "output_name_exhausted":
		return "前 1000 个目录名均已占用，请更换目录名或工作区。"
	case "cli_blocked":
		return "CLI 拒绝执行，请检查限制原因。"
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
		return "CLI 不可用或缺少所需命令，请安装或升级 CLI 后重试。"
	case "engine_check_error":
		return "引擎核验执行失败，请检查诊断后重试。"
	case "engine_check_invalid_response":
		return "CLI 引擎核验结果格式无效，请升级 CLI 后重试。"
	case "invalid_library_manifest":
		return "引擎运行库不完整或库链接无效"
	case "unsupported_engine_build_requires_adapter_validation":
		return "引擎版本与评测要求不符"
	case "unsupported_runtime_profile":
		return "引擎运行时 profile 不受支持"
	case "runtime_receipt_invalid":
		return "运行时回执无效"
	case "runtime_profile_mismatch":
		return "运行时回执与 profile 不符"
	case "runtime_asset_mismatch":
		return "运行时文件与清单不符"
	case "runtime_receipt_missing":
		return "引擎目录缺少 runtime-receipt.json，无法核验"
	case "engine_version_query_failed":
		return "引擎版本查询失败或超时"
	case "package_resource_invalid":
		return "CLI 内置资源无效，请重新安装或升级 CLI。"
	case "io_error":
		return "文件读写失败，请检查目录权限后重试。"
	case "invalid_input":
		return "输入无效，请检查参数后重试。"
	case "candidate_mismatch":
		return "候选配置与所选组合不一致，请重试。"
	case "engine_exited":
		return "模型服务启动后退出"
	case "engine_start_timeout":
		return "模型服务启动超时（模型加载未完成）"
	case "engine_start_failed":
		return "引擎进程启动失败，请检查文件权限与运行环境"
	case "engine_port_in_use":
		return "端口已被占用：该端口可能已有模型服务"
	case "service_discovery_invalid_response":
		return "CLI 服务发现结果格式无效，请升级 CLI 后重试。"
	case "service_discovery_error":
		return "本机服务发现失败，请检查诊断后手动刷新。"
	case "service_discovery_unsupported_platform":
		return "当前平台不支持本机服务发现。"
	case "listener_enumeration_failed":
		return "无法读取连接表或枚举进程，本次发现失败。"
	case "listener_table_incomplete":
		return "部分连接表不可读或含无效记录，扫描不完整。"
	case "listener_ownership_incomplete":
		return "部分监听进程不可读、已退出或无法确定，扫描不完整。"
	case "process_identity_unavailable":
		return "部分监听进程名称不可读或启动身份已变化，扫描不完整。"
	case "models_timeout":
		return "模型列表请求超时。"
	case "models_unreachable":
		return "模型列表连接、传输或 HTTP 协议失败。"
	case "models_http_1xx":
		return "模型列表返回 HTTP 1xx。"
	case "models_http_3xx":
		return "模型列表返回 HTTP 3xx，未跟随重定向。"
	case "models_http_4xx":
		return "模型列表返回 HTTP 4xx，可能需要认证。"
	case "models_http_5xx":
		return "模型列表返回 HTTP 5xx。"
	case "models_invalid_response":
		return "模型列表响应格式无效。"
	case "models_empty":
		return "服务未返回可列出的模型。"
	case "models_response_too_large":
		return "模型列表响应超过 1 MiB。"
	case "health_timeout":
		return "模型列表可用，但健康检查超时。"
	case "health_unreachable":
		return "模型列表可用，但健康检查连接、传输或协议失败。"
	case "startup_arguments_mismatch":
		return "启动参数与候选不一致，绑定已停止。请核对候选与引擎配置；不会自动重试。"
	case "service_process_unavailable":
		return "绑定的服务进程已不可用，请刷新服务列表后重新选择。"
	case "binding_progress_changed":
		return "工作区或候选绑定阶段已变化，绑定链已停止；不会自动重试。"
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
		return "尚未识别的原因码：" + code + "。请检查诊断与工作区证据后重试。"
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
			if mapped.Message == "" {
				mapped.Message = human(e.Code)
			}
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

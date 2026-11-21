package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func human(code string) string {
	switch code {
	case "candidate_description_invalid":
		return "CLI 的测评配置描述不符合当前接口，请升级配套 CLI。"
	case "local_engine_installation_changed":
		return "安装声明在准备过程中发生变化，请重新核验并创建测评配置。"
	case "local_engine_installation_unreadable", "local_engine_installation_rejected", "local_engine_installation_ineligible":
		return "本地安装声明未通过核验，请检查声明和运行环境。"
	case "local_engine_requires_darwin_arm64":
		return "此本地引擎流程目前仅支持 Apple Silicon macOS。"
	case "local_engine_json_unreadable":
		return "本地引擎安装声明 JSON 无法读取或格式无效，请检查 installation.json。"
	case "local_engine_absolute_path_required":
		return "本地引擎安装声明中的路径必须为绝对路径。"
	case "local_engine_locator_missing":
		return "本地引擎安装声明缺少定位或入口字段，请补充完整后重试。"
	case "local_engine_runtime_root_invalid":
		return "本地引擎运行时根目录无效或不存在，请检查安装目录。"
	case "local_engine_cwd_invalid":
		return "本地引擎工作目录无效，请检查声明中的工作目录路径。"
	case "local_engine_executable_invalid":
		return "本地引擎可执行文件无效或不可执行，请检查路径与权限。"
	case "local_engine_models_root_required_for_lmstudio_only":
		return "仅 LM Studio 安装声明需要指定模型目录，其他本地引擎请移除该字段。"
	case "local_engine_non_regular_asset":
		return "本地引擎资产不是常规文件，未通过完整性核验。"
	case "local_engine_directory_symlink":
		return "本地引擎目录包含符号链接，未通过安全与一致性核验。"
	case "local_engine_python_venv_required":
		return "此 Python 本地引擎需要使用虚拟环境（venv）中的 Python 解释器。"
	case "local_engine_python_probe_failed":
		return "Python 本地引擎环境探测失败，请检查虚拟环境与依赖是否完整。"
	case "local_engine_runtime_changed_during_hash":
		return "校验期间本地引擎运行时文件发生变化，请确认无后台改动后重试。"
	case "local_engine_runtime_unreadable":
		return "无法读取本地引擎运行时文件，请检查目录权限与文件完整性。"
	case "local_engine_runtime_invalid":
		return "本地引擎运行时环境无效或不完整，请检查安装内容。"
	case "local_engine_runtime_changed":
		return "本地引擎运行时环境在准备或核验后发生变化，请重新核验引擎。"
	case "local_engine_python_import_path_invalid":
		return "本地引擎的 Python 模块导入路径无效，请检查安装声明配置。"
	case "local_engine_import_root_undeclared":
		return "本地引擎使用了未在安装声明中申明的 Python 导入根目录。"
	case "local_engine_python_entrypoint_invalid":
		return "本地引擎的 Python 入口脚本无效，请检查路径与格式。"
	case "local_engine_python_entrypoint_missing":
		return "未找到本地引擎声明的 Python 入口脚本，请检查文件路径。"
	case "local_engine_mapping_unverified":
		return "无法核验本地引擎的动态库或路径映射。"
	case "local_engine_macho_unreadable":
		return "无法读取本地引擎的 Mach-O 二进制信息，请检查文件完整性。"
	case "local_engine_mapped_library_closure_unverified":
		return "未能完整核验本地引擎加载的动态库依赖闭包。"
	case "local_engine_service_reused":
		return "上一测评题集的服务不能重复使用，请重新启动服务并填写新的后台 PID。"
	case "local_engine_lmstudio_background_pid_required", "local_engine_external_binding_required":
		return "请先启动 LM Studio 后台服务并加载模型，再连接后台进程 PID。"
	case "candidate_endpoint_mismatch":
		return "服务地址与本次固定的测评配置不一致，请填写创建测评配置时的本机地址。"
	case "service_identity_unavailable":
		return "无法核验后台进程的启动身份，请检查 PID 和进程权限。"
	case "previous_scope_incomplete":
		return "上一测评题集未完整结束，后续测评题集尚未执行。请先处理停止原因。"
	case "candidate_binding_invalid":
		return "连接产物与本次测评题集或结果目录不一致，运行已停止。"
	case "local_engine_context_speed_unsupported", "local_engine_requires_explicit_candidate":
		return "此安装声明引擎需要使用常规测评的本地引擎流程。"
	case "engine_working_directory_invalid", "engine_environment_invalid", "engine_process_isolation_failed":
		return "引擎工作目录或进程隔离设置无效，启动已停止。"
	case "engine_candidate_mismatch":
		return "当前服务属于另一测评配置，请先结束任务并停止该服务。"
	case "model_discovery_limit":
		return "模型目录超过扫描范围，请将模型放在较浅的独立目录中。"
	case "model_discovery_unreadable":
		return "无法读取模型目录，请检查目录与权限。"
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
		return "启动参数含凭据，只接受凭据环境变量引用，请重新创建测评配置。"
	case "engine_not_executable":
		return "引擎文件不存在或不可执行，请检查引擎路径和权限。"
	case "service_identity_unreadable":
		return "无法读取绑定进程身份，请检查进程权限后刷新。"
	case "unsupported_platform":
		return "当前平台不支持此测评配置流程，请检查平台支持范围。"
	case "device_preflight_blocked":
		return "设备检查未给出可运行推荐，请检查设备报告后重试。"
	case "no_local_model":
		return "还没有可用的本地模型，请到下载页获取模型，或在设置中指定已有模型目录。"
	case "blocked", "preflight_missing":
		return "设备检查尚未通过，请检查准备条件后重试。"
	case "incomplete", "run_incomplete":
		return "运行证据尚未收尾，请检查停止原因后继续运行。"
	case "single_run_resume_not_supported":
		return "单次运行不支持断点续跑；请重新运行该配置。"
	case "not_resumable":
		return "当前运行不可续测；请用「新任务」重新测评。"
	case "duration_resume_requires_new_window":
		return "时长型任务不支持续跑；请按固定计划重新开窗运行。"
	case "batch_plan_not_finished":
		return "批计划尚有未完成题目；请按固定计划继续运行。"
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
	case "ac_power_required":
		return "macOS 测评需接通交流电源后运行，请插上电源适配器后重试。"
	case "chat_template_unavailable":
		return "模型缺少聊天模板，无法用于当前对话测评。"
	case "model_acquire_incomplete":
		return "模型下载未完成，请检查下方具体原因与网络或目录状态后重试。"
	case "engine_acquire_incomplete":
		return "引擎下载未完成，请检查下方具体原因与网络或目录状态后重试。"
	case "engine_checksum_mismatch":
		return "引擎下载校验和不匹配，文件可能已损坏，请重新下载。"
	case "token_env_missing":
		return "指定的访问凭据环境变量未设置或内容无效，请在系统环境中配置后重试。"
	case "auth_required":
		return "远端服务拒绝访问（HTTP 401/403），请检查访问凭据与权限后重试。"
	case "rate_limited":
		return "远端下载请求触发限流（HTTP 429），请稍后重试。"
	case "upstream_http_error":
		return "远端服务器返回异常 HTTP 状态，请稍后重试或检查下载地址。"
	case "unexpected_content_encoding":
		return "远端响应使用了不支持的内容编码（需 identity 原始字节流），下载已中止。"
	case "network_timeout":
		return "网络请求或读取超时，请检查网络连接后重试。"
	case "connection_error":
		return "网络连接失败或中断，请检查网络及代理配置后重试。"
	case "transfer_error":
		return "下载传输过程中发生网络协议或传输错误，请重试。"
	case "disk_full":
		return "磁盘可用空间不足，无法继续写入文件，请清理磁盘空间后重试。"
	case "permission_denied":
		return "没有足够的文件或目录写入权限，请检查目标目录权限后重试。"
	case "engine_unpack_failed":
		return "引擎归档解包失败，请检查来源后重试。"
	case "engine_binary_missing":
		return "引擎归档内未找到 llama-server，请检查引擎来源。"
	case "cli_outdated":
		return "CLI 不可用或缺少所需命令，请安装或升级 CLI 后重试。"
	case "engine_check_error":
		return "引擎核验执行失败，请检查引擎路径后重试。"
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
		return "测评配置与所选组合不一致，请重试。"
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
		return "本机服务发现失败，请检查权限后手动刷新。"
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
		return "启动参数与测评配置不一致，连接已停止。请核对测评配置与引擎设置；不会自动重试。"
	case "service_process_unavailable":
		return "已连接的服务进程不可用，请刷新服务列表后重新选择。"
	case "binding_progress_changed":
		return "工作区或测评配置的连接阶段已变化，连接链已停止；不会自动重试。"
	case "report_unreadable":
		return "报告文件无法读取或格式无效，请重新生成。"
	case "unsupported_report_format":
		return "旧格式报告不受支持，请用当前 CLI 重新生成报告。"
	case "cli_error":
		return "CLI 执行失败，请检查下方错误详情与运行状态后重试。"
	case "invalid_request":
		return "输入不符合要求，请检查路径、URL 和环境变量名。"
	case "not_found":
		return "所需文件、记录或远端资源不存在，请检查路径、工作区状态或下载地址后重试。"
	case "conflict":
		return "操作与当前状态冲突，请刷新进度后重试。"
	case "internal_error":
		return "Studio 内部错误，请检查下方错误详情后重试。"
	case "error":
		return "CLI 报告工具或证据错误，请检查下方错误详情与工作区证据后重试。"
	case "unknown":
		return "CLI 异常退出，请检查下方错误详情与运行状态后重试。"
	default:
		return "尚未识别的原因码：" + code + "。请检查下方错误详情与工作区证据后重试。"
	}
}

func (s *viewState) guideWork(fn func() (string, *core.ApiError)) {
	s.guideWorkAs("", fn)
}

func (s *viewState) guideWorkAs(action string, fn func() (string, *core.ApiError)) {
	if s.busy {
		return
	}
	s.workAs(action, func() (string, *core.ApiError) {
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
		if s.failurePage != "" && s.failurePage != s.selected {
			s.failure = nil
			s.failurePage = ""
		} else if !s.stepOwnsError(s.failure) {
			errorRecoveryBanner(c, s.failure, func() {
				if ghostButton(c, "关闭").Clicked() {
					s.failure = nil
					s.failurePage = ""
				}
			})
		}
	}
	if s.failure == nil && s.refreshFailure != "" {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Size:    bannerCompact,
			Message: s.refreshFailure,
			Actions: func() {
				button(c, "重试").OnClick(s.refreshAction)
				if ghostButton(c, "关闭").Clicked() {
					s.refreshFailure = ""
				}
			},
		})
	}
}

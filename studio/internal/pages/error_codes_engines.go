package pages

// humanLocalEngineCode maps local-engine and PolyStrata error codes to user-facing copy.
func humanLocalEngineCode(code string) (string, bool) {
	switch code {
	case "local_engine_installation_changed":
		return "安装声明在准备过程中发生变化，请重新核验并创建测评配置。", true
	case "local_engine_installation_unreadable", "local_engine_installation_rejected", "local_engine_installation_ineligible":
		return "本地安装声明未通过核验，请检查声明和运行环境。", true
	case "local_engine_requires_darwin_arm64":
		return "此本地引擎流程目前仅支持 Apple Silicon macOS。", true
	case "local_engine_json_unreadable":
		return "本地引擎安装声明 JSON 无法读取或格式无效，请检查 installation.json。", true
	case "local_engine_absolute_path_required":
		return "本地引擎安装声明中的路径必须为绝对路径。", true
	case "local_engine_locator_missing":
		return "本地引擎安装声明缺少定位或入口字段，请补充完整后重试。", true
	case "local_engine_runtime_root_invalid":
		return "本地引擎运行时根目录无效或不存在，请检查安装目录。", true
	case "local_engine_cwd_invalid":
		return "本地引擎工作目录无效，请检查声明中的工作目录路径。", true
	case "local_engine_executable_invalid":
		return "本地引擎可执行文件无效或不可执行，请检查路径与权限。", true
	case "local_engine_models_root_required_for_lmstudio_only":
		return "仅 LM Studio 安装声明需要指定模型目录，其他本地引擎请移除该字段。", true
	case "local_engine_non_regular_asset":
		return "本地引擎资产不是常规文件，未通过完整性核验。", true
	case "local_engine_directory_symlink":
		return "本地引擎目录包含符号链接，未通过安全与一致性核验。", true
	case "local_engine_python_venv_required":
		return "此 Python 本地引擎需要使用虚拟环境（venv）中的 Python 解释器。", true
	case "local_engine_python_probe_failed":
		return "Python 本地引擎环境探测失败，请检查虚拟环境与依赖是否完整。", true
	case "local_engine_runtime_changed_during_hash":
		return "校验期间本地引擎运行时文件发生变化，请确认无后台改动后重试。", true
	case "local_engine_runtime_unreadable":
		return "无法读取本地引擎运行时文件，请检查目录权限与文件完整性。", true
	case "local_engine_runtime_invalid":
		return "本地引擎运行时环境无效或不完整，请检查安装内容。", true
	case "local_engine_runtime_changed":
		return "本地引擎运行时环境在准备或核验后发生变化，请重新核验引擎。", true
	case "local_engine_python_import_path_invalid":
		return "本地引擎的 Python 模块导入路径无效，请检查安装声明配置。", true
	case "local_engine_import_root_undeclared":
		return "本地引擎使用了未在安装声明中申明的 Python 导入根目录。", true
	case "local_engine_python_entrypoint_invalid":
		return "本地引擎的 Python 入口脚本无效，请检查路径与格式。", true
	case "local_engine_python_entrypoint_missing":
		return "未找到本地引擎声明的 Python 入口脚本，请检查文件路径。", true
	case "local_engine_mapping_unverified":
		return "无法核验本地引擎的动态库或路径映射。", true
	case "local_engine_macho_unreadable":
		return "无法读取本地引擎的 Mach-O 二进制信息，请检查文件完整性。", true
	case "local_engine_mapped_library_closure_unverified":
		return "未能完整核验本地引擎加载的动态库依赖闭包。", true
	case "local_engine_service_reused":
		return "上一测评题集的服务不能重复使用，请重新启动服务并填写新的后台 PID。", true
	case "local_engine_lmstudio_background_pid_required", "local_engine_external_binding_required":
		return "请先启动 LM Studio 后台服务并加载模型，再连接后台进程 PID。", true
	case "local_engine_context_speed_unsupported", "local_engine_requires_explicit_candidate":
		return "此安装声明引擎需要使用常规测评的本地引擎流程。", true
	case "local_engine_data_dir_polystrata_only":
		return "仅 PolyStrata 引擎支持设置数据目录。", true
	case "local_engine_data_dir_invalid":
		return "引擎数据目录无效，请检查路径。", true
	case "polystrata_preflight_blocked":
		return "环境预检未通过，请根据下方提示处理后重试。", true
	case "polystrata_setup_active":
		return "PolyStrata 安装正在进行中，请等待完成。", true
	case "polystrata_setup_failed":
		return "PolyStrata 安装失败，请查看错误详情。", true
	case "polystrata_setup_timeout":
		return "PolyStrata 安装超时，请重试。", true
	case "polystrata_setup_cancelled":
		return "PolyStrata 安装已取消。", true
	case "polystrata_runtime_invalid":
		return "PolyStrata 运行时环境无效，请重新安装或修复。", true
	case "polystrata_python_unsupported":
		return "未找到兼容的 Python（需 3.10–3.13，推荐 3.12）或 uv，请安装后重试。", true
	case "polystrata_venv_missing":
		return "缺少 PolyStrata 虚拟环境，请先准备环境。", true
	case "polystrata_python_venv_missing":
		return "当前 Python 缺少 venv / ensurepip 模块，无法创建 PolyStrata 虚拟环境。", true
	case "polystrata_venv_invalid":
		return "PolyStrata 虚拟环境无效，请重新准备环境。", true
	case "polystrata_venv_failed":
		return "创建 PolyStrata 虚拟环境失败。", true
	case "polystrata_config_missing", "polystrata_config_required":
		return "缺少 PolyStrata 配置文件。", true
	case "polystrata_config_invalid":
		return "PolyStrata 配置无效，请检查后重试。", true
	case "polystrata_config_model_missing":
		return "PolyStrata 配置缺少模型设置。", true
	case "polystrata_gguf_architecture_unsupported", "polystrata_model_unsupported":
		return "当前模型架构或格式不受 PolyStrata 支持。", true
	case "polystrata_plaintext_credential_rejected", "polystrata_credentialed_env_url":
		return "不支持明文或带凭据的地址，请改为填写环境变量名。", true
	case "polystrata_status_invalid":
		return "PolyStrata 服务状态无效。", true
	case "polystrata_service_unverified":
		return "PolyStrata 服务未通过核验。", true
	case "polystrata_model_not_loaded":
		return "PolyStrata 尚未加载所需模型。", true
	case "polystrata_context_mismatch":
		return "PolyStrata 上下文长度与测评配置不一致。", true
	case "polystrata_platform_unsupported":
		return "当前系统平台不支持此 PolyStrata 配置。", true
	case "polystrata_backend_unsupported":
		return "当前计算后端不支持 PolyStrata。", true
	case "polystrata_gpu_missing":
		return "未检测到可用 GPU。", true
	case "polystrata_nvidia_gpu_missing":
		return "未检测到可用的 NVIDIA GPU。", true
	case "polystrata_amd_gpu_missing":
		return "未检测到可用的 AMD GPU。", true
	case "polystrata_nvidia_driver_too_old":
		return "NVIDIA 驱动版本过旧，请升级驱动。", true
	case "polystrata_nvidia_driver_unverified":
		return "未能核验 NVIDIA 驱动版本。", true
	case "polystrata_macos_too_old":
		return "当前 macOS 版本过旧，不支持此操作。", true
	case "polystrata_xcode_clt_missing", "xcode_clt_missing":
		return "缺少 Xcode Command Line Tools，请先安装。", true
	case "cmake_missing":
		return "缺少 CMake（需 3.24 及以上版本），请先安装。", true
	case "polystrata_build_tools_missing":
		return "缺少构建工具链，请先安装所需编译依赖。", true
	case "polystrata_disk_insufficient", "insufficient_disk_space":
		return "可用磁盘空间不足。", true
	case "polystrata_gguf_path_missing":
		return "找不到指定的 GGUF 模型文件。", true
	case "polystrata_gguf_path_invalid":
		return "GGUF 模型路径无效。", true
	case "polystrata_gguf_unmatched_model":
		return "GGUF 模型文件与当前选择的模型不匹配。", true
	case "polystrata_data_dir_mismatch":
		return "PolyStrata 数据目录与当前配置不一致。", true
	case "polystrata_sibling_migration_risk":
		return "数据目录与相邻目录存在迁移风险，请核对路径。", true
	default:
		return "", false
	}
}

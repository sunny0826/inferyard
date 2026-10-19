# engine-fit 职责与事实源

本文属于 [engine-fit 引擎诊断](engine-fit.md)。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 命令注册 | `engine-fit engines/plan/run/compare/verify` 五个子命令及参数；`--skip-temperature-stop`、`--skip-memory-stop` 与 `--min-free-memory-mib` 构成互斥组 | `add_commands` 在 [`cli/src/inferyard/cli/commands/engine_fit.py`](../../cli/src/inferyard/cli/commands/engine_fit.py) |
| 应用分派 | 按命令名懒加载对应处理器；`run` 的 `PreflightError` 统一映射为退出码 2 的 `blocked` | `execute` 在 [`cli/src/inferyard/application/engine_fit.py`](../../cli/src/inferyard/application/engine_fit.py)，命令清单在 [`cli/src/inferyard/application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py) |
| 能力表 | 离线 JSON 能力表（引擎 ID、平台、资产形态、idle 来源、MLX 脚本路径），不加载实时后端 | `capabilities` 在 [`cli/src/inferyard/config/engine_fit_capabilities.py`](../../cli/src/inferyard/config/engine_fit_capabilities.py) |
| 计划冻结 | 校验输入、冻结诊断文本与参数、计算 `plan_id`；`load_plan` 严格校验并重算计划哈希；`request_rows` 生成初始未执行行 | `prepare`、`validate_plan`、`load_plan`、`request_rows`、`ENGINES` 在 [`cli/src/inferyard/config/engine_fit.py`](../../cli/src/inferyard/config/engine_fit.py)；资产清单 `model_asset_manifest`、`runtime_manifest`、`mlx_server_path`、`mlx_server_sha256` 在 [`cli/src/inferyard/config/engine_fit_assets.py`](../../cli/src/inferyard/config/engine_fit_assets.py) |
| 模型与主机身份 | 目录/GGUF 清单哈希（拒绝目录软链接与特殊文件）、稳定主机指纹（持久化哈希而非明文机器 ID，hostname 不能单独证明同机） | `model_manifest`、`host_identity` 在 [`cli/src/inferyard/platforms/engine_fit.py`](../../cli/src/inferyard/platforms/engine_fit.py) |
| 服务绑定与资源 | 绑定 PID、启动身份、argv 哈希、可执行文件哈希、监听归属与模型资产；`check_service` 逐次重建 binding 比对；`resource_snapshot` 返回进程树 RSS/CPU/主机可用内存及来源说明 | `bind_service`、`check_service`、`resource_snapshot` 在 [`cli/src/inferyard/platforms/engine_fit.py`](../../cli/src/inferyard/platforms/engine_fit.py)；Darwin 分派 [`cli/src/inferyard/platforms/engine_fit_macos.py`](../../cli/src/inferyard/platforms/engine_fit_macos.py)；Windows 分派 [`cli/src/inferyard/platforms/engine_fit_windows.py`](../../cli/src/inferyard/platforms/engine_fit_windows.py) |
| 启动入口解析 | 各引擎唯一合法的启动命令形态、单资产参数白名单、`LLAMA_ARG_` 环境拒绝、MLX 脚本 SHA256 比对 | `startup_binding`、`validate_model`、`linux_llama_environment`、`macos_llama_environment`、`lmstudio_executable`、`reject_llama_environment` 在 [`cli/src/inferyard/platforms/engine_fit_entrypoints.py`](../../cli/src/inferyard/platforms/engine_fit_entrypoints.py) |
| 监听唯一性 | 全局 `lsof` 查询拒绝共享监听（不按 PID 过滤，双栈歧义即拒绝） | `unique_listener` 在 [`cli/src/inferyard/platforms/engine_fit_macos_listener.py`](../../cli/src/inferyard/platforms/engine_fit_macos_listener.py) |
| LM Studio 观察器 | 有界 `lms` 子进程查询（不传 `--host`、显式 `--port`）、唯一已加载实例与 LM Link 禁用核验、模型文件身份观察 | `bind_observer`、`observe`、`LMStudioObserver` 在 [`cli/src/inferyard/platforms/engine_fit_lmstudio.py`](../../cli/src/inferyard/platforms/engine_fit_lmstudio.py)；`verify_model_file`、`lsof_records`、`MAX_PROCESSES` 在 [`cli/src/inferyard/platforms/engine_fit_lmstudio_files.py`](../../cli/src/inferyard/platforms/engine_fit_lmstudio_files.py)；子进程读边界 `read_process`、`TOTAL_TIMEOUT` 在 [`cli/src/inferyard/platforms/engine_fit_observer_io.py`](../../cli/src/inferyard/platforms/engine_fit_observer_io.py) |
| 引擎传输 | 只限 loopback 的有界 SSE 客户端：`inspect` 核验模型 ID 与版本来源，`idle` 核验全服务空闲指标，`complete` 固定 temperature=0 流式请求 | `FitClient`、`FitTransportError` 在 [`cli/src/inferyard/adapters/engine_fit.py`](../../cli/src/inferyard/adapters/engine_fit.py)；`COMMON_METRICS`、`common_service`、`observer_service`、`observer_idle` 在 [`cli/src/inferyard/adapters/engine_fit_common.py`](../../cli/src/inferyard/adapters/engine_fit_common.py) |
| 运行执行 | 平台/引擎准入、主机与资产一致性、主机锁、请求循环、停止 Guard、五终态计数 | `execute`、`Guard` 在 [`cli/src/inferyard/runtime/engine_fit.py`](../../cli/src/inferyard/runtime/engine_fit.py)；`HostLock` 在 [`cli/src/inferyard/runtime/lock.py`](../../cli/src/inferyard/runtime/lock.py) |
| 封存与离线读取 | 封存 `plan.json`、`run.json`、`requests.json`、`report.html` 与 manifest.v2；比较复制已核验来源字节；verify 支持 `--rerender` 与未封存中断读取 | `seal_run`、`compare`、`verify` 在 [`cli/src/inferyard/reporting/engine_fit.py`](../../cli/src/inferyard/reporting/engine_fit.py)；中断检查点 `write_checkpoint`、`read_partial`、`request_counts` 在 [`cli/src/inferyard/evidence/engine_fit_checkpoint.py`](../../cli/src/inferyard/evidence/engine_fit_checkpoint.py) |
| MLX 受控服务 | 操作者在独立 MLX 环境启动的随包脚本；自有队列、`poisoned` 状态与 `/version` 协议绑定 | [`cli/src/inferyard/data/engine_fit/mlx_server.py`](../../cli/src/inferyard/data/engine_fit/mlx_server.py) |

关键状态的事实源：计划与运行的 definition 版本映射（plan.v1–v4 → run.v1–v6）由
`runtime/engine_fit.py` 的 `execute` 按 `platform.system()` 与计划 definition 选择；
五终态计数由 `request_counts` 计算；dirty 状态与恢复 token 由 `HostLock.state` 持有；
测量源码身份 `measurement_source_sha256` 由 `tool_source_hash` 计算并写入 `service`；
`Guard` 按平台选择温度采集器（Linux `LinuxSensors`、macOS `MacSensors`、Windows
`WindowsTemperature`，均只保留 temperature 指标）。这些字段的语义与合法取值以
[engine-fit 契约](../../cli/docs/contracts/engine-fit-contract.md) 为准，本文不复制。


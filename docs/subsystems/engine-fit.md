# engine-fit 引擎诊断

`engine-fit` 记录同一台机器、同一份模型资产在不同引擎上的协议完成情况、客户端耗时、
token 与进程树资源，形成独立诊断产物；不计算正式质量分数或性能赢家。它与
[运行时执行与引擎适配](runtime-execution.md) 共享主机锁、dirty 恢复与原生资源采集机制，
但使用独立的诊断计划定义（`engine_fit_plan.v1`–`v4`）与运行定义
（`engine_fit_run.v1`–`v6`），不复用正式基准的 workload/trial 通路，也不进入正式评分
分母。

字段语义、枚举值与版本规则只维护在
[engine-fit 契约](../../cli/docs/contracts/engine-fit-contract.md)；操作步骤见
[engine-fit 使用指南](../../cli/docs/engine-fit-common.md)；长期理由见
[ADR 012](../../cli/docs/decisions/012-engine-fit.md)、
[ADR 013](../../cli/docs/decisions/013-engine-fit-macos.md)、
[ADR 014](../../cli/docs/decisions/014-macos-common-engines.md)、
[ADR 015](../../cli/docs/decisions/015-lmstudio-local-cli-identity.md)、
[ADR 016](../../cli/docs/decisions/016-engine-fit-temperature-override.md)、
[ADR 019](../../cli/docs/decisions/019-engine-fit-memory-override.md)、
[ADR 020](../../cli/docs/decisions/020-windows-engine-fit-native.md)。

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

## 运行路径

以下路径均在 [通用层职责](../../cli/docs/architecture.md) 约束下执行；跨子系统的锁、
排空与证据规则见 [运行时执行与引擎适配](runtime-execution.md) 与
[证据、封存与离线读取](evidence-storage.md)。

### `engine-fit plan`（离线冻结）

1. CLI 解析参数后由 `application/engine_fit.py` 懒分派到 `config/engine_fit.py` 的
   `prepare`；先做廉价输入校验（case 数量、prompt 大小、token/超时/重复上限、覆盖原因
   合法性），再哈希可能很大的模型权重。
2. `model_asset_manifest`（新计划）或 `model_manifest`（plan.v1 遗留目录计划）生成资产
   清单；`host_identity` 读取 OS 机器身份与硬件描述并计算主机哈希。
3. 按是否使用停止覆盖、资产形态与引擎集合选择 definition（v1–v4），冻结 cases、
   parameters 与 `plan_id`（去除自身字段的 canonical JSON SHA256），经 `validate_plan`
   复核后原子写入 `plan.json` 到新目录。不发送任何请求，也不启动任何服务。

### `engine-fit run`（绑定一个操作者启动的服务）

1. `runtime/engine_fit.py` 的 `execute` 先做平台/引擎准入：Windows 仅 llama-cpp，
   mlx-lm/lmstudio 仅 Darwin，ollama 固定阻断；LM Studio 强制要求 `--lms-path` 与
   `--models-root`，其他引擎拒绝这两个选项。输出目录必须不存在且不得位于模型目录内。
2. `load_plan` 严格校验并重算计划哈希；核对当前 `host_identity` 与计划主机哈希、
   `runtime_manifest` 与计划模型清单一致。
3. 获取 `HostLock`；调用 `bind_service` 建立 binding（平台分派：Linux 走 `/proc`，
   Darwin 走 [`engine_fit_macos.py`](../../cli/src/inferyard/platforms/engine_fit_macos.py)，
   原生 Windows 走
   [`engine_fit_windows.py`](../../cli/src/inferyard/platforms/engine_fit_windows.py)）。
   锁为 dirty 时经 `verify_manual_recovery` 核验恢复 token、说明与新端点；非恢复状态
   携带恢复参数即拒绝。
4. `FitClient` 的 `inspect` 核验 `/v1/models` 的模型 ID 与引擎版本来源，`idle` 核验
   全服务空闲指标；不 idle 即阻断（连接关闭、低 CPU 或 `/health` 成功都不能替代空闲
   证据）。LM Studio 的 inspect/idle 委托 `LMStudioObserver`，执行与观测通过同一已绑定
   服务，不做独立端口自发现。
5. `Guard.check` 在请求前、请求中、请求后采集身份、资源、磁盘、温度与内存读数并按
   计划阈值停止；请求期间的原生检查在独立线程中每 0.5 秒执行，不阻塞 HTTP 截止或
   取消，原生读结束才允许后续检查或封存触碰最近快照。
6. 请求循环按冻结顺序执行（并发固定 1，无隐式重试）：每行先持久化为
   `invalid/request_in_flight` 并标记 dirty，收到完整响应后才改为 `completed`；
   协议完整终止后 `check_service` 复核身份，`_completed_idle` 最多等待 10 秒确认空闲
   指标归零，然后 `lock.clean`；该排空等待不计入请求时限。
7. 结束时按五终态守恒计算 `counts` 并写入 completeness；`seal_run` 封存四份文件与
   manifest.v2（manifest 最后写入，已封存目录不可重复封存）；dirty 未清时在结果
   details 中返回 `recovery_token`。

### `engine-fit compare` 与 `engine-fit verify`（纯离线）

1. `compare` 对两份已封存运行重新核验 manifest 字节与 plan/行语义，要求同计划、同
   测量源码、不同引擎，把已核验来源字节复制到 `sources/<index>` 后新建比较目录；
   原路径关闭或删除后仍可离线核验。
2. `verify` 优先读取最终 manifest；无 manifest 时经 `read_partial` 读取未封存中断的
   最小文件集。通用 `inferyard verify --path` 经 `application/verification.py` 识别
   engine-fit 产物后复用同一验证器，不复制其逻辑。

## 约束与失败行为

- 请求并发固定 1、无隐式重试、temperature=0、顺序与预算冻结；请求 ID 从 `r000001`
  起，发送前把该行持久化为在途状态。五终态（completed/failed/cancelled/invalid/
  not_executed）守恒与字段语义见 [engine-fit 契约](../../cli/docs/contracts/engine-fit-contract.md)，
  此处不复制。
- 凭据只经 `--api-key-env` 环境变量引用；冻结输入（计划、模型名）内出现凭据即拒绝
  （`credential_in_frozen_input`）；CLI 输出只保存 `lms` 白名单字段，argv 与环境实际值
  不落盘。
- 取消映射为退出码 130 与 `cancelled` 终态；传输/预检失败映射为退出码 3 并保留触发
  读数（`stop:<request_id>` 阶段快照）；证据或 IO 错误映射为退出码 4 并保留 dirty。
- 超时或取消后停止发送并按冻结期限排空；无法确认空闲时保留 dirty，恢复须显式 token
  与说明，旧失败目录不覆盖、不删除锁或 state；资源停止后先处理原因，不反复跑满。
- 所有 run/compare 产物 `diagnostic=true`、`performance_comparison_qualified=false`，
  limitations 明确列出客户端耗时、边界采样而非峰值、进程树 RSS 共享页重复计数、
  GPU 内存/能耗未采集等边界；模拟与软件验证不授予真机或正式性能资格（软件缺口不
  归因于硬件，见 [ADR 014](../../cli/docs/decisions/014-macos-common-engines.md)）。
- 旧 definition 的读取语义保持，离线校验按版本分派，不猜测或改写旧数据；不支持的
  旧格式按 [ADR 038](../../cli/docs/decisions/038-inferyard-current-format.md) 拒绝，
  不转换、不覆盖已有证据。

### 各引擎绑定方式

- vLLM / SGLang：绑定模型目录，启动入口须为 `vllm serve DIR` 或
  `python -m vllm.entrypoints.openai.api_server` / `sglang.launch_server` 加唯一
  `--model` / `--model-path`；歧义参数（额外的 `--model`/`--config`、值以 `-` 开头）
  即拒绝。idle 来源为 `/metrics` 全部 running/waiting series，缺失、重复、非有限或
  负值均拒绝。
- llama.cpp：绑定单 GGUF，入口为 `llama-server -m|--model FILE` 且参数限于白名单
  （[`engine_fit_entrypoints.py`](../../cli/src/inferyard/platforms/engine_fit_entrypoints.py)
  的 `_LLAMA_VALUES`/`_LLAMA_FLAGS`），拒绝额外模型、LoRA、draft、mmproj、router、
  HF/URL 下载与未知参数；服务环境出现任何 `LLAMA_ARG_` 变量即阻断，环境值本身不读取
  或持久化。idle 使用 `llamacpp:requests_processing` 与 `llamacpp:requests_deferred`
  均为整数零。
- MLX-LM：仅 macOS，进程入口必须是随包
  [`mlx_server.py`](../../cli/src/inferyard/data/engine_fit/mlx_server.py) 的内容匹配
  脚本（binding 增加 `entrypoint_sha256`），`--model` 指向目录；idle 要求
  `engine_fit:num_requests_running`、`engine_fit:num_requests_waiting` 与
  `engine_fit:poisoned` 三项指标齐全且为零，poisoned 状态在重启前不可声明 idle。
- LM Studio：仅 macOS，绑定唯一已加载本机实例；`lms` 调用固定 `127.0.0.1` 与显式
  `--port`，不传 `--host`（[ADR 015](../../cli/docs/decisions/015-lmstudio-local-cli-identity.md)）；
  `link status` 的 issues 必须含 `deviceDisabled`，实例 `deviceIdentifier` 必须为 null，
  相对模型路径不得逃逸 models_root 且解析后与冻结 GGUF samefile；还须通过
  `verify_model_file` 在稳定服务子树的真实打开/映射文件中观察到冻结 GGUF 的
  device/inode，观察到额外 GGUF 即拒绝。原生绑定检查共享 10 秒总预算，单次 CLI/lsof
  最多 2 秒、输出各最多 1 MiB、服务子树最多 8 个进程（`MAX_PROCESSES`），超限阻断。
- Ollama：缺少完整服务 idle 来源，在运行准入与传输构造两处均固定阻断
  （`ollama_service_idle_observation_unavailable`），不发送模型请求；能力表将其标为
  工具观察缺口而非硬件或模型不兼容。

### 温度与内存停止覆盖

`--skip-temperature-stop` 与 `--skip-memory-stop` 只能在创建计划时冻结原因（非空、
最多 1024 UTF-8 bytes，参与计划哈希），分别产生 plan.v3/run.v4 与 plan.v4/run.v5
（[ADR 016](../../cli/docs/decisions/016-engine-fit-temperature-override.md)、
[ADR 019](../../cli/docs/decisions/019-engine-fit-memory-override.md)）；run 不接受
运行时切换。覆盖时对应阈值为 null，`Guard` 跳过该阈值比较与缺测阻断，但温度、内存
与缺测原因仍采集并保存，HTML 逐项明示转义后的冻结原因；内存覆盖与显式
`--min-free-memory-mib` 互斥，非法原因在资产哈希前拒绝。磁盘、身份、主机锁、dirty、
时限、取消与排空不受覆盖影响，旧计划保持默认 85°C 与内存下限；新旧计划不可混合
比较。

### 平台定义分派

Linux 产物为 run.v1，macOS 为 run.v2（v2 绑定增加 `cwd_sha256`、
`listener_identity`/`listener_source` 且 `listener_inode=null`，来源为
`lsof:TCP:LISTEN:pid`），Windows 单 GGUF llama.cpp 为 run.v6
（[ADR 020](../../cli/docs/decisions/020-windows-engine-fit-native.md)，绑定增加
`process_start_source`，温度来源为 `nvidia-smi` 主机 GPU 读数）；plan.v2/v3/v4 分别
对应 run.v3/v4/v5。字段形状与平台/来源规则见
[engine-fit 契约](../../cli/docs/contracts/engine-fit-contract.md#常用引擎扩展) 的
macOS 与 Windows 原生来源小节。

## 验证

在仓库根 `cd cli` 后执行；测试使用 fixtures 与模拟服务，不发送真实模型请求。

```sh
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_common_plan.py tests/unit/test_engine_fit_plan.py tests/unit/test_engine_fit_common_identity.py tests/unit/test_engine_fit_identity.py tests/unit/test_engine_fit_transport.py tests/unit/test_engine_fit_common_transport.py tests/unit/test_engine_fit_common_evidence.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_temperature_override.py tests/unit/test_engine_fit_memory_override.py tests/unit/test_engine_fit_report.py tests/unit/test_engine_fit_partial.py tests/unit/test_engine_fit_native_evidence.py tests/unit/test_engine_fit_async_guard.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_lmstudio.py tests/unit/test_engine_fit_lmstudio_files.py tests/unit/test_engine_fit_macos_listener.py tests/unit/test_engine_fit_macos_identity.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_mlx_server.py tests/unit/test_engine_fit_mlx_stream.py tests/unit/test_engine_fit_mlx_context.py
mise exec -- uv run --frozen pytest -q tests/unit/test_engine_fit_windows_identity.py tests/unit/test_engine_fit_windows_file_hash.py tests/unit/test_engine_fit_windows_temperature.py tests/unit/test_engine_fit_windows_evidence.py
mise exec -- uv run --frozen pytest -q tests/integration/test_engine_fit.py tests/integration/test_engine_fit_common.py tests/integration/test_engine_fit_macos.py tests/integration/test_engine_fit_temperature_override_flow.py tests/integration/test_engine_fit_memory_override_flow.py tests/integration/test_engine_fit_windows.py
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen inferyard engine-fit engines
```

`engine-fit` 相关测试文件位于 `tests/unit/` 与 `tests/integration/`，按平台与主题分组；
pytest 与 ruff 由 `pyproject.toml` 的 dev 依赖组提供（pytest 8.x、ruff 0.12+，Ruff 目标
Python 3.14、行宽 100）；`inferyard engine-fit engines` 为离线能力表，可作为安装烟测。
集成测试中涉及本地 TCP 模拟的用例需要 socket 权限；沙箱阻塞应与实现失败分开报告。

# 同机引擎适配契约

依据 [ADR 012](../decisions/012-engine-fit.md)。本文件是原 Linux 接口基线；
[ADR 013](../decisions/013-engine-fit-macos.md) 与 [macOS 契约](engine-fit-contract.md#macos-原生来源)
扩展实时平台和 run.v2，其他语义保持。

## CLI 与产物

`engine-fit plan --model DIR --out NEW [--engines vllm sglang] [--prompts JSON]`
冻结默认三条诊断文本或用户提供的 `[{"id":"case", "prompt":"text"}]`，
以及 `--max-tokens`、`--request-timeout`、`--repetitions`、`--min-free-memory-mib`。
规划不发送请求。文件是 `plan.json`，包含模型文件清单和哈希、主机身份、
引擎列表、请求文本和参数、计划哈希与最坏请求时间预算。

`engine-fit run --plan PLAN --engine vllm --endpoint-url URL --server-pid PID
--served-model NAME --out NEW [--api-key-env ENV]` 执行一个冻结引擎。
PID 的启动身份在预检读取并持续核验；参数不接受远程 URL 或任意代码。
`--recovery-confirm TOKEN --recovery-note NOTE` 复用全局 dirty 恢复。
请求并发固定 1，无隐式重试；重复按固定顺序展开。最大请求预算为 1000。
响应和请求日志先于汇总保存；完成、失败、取消、工具错误、未执行五终态守恒。
每次输出到新目录，封存 `plan.json`、`run.json`、`requests.json`、`report.html`。
`requests.json` 是按计划顺序的数组：每项 `request_id, case_id, status, reason,
response`；发送前把该项持久化为 `status=invalid`、`reason=request_in_flight`，收到完整响应才改为 `completed`。
response 为 complete 返回的字典或 null；终态不删项。请求 ID 为 `r000001` 起。

plan 字段固定为 `schema_version, definition, plan_id, model, host, engines, cases,
parameters, request_count, max_request_wall_seconds`；cases 为 id/prompt 列表。
parameters 为 `max_tokens, temperature, repetitions, request_timeout_seconds,
min_free_memory_bytes, min_free_disk_bytes, max_temperature_celsius`。温度阈值固定 85°C，
只使用可得且来源可核验的 Linux thermal zone，缺测保留原因，不推断 GPU 温度。
plan_id 是去除自身字段后 canonical JSON SHA256（含尾部换行）。
run 字段固定为 `schema_version, definition, run_id, plan_id, engine, diagnostic,
performance_comparison_qualified, completeness, stop_reason, binding, service,
resources, counts, limitations`。resources 保存每次请求边界的快照，带 `phase`。
counts 为 `planned, completed, failed, cancelled, invalid, not_executed`。

`engine-fit compare --runs A B --out NEW` 只读封存产物，应同 plan、
同主机和模型，每种引擎最多一份。输出比较 JSON、内嵌可核验来源及自包含 HTML。
`engine-fit verify --path DIR` 离线核验文件哈希、数据语义与按版本要求的呈现；不访问服务。
通用 `verify --path` 也识别这些产物；新封存与中断语义见[离线读取契约](offline-reading-contract.md)。

## 模块接口与责任

共享请求/结果仍只定义在 `application/types.py`。新增请求字段以 `fit_` 开头；
现有 `model_path`、`frozen_plan`、`endpoint_url`、`server_pid`、`out`、
`api_key_env`、`recovery_confirm`、`recovery_note`、`report_runs`、`run` 复用。

`platforms/engine_fit.py`：

- `model_manifest(path: Path) -> dict`：`path, sha256, files`；files 按相对路径排序，
  每项 `path, sha256, size`。允许 HF snapshot 文件软链接，拒绝目录软链接和特殊文件。
- `host_identity() -> dict`：稳定本机指纹 `sha256` 和可读平台/CPU/内存描述；
  不持久化明文机器 ID。同机条件不能由 hostname 单独证明。
- `bind_service(engine, model_path, pid, url) -> dict`：Linux / macOS 原生分派，返回
  `pid, start_ticks, origin, address, port, executable_sha256, argv_sha256, model_binding`。
  另保存 `listener_inode`；model_binding 为 engine/path/device/inode/source 字典。
  启动参数仅用于核验，避免把秘密参数落盘。身份不支持时抛 `PreflightError`。
- `check_service(binding) -> None`：核验 PID 启动身份、监听归属及启动参数不变。
- `resource_snapshot(binding) -> dict`：`memory_available_bytes, process_tree_rss_bytes,
process_tree_cpu_seconds, process_count, scope, missing_reasons`，原生 /proc 来源。
  scope 是带来源、单位和采集范围的字典。运行器快照另附 phase、disk_free_bytes、
  temperature_samples、temperature_missing_reason；阈值停止保存 `stop:请求ID` 的触发读数。

`adapters/engine_fit.py`：

- `FitClient(engine, origin, served_model, api_key=None)` 异步上下文管理器；
  禁止代理环境与重定向，限制响应体大小，严格 JSON，不回显服务错误正文。
- `await inspect() -> dict`：核验 `/v1/models` 的模型 ID 和引擎版本端点，返回
  `engine, version, served_model, version_source`；vLLM `/version`，
  SGLang `/get_server_info`。不把服务自报版本视为完整安装包哈希。
- `await idle() -> dict`：核验 `/metrics` 中全部 running/waiting series 为零；
  缺失、重复、非有限/负值拒绝；返回 `idle, source, values`。
- `await complete(prompt, max_tokens, timeout_seconds) -> dict`：OpenAI SSE 请求，
  固定 temperature=0、stream=true、stream_options.include_usage=true；
  返回 `text, finish_reason, elapsed_ms, first_content_ms, prompt_tokens,
completion_tokens, usage_missing_reason`。总截止时间、finish + [DONE] 双终止，
  usage 独立于文本，不把网络 chunk 当 token。`FitTransportError` 携带固定 `reason`。

`config/engine_fit.py` 管理计划；`runtime/engine_fit.py` 管理锁、请求与停止；
`reporting/engine_fit.py` 管理封存、重建、比较、HTML；各文件宜小于 500 行。

计划入口 `load_plan(path) -> dict` 严格校验并重算计划哈希；
`request_rows(plan) -> list[dict]` 按 repetitions 外层、cases 内层产生初始未执行行。
service 另保存 measurement_source_sha256 和 idle_before；每个请求前后快照附 idle
的原始指标值与来源。正常协议完成后最多等待 10 秒让空闲指标刷新；未完整终止时
不进入这条清理路径。请求时间预算不含模型哈希、预检、写盘和此排空等待。
报告入口 `seal_run(out, plan, run, rows) -> dict` 封存已经存在的新运行目录；
`verify(path) -> dict` 返回经校验产物；`compare(paths, out) -> dict` 新建比较目录。
比较复制已核验来源到 `sources/<index>`，关闭服务及删除原路径后仍可核验。
每个运行 manifest 为本目录四个固定文件的 SHA256，不读取文件声明的任意路径。

## 限制与验收

所有 run/compare 标 `diagnostic=true`、`performance_comparison_qualified=false`。
耗时为客户端观察，资源为采样观察，token 为端点自报；不推导 GPU 驻留或整机能耗。
只允许并列适配观察，不因 unknown 相等宣称有效参数等价。
语义校验拒绝重复 JSON 键、bool 冒充数值、非有限数、破坏计划哈希和五终态。
历史 v3 核心产物继续由原入口读取；不转换/覆盖已有证据。

## 常用引擎扩展

依据 [ADR 014](../decisions/014-macos-common-engines.md)，继承原 engine-fit 与 macOS 契约。
本页既有 run.v1–v5 语义仍适用于原 Linux/macOS 路径。
[ADR 020](../decisions/020-windows-engine-fit-native.md) 单独增加 Windows run.v6，
接受同一 plan.v2/v3/v4 的单 GGUF llama.cpp 诊断；旧运行定义不接受 Windows。
Windows 原生来源与额外字段见[独立契约](engine-fit-contract.md#windows-原生来源)。

### 显式温度覆盖（ADR 016）

[ADR 016](../decisions/016-engine-fit-temperature-override.md) 追加 plan 选项
`--skip-temperature-stop REASON`，映射共享请求 `fit_temperature_stop_override_reason: str | None`。
默认 None；非空原因最多 1024 UTF-8 bytes，生成 plan.v3，model 沿用 v2 的 kind。
v3 parameters 严格增加 `temperature_stop_override_reason`，并强制
`max_temperature_celsius=null`。v1/v2 禁止此字段且仍只接受 85°C。
v3 不接受空白、非字符串、超长原因或数值阈值；哈希含完整原因与停止规则。

plan.v3 仅对应 run.v4；v4 字段形状沿用 v3，platform 与来源规则相同，limitations
应含 `temperature_stop_explicitly_disabled`。不覆盖旧 definition 的读取语义。
旧运行禁止该覆盖标记；v4 的摘要和逐请求 reason 均禁止温度阈值停止原因。
Guard 仍采集温度，仅 null 时跳过温度阈值比较；内存、磁盘、身份、预算和 dirty 不变。
run/compare 的 HTML 顶部显示“本次已显式跳过温度停止”及转义后的冻结原因，
旧计划生成的 HTML 字节保持原样。comparison.v1 的来源可为新 run.v4，但仍要求
两个来源完整计划及测量源码一致，不把新旧计划混合比较。

### CLI 与共享数据

[ADR 019](../decisions/019-engine-fit-memory-override.md) 增加 plan-only
`--skip-memory-stop REASON`，映射 `CommandRequest.fit_memory_stop_override_reason: str | None`，
默认 None。CLI 与显式 `--min-free-memory-mib` 互斥；非法原因在资产哈希前拒绝。
仅使用内存覆盖时生成 plan.v4/run.v5；未使用它时保持全部旧版本选择规则。

plan.v4 模型/顶层字段沿用 v3，parameters 严格包含旧基础字段及两个覆盖原因：
`memory_stop_override_reason` 为非空字符串，最多 1024 UTF-8 bytes，
`min_free_memory_bytes` 应为 null；`temperature_stop_override_reason` 可为 null，
此时温度阈值应为整数 85，或为同样规则的字符串，此时阈值应为 null。
v1/v2/v3 禁止内存原因和 null 内存下限；v3 温度原因仍应非空。
完整原因和停止条件参与计划哈希，不猜测或归一化原因。

run.v5 形状沿用 v4，严格对应 plan.v4，应含
`memory_stop_explicitly_disabled`；温度标记存在与否应与计划一致。
旧运行禁止内存覆盖标记。内存覆盖禁止摘要/逐请求 reason 为
`memory_safety_threshold_reached` 或 `system_memory_unavailable`；温度覆盖继续禁止
`temperature_safety_threshold_reached`。Guard 在内存下限 null 时跳过内存阈值与缺测阻断，
仍采集并保存 null/缺测原因；磁盘、身份、主机锁、dirty、时限、取消和排空不受影响。
HTML 逐项显示覆盖及转义原因；v1–v4 旧运行的渲染字节保持。
comparison.v1 允许两份同计划 run.v5，仍拒绝混合计划、原因或测量源码。

- `engine-fit engines`：离线 JSON 能力表，无实时后端加载。引擎 ID 为
  vllm、sglang、llama-cpp、mlx-lm、lmstudio、ollama；最后一个执行阻断。
- plan 的 `--model` 接受目录或单 GGUF。新增引擎或单文件产生 plan.v2；
  原两引擎目录计划仍是 v1。v1 引擎枚举保持原样，不能改名读入新字段。
- plan.v2 的 model 在原 path/sha256/files 上增加 kind=directory|gguf。
  单文件 files 仅一项，path 为文件名；sha256 仍是 files canonical JSON 哈希。
  GGUF 元数据 split.count 应缺失或整数 1，split.no 应缺失或整数 0；分片拒绝。
  gguf 仅允许 llama-cpp/lmstudio/ollama；目录仅允许 vllm/sglang/mlx-lm。
- LM Studio run 增加 `--lms-path FILE`、`--models-root DIR`，映射共享请求
  `fit_lms_path`、`fit_models_root`。其他引擎拒绝这两个选项，不能静默忽略。
- 新计划运行定义 run.v3，在原 run 字段新增 platform，等于 plan.host.platform。
  v3 的 service 增加 version_missing_reason；version 为 null 时应有原因，
  不支持版本来源为 not_exposed。旧定义字段集与读取语义保持。

### 身份接口

bind_service 保留原参数，新增可选关键字 `lms_path/models_root/served_model`。
check_service 应重建同一 binding；所有引擎保留 PID/start/exe/argv/监听独占核验。
macOS extended binding 继续含 cwd_sha256、listener_inode=null 与 listener_identity/source。

[ADR037 的离线契约](offline-reading-contract.md) 去除与上述身份无关的 executable
basename 限定；下面入口名表示启动协议，允许同内容核验通路下的重命名二进制。
MLX 脚本摘要、模型加载路径和 LM Studio app/helper 发现约束保留。
新 manifest.v2 封存保存字节；旧 manifest.v1 仍重渲染核验。未封存中断的最小读取文件、
partial 状态与损坏拒绝亦见该契约。

llama-cpp 的入口为 `llama-server -m|--model` 本地单 GGUF；拒绝额外模型、draft、LoRA、
mmproj、router、HF/URL 下载与歧义参数。model_binding 保留原 engine/path/device/inode/source
字段集，source=verified_startup_file_identity。CPU/RSS 使用既有原生进程树。
读取服务进程环境并拒绝全部 LLAMA_ARG_ 配置变量，避免 RPC、mmproj 或其他资产从环境
绕过启动参数核验；不可读取则阻断。环境实际值不输出或持久化，操作者改用显式参数。

mlx-lm 仅接受 Python 执行随包 `data/engine_fit/mlx_server.py`（允许内容相同的副本），
--model DIR。脚本 SHA256 应匹配随包资源，binding 增加 entrypoint_sha256；
模型 source 仍为 verified_startup_directory_identity。不接受原版 mlx_lm.server
假装已具备队列观察能力。仅 macOS。

lmstudio 仅 macOS，同用户的 LM Studio/llmster 监听进程。
model_binding.source=lms_loaded_instance_path；path 为冻结的绝对 GGUF 路径。
binding 增加 `observer={kind:lms,path:绝对CLI路径,sha256,models_root:绝对目录,instance_id:模型identifier}`。每次重新验证 CLI 哈希、实例和本地资产。
依据 [ADR 015](../decisions/015-lmstudio-local-cli-identity.md)，地址固定为 127.0.0.1；
`lms ps --json` 和 `link status --json` 应显式 `--port PORT`，不传 `--host`，使用本机
CLI 身份且不触发 findOrStartLlmster。非 127.0.0.1 或非法/缺省端口在任何 CLI
调用前阻断。仅一个已加载 llm，identifier 匹配 served_model，
deviceIdentifier 显式 null；相对模型 path 不得逃逸 root，解析后与冻结文件 samefile。
LM Link issues 应包含 deviceDisabled；未知结构/权限/远程/多实例均拒绝。
用户提供的 models_root 不是服务目录证据：还应在绑定服务及稳定后代进程的真实打开/
映射文件中观察到冻结 GGUF 的 device/inode，确认 lms 相对路径没有被拼到另一份同名资产。
无法观察文件时阻断，不补造已加载证明。
已观察到的额外 GGUF 映射或打开文件也拒绝；完整有效加载配置与非 GGUF 附加资产
仍未独立核验，并写入运行限制。原生绑定检查共享 10 秒总预算，CLI/lsof 单次最多
2 秒、合计输出最多 1 MiB，服务子树最多 8 个进程；超限阻断。
版本不从 CLI commit 推断。CLI 输出只保存白名单字段。

platforms/engine_fit_lmstudio.py 提供 LMStudioObserver(binding)，异步 inspect()/idle()；
inspect 返回 engine/version(null)/served_model/version_source(not_exposed)/version_missing_reason。
idle 返回 `{idle,source:lms:ps,values:[{metric:lmstudio:queued,labels:{instance:ID},value:N},{metric:lmstudio:active,labels:{instance:ID},value:0或1}]}`，active 由处理状态映射。
状态仅 idle/processingPrompt/generating/computingEmbedding，queued 为非负整数。
执行与观测通过同一已绑定服务，不使用独立自发现端口。

### 传输接口

FitClient(engine,origin,served_model,api_key=None,*,transport=None,observer=None)。
保留 bounded SSE、完整 finish+[DONE]、无重试、凭据脱敏与固定 deadline。
llama-cpp inspect 读取单模型 /v1/models 与 /props.build_info；idle 使用
llamacpp:requests_processing 与 llamacpp:requests_deferred，应均为整数零。
MLX 受控服务暴露单模型 /v1/models、/version、/metrics；inspect 检查
engine_fit_protocol=mlx-lm.v1 与 engine_fit_source_sha256 匹配随包脚本。
指标为 engine_fit:num_requests_running、engine_fit:num_requests_waiting、
engine_fit:poisoned；任一非零即不 idle，三项都必需。
LM Studio inspect 在 OpenAI /v1/models 中核验目标 ID 唯一存在（候选列表可有其他模型），
然后使用 observer 的加载实例核验；idle 委托 observer。没有 observer 则拒绝。
Ollama 返回固定 ollama_service_idle_observation_unavailable，不发送模型请求。

### MLX 受控服务

`data/engine_fit/mlx_server.py` 是独立 Python 3.12+ 脚本，操作者在自己的 MLX 环境启动。
不导入测评器 Python 3.14 模块，不自动下载。参数 `--model DIR --host 127.0.0.1
--port PORT --served-model-name NAME [--api-key-env ENV]`；只监听 loopback。
使用公开 mlx_lm.load/stream_generate、make_sampler(temp=0) 与 mx.synchronize。
只接受固定模型、单条文本 chat，输出 token 与队列容量有界；不能动态换模/工具/图片。
自有队列/锁覆盖所有排队与活动生成；只有生成器关闭且同步完成后才能清零。
异常/未确定的同步结果置 poisoned，重启前不可声明 idle；客户端断开不能提前清零。
metrics/version/models 的只读请求不得等待生成锁。认证失败不进入生成队列。
load/依赖失败即启动失败，不回退到远端模型 ID。测试可注入合成后端，应明确标识。

### 证据与安全

比较仍要求完全相同 plan/资产/主机/测量源码及不同引擎，保留原两份输入规则。
idle 校验按定义/引擎分派，未知来源拒绝；版本缺测明确 null+原因。
默认温控仍为 85°C；仅新 plan.v3 的显式覆盖按 ADR 016 执行。
内存、磁盘、HostLock、dirty 与人工新进程恢复不变。
请求期间原生检查在线程中执行，HTTP 超时和取消保持响应；先关闭 HTTP，再等待
同一个有界原生检查结束才封存证据。请求时限不包括原生观察收尾、排空或封存时间。
所有新增引擎仍是诊断，性能资格 false；软件缺口、未测硬件与模型失败分别报告。


## macOS 原生来源

依据 [ADR 013](../decisions/013-engine-fit-macos.md)，在公共诊断规则上增加原生来源。
CLI、计划 v1、比较 v1 与封存目录格式保持不变。

### 运行与身份

macOS 输出 `schema_version=3, definition=engine_fit_run.v2`；Linux 仍输出 v1。
v2 要求冻结计划 host.platform 为 Darwin。
离线校验明确分派 v1/v2，不猜测或改写旧数据。v2 只接受以下 macOS binding：
保留 v1 的 pid/start_ticks/origin/address/port/executable_sha256/argv_sha256/model_binding，
`listener_inode` 应为 null，新增 `listener_identity`、`listener_source`、`cwd_sha256`。
cwd_sha256 为规范化绝对 cwd 路径的 `os.fsencode` 字节 SHA256（不含尾 NUL），不暴露路径。
source 应是 `lsof:TCP:LISTEN:pid`，identity 应匹配
`macos:tcp:<address>:<port>:pid:<pid>`。v1 继续只接受原来 numeric inode 形状。
启动身份是原始内核单调启动时间整数微秒。模型目录按 device/inode 核验。
同用户 PID、可执行文件当前映射、原始 argv 哈希、cwd、模型入口、全局端口唯一监听归属
在绑定与后续检查中核验；任何变化或权限不明阻断，不持久化 argv 和凭据。

### 平台接口

`platforms/engine_fit.py` 的 bind_service/check_service/resource_snapshot 保持签名。
Darwin 且 proc_root 为默认 `/proc` 时延迟分派 `engine_fit_macos.py`；自定义 proc_root
继续走 Linux fixture。macOS 模块可复用既有 identity/macOS 原生 helper。
resource_snapshot 返回原来的字段集：host 可用内存 bytes、进程树 RSS bytes、
CPU user+system seconds、process_count、scope、missing_reasons。
scope 应明确 psutil 来源、observed_live_descendants、排除退出进程与 RSS 共享页重复。
启动身份、父子关系和子集合读取前后校验，任一成员变化/权限失败整棵树缺测；
主机可用内存单独保留。不能将主 PID 指标冒充进程树。

### 温度与证据

Linux 保留 linux-sensors.v2 / thermal_zone_reported，整数 raw 毫摄氏度乘 0.001。
macOS 使用 macos-smc.v1 / smc_key_reported，AppleSMC: 来源，raw=value 摄氏度，
有限且范围 -273.15 到 200；两种来源均保持缺测原因与单调读取时间。
v1 仅接受 Linux 温度来源，v2 仅接受 macOS 温度来源，不混淆单位。
运行器按平台选择传感器，85°C、内存/磁盘阈值与 dirty 恢复规则不变。
新记录不能声称已独立核验目标进程架构、Metal/MLX 后端、GPU 内存或已退出子进程资源。


## Windows 原生来源

依据 [ADR 020](../decisions/020-windows-engine-fit-native.md)，继承
[常用引擎契约](engine-fit-contract.md#常用引擎扩展)。核心 schema_version 仍为 3。

### 运行定义

新增 run.v6；字段集合沿用 run.v5，platform 应为 Windows 并等于 plan.host.platform。
只允许 engine=llama-cpp、model.kind=gguf、plan.v2/v3/v4。
停止覆盖标记、禁止原因及参数分别遵循对应计划；默认计划仍保持原默认停止。
run.v1–v5 平台/形状/来源规则不变，不能把 Windows 产物重新标为旧版本。
service 继续使用 /v1/models、/props 与 /metrics 的现有严格协议，版本缺测不能猜测。

Windows binding 为既有 pid/start_ticks/origin/address/port/executable_sha256/
argv_sha256/listener_inode/model_binding，加 cwd_sha256、listener_identity、
listener_source、process_start_source。
listener_inode 应 null；listener_source=GetExtendedTcpTable:owner_pid；
listener_identity 为 windows:tcp:ADDRESS:PORT:pid:PID；
process_start_source=GetProcessTimes:creation_FILETIME_100ns_since_1601。
model_binding 保留 engine/path/device/inode/source；source=verified_startup_file_identity。
device 是 0 至 2^64−1 的无符号卷标识，inode 是 0 至 2^128−1 的文件标识；应为真正整数，
不得经浮点数或有符号截断。旧运行定义继续保持其既有整数范围。
argv/cwd 只持久化哈希，账户及环境只用于当次核验。

### 身份与资源

platforms/engine_fit_windows.py 提供 bind_service 与 resource_snapshot；
现有平台分派仅在原生 Windows 且默认 proc_root 时进入它。
bind_service 只接受明确 `llama-server.exe -m|--model FILE`，执行文件与 argv[0]
均应匹配；参数白名单及单资产规则沿用 llama.cpp。Windows 参数和环境名的大小写
语义分别处理：环境配置名按不区分大小写拒绝，参数保持引擎实际的大小写。
exe 路径、账户、创建 FILETIME、argv、cwd、模型文件和唯一监听在绑定末尾重新核验。
服务管理仍由外部操作者执行。

稳定文件哈希在 Windows 统一使用 GetFileInformationByHandleEx，分别打开路径和
待哈希句柄，前后比较相同来源的卷号、完整 128 位文件 ID、原生属性、大小、创建时间、
写入时间及真实 ChangeTime（FILETIME 100ns）。只排除会因读取变化的访问时间。
Python 路径 stat/lstat 前后另作原始 stamp 对比，不跨 API 比较 mode/ctime。
目录、重解析点、待删除文件及任何原生查询失败均拒绝，不回退到截断 ID 或创建时间。
来源依据 [原生查询与信息类](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-getfileinformationbyhandleex)、
[文件基本信息和 ChangeTime](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_basic_info)及
[完整文件标识](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)。

进程树从 psutil.Process.children 获得，逐成员读取准确创建 FILETIME、父 PID、CPU
user+system 秒及 working-set RSS bytes；二次读取创建时间/父子关系，CPU 不得回退。
树最多 256 个活进程，身份/资源观察检查共享 10 秒预算；超限与读取失败保留 null 和原因。
memory_available_bytes 来自 GlobalMemoryStatusEx 的主机可用物理内存。
scope 应写入这些 Windows 来源及 FILETIME 单位，不能套用 `/proc` 或 Darwin 来源。

### 温度与执行

Windows 温度 collector=windows-nvidia-temperature.v1，semantics=gpu_temperature_reported，
unit=celsius，raw_value/value 为相等的有限数值，范围 -273.15 至 200。
source 为 nvidia-smi:gpu:INDEX:temperature.gpu，明确只归属主机 GPU；不声称模型温度。
缺少可核验工具、设备或读数时 raw_value/value=null，保存明确 missing_reason；
没有可确认设备时 source=nvidia-smi:unavailable，不能猜测设备序号。
读数有单调 ns 读取起止，server_pid/process_start_ticks/request_id 保持 null。
采集进程只运行只读查询，有 2 秒期限及 64 KiB 输出界限；CPU 温度不使用 ACPI 替代。

Guard 在 Windows 选择该采集器，默认/显式覆盖的停止比较仍使用计划值。
主机锁、dirty、请求冻结顺序、时限、连接关闭和排空不变；失败连接不自动 clean。
run.v6 仍 diagnostic=true、performance_comparison_qualified=false，限制明确当前磁盘
exe 哈希不证明加载字节、Windows CPU 温度缺测及 GPU 驻留未独立核验。
比较仍要求完全相同计划和测量源码；未接入的其他 Windows 引擎在发送前明确拒绝。


## MLX 总序列与线程

依据 [ADR 017](../decisions/017-macos-hf-three-engines.md)，活动核心 schema 仍为 3。

### 接口与边界

- 受控 `mlx_server.py` 接受可选 `--max-model-len N`，N 为 1 至 32768 的整数；
  未指定保留原输入上限。指定时 `prompt_tokens + max_tokens > N` 在生成前拒绝。
- MLX `/version` 保持 `mlx-lm.v1`、版本和脚本 SHA 绑定，可附上下文上限诊断字段。
  进程入口解析只增加此明确参数；重复、非法、未知参数继续拒绝。
- 不修改 plan/run 类型、CLI 生命周期或 idle/poisoned 语义。所有请求仍只有 temperature=0。
- MLX 显式使用公开线程本地 stream；主线程加载完成应同步，worker 的生成和同步
  使用其本线程 stream，不把普通线程所属 stream 传到另一线程。失败仍保留 poisoned。
- 第三方依赖、权重和服务由已获安装授权的操作者外部准备；环境与资产在忽略目录，
  测评器 Python 3.14 和根锁文件不加入引擎依赖。引擎逐一启动、运行、排空、关闭。

### 拒绝与证据

MLX 超总序列上限是客户端输入错误，不能启动生成器。准备阶段计入 waiting；
拒绝后撤回该请求的 waiting 预约，不改变其他活动或排队计数。输入拒绝本身不置
poisoned。客户端超时期间不能把仍在准备的请求观察为 idle。
第三方缺指标、启动失败或身份不匹配为软件/环境问题，保留日志与失败目录；
不放宽验证器。真实运行完成不自动授予质量评分、全平台支持或正式性能资格。

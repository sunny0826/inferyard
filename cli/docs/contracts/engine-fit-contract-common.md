# 同机引擎常用扩展

本文是 [engine-fit 契约](engine-fit-contract.md) 的常用引擎扩展。
基线见 [基线契约](engine-fit-contract-base.md)，平台来源见 [平台契约](engine-fit-contract-platforms.md)。

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
不导入测评器 Python 3.15 模块，不自动下载。参数 `--model DIR --host 127.0.0.1
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


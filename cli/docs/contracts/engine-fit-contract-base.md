# 同机引擎适配基线

本文是 [engine-fit 契约](engine-fit-contract.md) 的原 Linux 接口基线。
常用引擎见 [扩展契约](engine-fit-contract-common.md)，平台来源见 [平台契约](engine-fit-contract-platforms.md)。

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

# 跨引擎受控比较契约

本契约对应 [ADR 056](../decisions/056-engine-comparison.md)。
实施进度见[实施计划](../plans/engine-comparison.md)。

## 命令

- `compare --mode engine --left RUN --right RUN`：只做单对身份门槛，
  始终阻断 `engine_verdict_requires_block_comparison`，不给出性能资格。
- `compare-engines --left-runs RUN... --right-runs RUN... --out DIR`：
  读取全部块，在新目录写入 `engine-comparison.json`（`engine-comparison.v1`）与离线
  `report.html`，再以 `artifact-manifest.json`（`presentation-seal.v1`）封存两者字节；
  stdout 输出同一结果。输出目录必须不存在，且不能位于任何输入 run 内。
- `compare-plan --left-config CFG [--left-config CFG]... --right-config CFG [--right-config CFG]... [--bundle NAME_OR_PATH]... (--out DIR | --dry-run)`：
  离线冻结一份 `engine-comparison-plan.v1`（现有 plan JSON 的受限形状），stdout 输出计划。
  不连接或启动模型服务。默认题包为 `zh-core` 与 `zh-svg-pelican`；单题包必须由
  `--bundle` 显式声明。输出目录必须不存在。
- `engine-comparison.json` 的 `source_runs[].path` 相对输出目录；`producer` 记录
  `tool_source_sha256` 与 `template_sha256`，只作来源，不作核验依据。
- `verify --path DIR` 按 `engine-comparison.json` 识别该产物：先核对封存字节，再按
  `source_runs` 读取原 run 重算全部结果并逐字节比对（`--source-root OLD=NEW` 定位已移动的来源）。
  字节改动为 `presentation_bytes_changed`，重算不符为 `engine_comparison_recomputation_mismatch`；
  `--rerender` 另核对 HTML，不符为 `engine_comparison_html_recomputation_mismatch`。
  HTML 不含脚本与外部资源。

## `engine-comparison-plan.v1`

它是现有冻结 plan 的受限形状，不是第二份计划文档：

- `experiment.comparison = {"mode": "engine", "factor": null}`。
- 恰好 6 个块，块号 1..6；奇数块为 `a` 侧、偶数块为 `b` 侧，两侧交错。
- 每块包含同一组题包（默认 `core` 与 `pelican`）。槽位 `workload_id` 固定为
  `block-NN-[ab]-<bundle>`，每槽位 `repeats = 1`、固定题序，trial 顺序与块顺序一致。
- 每个槽位引用本侧、本题包的 config 与 bundle；同侧同题包各块必须引用同一 config 摘要，
  同题包各块必须引用同一 bundle 摘要。
- 每个被引用 config 的 `execution.require_fresh_process = true`（不是
  `experiment.execution`）；`compare-plan` 写入冻结副本，不修改源配置。
- `plan_sha256` 仍按现有 canonical JSON 计算；核心 `schema_version = 3` 与 run 字段不变。
- `compare-plan` 在写计划前拒绝：无效配置、两侧题包覆盖不一致、两侧题包内容不同、
  两侧引擎 profile 相同、输出目录已存在。它不判断真实服务身份。

## `engine-comparison.v2`（T2a 仅内部构造）

本阶段只实现内部读取与阻断数据；`compare-engines` 的公开派生仍输出 v1，
v2 HTML、封存与 `verify` 由 T2b 交付。读取规则：

- 先核验完整 batch，再按冻结槽位选择 run；从不按 `first_event_utc` 重排槽位。
- v2 reader 与离线 verify 复用同一 `batch.json` 严格准入：整数 `schema_version=3`、
  布尔 `diagnostic`、完整身份/工具摘要与计划哈希；非法输入返回 `batch_envelope_invalid`，
  `eligible=false`。
- layout 1 旧 run 禁用 v2 身份回退；只有 layout 2 已验证 envelope 可提供
  `trial_id`/`run_id`。
- 每个 run 必须绑定同一完整 `plan_sha256`、experiment 与 definition versions。
- 交接链读取各 run 已封存的 `service_binding.previous_run_id`：首槽位必须为 null；
  后续必须精确等于前一冻结槽位选中 run 的 `run_id`。
- 完整 batch 核验后，按题包拆成各自的 A/B 块，再复用既有六块判定；
  不把 12 个 run 直接交给 v1 逻辑。

规范阻断原因（每种条件只保留一个名字）：

| 原因 | 条件 |
| --- | --- |
| `comparison_plan_required` | 计划不是上述受限形状，或缺少可用 plan |
| `comparison_plan_hash_mismatch` | batch 冻结计划哈希与提交计划不同 |
| `planned_run_missing` | 冻结槽位没有可用 run（错计划/重复尝试单独报） |
| `comparison_unplanned_trial` | batch 内存在计划外 trial；`unplanned_run_present` 不再单独输出；`run_set` 与 diagnostic 保留 `detail` 及底层 error |
| `run_plan_mismatch` | run 绑定的计划哈希、experiment 或 definition versions 不同 |
| `comparison_duplicate_trial_attempt` | 同 trial 多根、多次尝试或重复 run id |
| `comparison_handoff_missing` | 缺已封存交接观测、字段缺失，或应有前驱为空 |
| `comparison_handoff_broken` | 非首槽位前驱不是前一槽位选中 run（含跳槽位、跨 batch） |
| `comparison_handoff_slot_mismatch` | 首槽位存在前驱 |
| `comparison_history_identity_error` | 证据、计划、trial、run 或身份核验失败；含成功后缺槽位仍有后续 run，以及无法归属计划槽位的拒绝污染整个 batch |
| `comparison_history_cancelled` | `stop_reason = user_cancelled` 或存在 cancelled 终态 |
| `comparison_history_failed` | 其他非 `plan_finished` 停止，或存在 failed/invalid/not_executed |
| `comparison_history_incomplete` | 全部请求 completed 但 scope/evidence 不完整 |

`execution_order_differs` 不输出：v2 只按冻结槽位顺序读取，实际连续性由交接链证明；
事件时间不参与选择。缺槽位或错计划时不重复报 `planned_run_missing`。

## 比较专用历史停止

`comparison_history_stop(plan, selected_runs, rejections=())` 是执行入口与 v2 共用的唯一 CLI 判定来源：

- 按冻结槽位顺序返回首个 `{trial_id, run_id, reason, detail}`；无停止返回 null。
- 取消、执行失败、身份错误或成功后缺槽位却已有后续 run 时阻断，不再派发后续槽位。
- 槽位级停止（rejection、terminal、缺槽且已有后续 run）优先于 batch 兜底；
  batch 污染只在遍历完所有冻结槽位后仍无槽位级停止时返回。
- 计划外或 trial 为 null 的拒绝无可归属槽位时，整个 batch 阻断：
  `comparison_history_identity_error` / `rejection_outside_planned_slots`，保留已知
  trial/run/error；`run` 与 `resume` 同样停止。
- 被拒绝的 trial 不得因同一 trial 的另一尝试可用而重新进入 selected runs、handoff 或指标。
- 同一 `run_id` 的成功、拒绝与身份回退读取都参与重复登记；关联的全部 trial 不进入
  selected runs、handoff 或指标。reader 与 runset 每个路径只留一条重复证据，
  原始拒绝原因与重复证据并存。
- 直接 `evaluate_comparison` 与 reader 共用停止判定：重复拒绝参与停止，取最早被拒槽位，
  detail 为重复根原因。
- 被拒绝的 run 保留已读取的 trial/run 身份与底层错误；确实无法证明 trial 的损坏记录不猜测归属。
- 无 slot 可归属且不属于规范列表的拒绝原因统一为 `comparison_history_identity_error`，
  不把 `EvidenceError` 等底层异常名复制到顶层 classification。
- 后续污染 run 附在最早停止记录中，不改变其 trial/run/reason/detail；停止后的缺失尾部不单独报告。
- 普通 batch history 校验 run/selection 的 schema relation 与 parent 一致性；comparison diagnostic history 在此之上保留拒绝分类。

v2 的分类与共享停止粒度映射：

| v2 分类 | 共享停止 | 说明 |
| --- | --- | --- |
| `comparison_unplanned_trial` | `comparison_history_identity_error`，detail 为 `rejection_outside_planned_slots` | run 的 trial 不属于冻结计划；保留 trial/run/path 与底层 error，不能猜测归属到某个计划槽位 |
| `run_plan_mismatch` | `comparison_history_identity_error`，detail 为 `run_plan_mismatch` | run 已绑定计划槽位，但计划哈希、experiment 或 definition versions 不匹配 |
| `comparison_duplicate_trial_attempt` | `comparison_history_identity_error`，detail 为重复根原因或 `duplicate_run_id` | 同 trial 多根或跨 trial 重复 run id；共享该 run_id 的所有 trial 都不进入 selected runs、handoff 或指标 |
- 完整成功要求 scope/evidence complete 且全部请求终态为 completed；
  模型答错但请求 completed、或 `unscorable` 不按执行失败处理。
- 正常 `awaiting_handoff` 不是 run 终态，不触发停止。
- `resume`、普通 `run --plan` 遇到比较停止时返回 `comparison_history_stopped`；
  `resume` 不能借同一 trial 的第二次尝试追加槽位。
- 普通计划、`resume` 与长上下文专项不匹配上述受限形状，保持原语义。

## 冻结调优与温度停止覆盖

见[调优与温度覆盖](engine-comparison-tuning.md)。

## 身份门槛（每个配对块）

- 两侧都必须是 `local-engine-config.v1`（`engine_mode_requires_local_profile`）。
- 家族必须不同（`engine_family_did_not_change`），且组合受支持；
  当前支持 llama-cpp 与 lmstudio（同资产）、llama-cpp 与 mlx-lm（跨格式）
  （其他组合 `unsupported_engine_pair`）。
- 同资产组合：两侧 `model.kind` 都是 `gguf`（`engine_mode_requires_single_gguf`），
  `content_sha256` 与 `assets_sha256` 相同。
- 跨格式组合：`config.model` 的 kind、content/assets 摘要、repo、revision、sha256、
  packing、template_sha256 允许不同。
- 允许不同：`config.engine.family`、`installation_sha256`、`startup_args`。
- 本地 profile 不填写的旧字段不参与比较，改由替代字段核验，替代字段缺失时仍阻断：
  `release`、`binary_sha256`、`identity.runtime_library_hashes` 由 `installation_sha256`
  （含运行时闭包核验）代替；`config.model.sha256` 由 `content_sha256`／`assets_sha256`
  代替；`config.conditions.cache_policy`（本地 schema 固定为 `unknown`）由逐请求冷缓存门槛代替。
- 原生服务不报告实际生成参数，本模式不逐项核对 effective 参数，
  只要求冻结的 `config.generation` 相同，并记录限制
  `effective_parameters_not_verified_native`。
- 其余条件沿用通用比较规则；其他模式下本地 profile 仍阻断
  `local_engine_controlled_comparison_not_supported`。

## 块结构

块按 `events.jsonl` 首事件 UTC 排序，取首块家族为 left。

| 阻断原因 | 条件 |
| --- | --- |
| `block_start_time_unknown` | 任一块缺首事件时间 |
| `blocks_not_interleaved` | 相邻两块家族相同 |
| `insufficient_blocks` | 两侧块数不等或少于 3 |
| `side_configuration_changed` | 同侧各块的 model/engine/generation/conditions/execution/safety、题包或 case 顺序不同 |
| `block_service_unknown` | 缺 `identity.local_engine.binding.service` 的 PID/start |
| `block_service_not_replaced` | 任两块共用同一服务进程 |
| `request_order_differs` | 配对块的同一 plan_index 不是同一 case |
| `block_without_paired_requests` | 某配对块没有可配对请求 |

## 逐请求门槛

逐请求证据来自已与 wire 复核的 `lab_usage` 事件（请求行的 `usage_detail`）。

- 两侧均 completed 才配对；否则计入 `excluded.not_completed`。
- `prompt_tokens` 缺失阻断 `prompt_tokens_unobserved`，不等阻断
  `prompt_tokens_differ` 并写入 `prompt_token_mismatches`。
- `cached_tokens` 缺失阻断 `<side>.cold_cache_unverified`，大于 0 阻断
  `<side>.cache_hit_observed`。不补 0。例外：mlx-lm 侧缺失时按结构保证计入
  `formal_server_no_prompt_cache_reuse`（正式 MLX 服务不复用 prompt 缓存，
  源码摘要在服务核验时比对）。各侧证据种类写入 `cache_evidence`。
- 输出 token 数不等或缺失时该对计入 `excluded.output_tokens_differ`，不参与排名。
  跨格式组合例外：输出 token 数不等的对仍参与配对，计入块的
  `output_length_unmatched`；缺失时计入 `excluded.output_tokens_unobserved`。

## 指标与判定

客户端指标：`first_content_ms`（L01）、`e2e_ms`（L03）、
`output_tokens_per_second`（L04）、`client_decode_interval_ms`（L08）
= (E2E − 首内容) / (输出 token − 1)，输出 token ≤ 1 或无首内容时为 null。
`first_body_text_ms`（L09）= 首个正文文本到达 − 发送。正文文本是 content 与 reasoning 累积后，
去掉开头 `<think>` 标记仍非空白的文本，由 `compare-engines` 从已校验的事件离线算出；
没有正文时为 null。
引擎自报 timings 不参与判定。

每个配对块取两侧中位数，差值为 right − left。
任一阻断存在时 verdict 为 `not_eligible`；
否则全部块差值同号且非零时 verdict 为较优一侧的家族名，其余为 `no_clear_difference`。
跨格式组合只判 `first_body_text_ms` 与 `client_decode_interval_ms`；
`e2e_ms` 与 `output_tokens_per_second` 依赖输出长度，verdict 固定为
`display_only_output_length_unmatched`，并附 `decode_interval_not_matched_by_output_length`。
`first_content_ms` 的 verdict 固定为 `display_only_first_output_token_not_aligned`，并附
`first_content_token_not_aligned_across_formats`：mlx-lm 把 `<think>` 作为文本输出，
llama-server 解析掉该标记，两侧计时的不是同一个输出 token，因此由 `first_body_text_ms` 判定。
解码间隔仍以 `first_content_ms` 为起点，llama-server 一侧的起点晚约 2 个 token，
其间隔因此略微偏小。
`scope` 只有全部调优键两侧已观察且相同时为 `engine_implementation`，
否则为 `product_configuration` 并附 `engine_defaults_not_controlled`；
llama-cpp 对 lmstudio 当前总是后者。跨格式组合的 `scope` 固定为
`engine_and_model_format`，附 `engine_defaults_not_controlled` 与
`model_format_differs_not_attributable_to_engine`。
任一块冻结了温度停止覆盖时附 `temperature_stop_overridden`。

## 未实现

- T2b 才切换 `compare-engines` 的公开派生入口：v2 输出、HTML、封存与 `verify`
  尚未交付；当前新比较仍走 v1。
- LM Studio 后台的停止与重启由操作者执行；CLI 不停止服务，
  只通过既有主机状态与本比较的 `block_service_not_replaced` 核验替换。
- 严格定长输出与块间温度等待证据。
- 单份 12 槽位计划仅用合成服务验证执行与内部读取；真实引擎、逐槽位服务替换
  与真实样本 `verify` 仍按 T2b/T9 单独验收。

既有 run 与比较证据按原定义读取，不因新模式获得资格。
证据解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

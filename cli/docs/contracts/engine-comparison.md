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
- `engine-comparison.json` 的 `source_runs[].path` 相对输出目录；`producer` 记录
  `tool_source_sha256` 与 `template_sha256`，只作来源，不作核验依据。
- `verify --path DIR` 按 `engine-comparison.json` 识别该产物：先核对封存字节，再按
  `source_runs` 读取原 run 重算全部结果并逐字节比对（`--source-root OLD=NEW` 定位已移动的来源）。
  字节改动为 `presentation_bytes_changed`，重算不符为 `engine_comparison_recomputation_mismatch`；
  `--rerender` 另核对 HTML，不符为 `engine_comparison_html_recomputation_mismatch`。
  HTML 不含脚本与外部资源。

## 冻结调优声明

`local-engine-config.v1` 的 `engine.tuning` 可选，键为
`flash_attention`、`batch_size`、`ubatch_size`、`kv_cache_type_k`、
`kv_cache_type_v`、`gpu_layers`（取值为启动参数文本）及 `prompt_cache`（`enabled`/`disabled`）。

- llama-cpp：声明值必须与 `startup_args` 中对应选项一致，否则配置拒绝；
  绑定已核验实际进程 argv，所以观察来源为 `verified_startup_args`，
  未出现的选项记为 `engine_default`。
  `prompt_cache = disabled` 时请求体额外发送 `cache_prompt: false`。
- lmstudio：没有经核验的加载参数观察，声明任何调优键或 `prompt_cache` 均拒绝；
  比较中全部调优键为 unknown（`lmstudio_load_parameters_not_exposed`）。
- mlx-lm：同样不接受调优声明，调优键为 unknown（`mlx-lm_load_parameters_not_exposed`）。

`config create --installation` 提供 `--prompt-cache disabled`（仅 llama-cpp）写入上述声明；
`--temperature-stop-override-reason TEXT` 写入 `safety.temperature_stop_override_reason`。
两个选项用于旧 `--engine` 或 `--runtime-receipt` 候选时拒绝
（`local_engine_installation_required`）。

## 温度停止覆盖

`safety.temperature_stop_override_reason` 可选，非空且不超过 1024 字符。
存在时单服务计划把 `experiment.safety.max_temperature_celsius` 冻结为 null；
`require_temperature` 仍为 true，温度源缺失仍停止，读数照常写入证据。
运行准入要求两者一致：有声明必须为 null 阈值，无声明时 null 阈值或超过配置阈值
都返回 `local_engine_safety_policy_required`。

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

- LM Studio 后台的停止与重启由操作者执行；CLI 不停止服务，
  只通过既有主机状态与本比较的 `block_service_not_replaced` 核验替换。
- 严格定长输出与块间温度等待证据。

既有 run 与比较证据按原定义读取，不因新模式获得资格。
证据解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

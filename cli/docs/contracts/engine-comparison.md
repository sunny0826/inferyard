# 跨引擎受控比较契约

本契约对应 [ADR 056](../decisions/056-engine-comparison.md)。
实施进度见[实施计划](../plans/engine-comparison.md)。

## 命令

- `compare --mode engine --left RUN --right RUN`：只做单对身份门槛，
  始终阻断 `engine_verdict_requires_block_comparison`，不给出性能资格。
- `compare-engines --left-runs RUN... --right-runs RUN... --out DIR`：
  读取全部块，写入新目录下的 `engine-comparison.json`（`engine-comparison.v1`），
  stdout 输出同一结果。输出目录必须不存在，且不能位于任何输入 run 内。
  该文件当前未封存，也不生成 HTML。

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
- 允许不同：`config.engine.family`、`installation_sha256`、`startup_args`、
  `identity.runtime_library_hashes`；本模式不比较旧字段 `release` 与 `binary_sha256`。
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

## 指标与判定

客户端指标：`first_content_ms`（L01）、`e2e_ms`（L03）、
`output_tokens_per_second`（L04）、`client_decode_interval_ms`
= (E2E − 首内容) / (输出 token − 1)，输出 token ≤ 1 或无首内容时为 null。
引擎自报 timings 不参与判定。

每个配对块取两侧中位数，差值为 right − left。
任一阻断存在时 verdict 为 `not_eligible`；
否则全部块差值同号且非零时 verdict 为较优一侧的家族名，其余为 `no_clear_difference`。
`scope` 只有全部调优键两侧已观察且相同时为 `engine_implementation`，
否则为 `product_configuration` 并附 `engine_defaults_not_controlled`；
llama-cpp 对 lmstudio 当前总是后者。跨格式组合的 `scope` 固定为
`engine_and_model_format`，附 `engine_defaults_not_controlled` 与
`model_format_differs_not_attributable_to_engine`。
任一块冻结了温度停止覆盖时附 `temperature_stop_overridden`。

## 未实现

- LM Studio 后台的停止与重启由操作者执行；CLI 不停止服务，
  只通过既有主机状态与本比较的 `block_service_not_replaced` 核验替换。
- 严格定长输出、块间温度等待证据、比较结果封存与 HTML 呈现。
- 客户端解码间隔尚未进入指标目录，ID 暂为 `client_decode_interval`。

既有 run 与比较证据按原定义读取，不因新模式获得资格。
证据解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

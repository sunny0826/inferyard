# 跨引擎受控比较契约

本契约对应 [ADR 056](../decisions/056-engine-comparison.md)。
实施分阶段进行，进度见[实施计划](../plans/engine-comparison.md)。

## 当前状态

`experiment.comparison.mode` 与 `compare --mode` 接受 `engine`。
比较器已执行身份门槛，但尚未实现的门槛保留为阻断原因，
因此本模式当前不会给出性能资格。

## 已实现的门槛

- 两侧都必须是 `local-engine-config.v1`，否则阻断
  `engine_mode_requires_local_profile`。
- 两侧引擎家族必须不同（`engine_family_did_not_change`），
  且组合属于支持列表；当前只有 llama-cpp 与 lmstudio（`unsupported_engine_pair`）。
- 两侧 `model.kind` 都必须是 `gguf`（`engine_mode_requires_single_gguf`）。
- `config.model.content_sha256` 与 `assets_sha256` 按普通条件比较，
  不同即阻断。
- 允许不同：`config.engine.family`、`config.engine.installation_sha256`、
  `config.engine.startup_args`、`identity.runtime_library_hashes`。
- 本地 profile 不使用 `config.engine.release` 与 `binary_sha256`，
  本模式不比较这两项；其安装身份由 `installation_sha256` 表示。
- 其余条件沿用通用比较规则；其他模式下本地 profile 仍阻断
  `local_engine_controlled_comparison_not_supported`。

## 待实现的门槛

以下原因在实现并通过回归前始终出现在 `blockers` 中：

| 阻断原因 | 解除条件 |
| --- | --- |
| `engine_tuning_observation_pending` | 两侧调优参数冻结并由服务侧观察核对 |
| `per_request_prompt_token_pairing_pending` | 逐请求实际输入 token 数配对相等 |
| `cold_cache_evidence_pending` | 每个请求有缓存命中数为 0 的证据 |
| `block_alternation_pending` | 每侧至少 3 个块，A/B 交错并有替换与重绑证据 |

既有 run 与比较证据按原定义读取，不因新模式获得资格。
证据解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

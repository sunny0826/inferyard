# 跨引擎比较：调优与温度覆盖

主契约见[跨引擎比较](engine-comparison.md)。

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

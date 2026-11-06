# 跨引擎受控比较实施

目标：比较本机推理引擎的速度，回答「换引擎是否改善」。首组为同一 GGUF 下的
llama.cpp 对 LM Studio；实测后增加 llama.cpp 对 MLX-LM（同一上游模型的不同格式）。
设计理由见 [ADR 056](../decisions/056-engine-comparison.md)，当前状态见
[backlog](../backlog/engine-comparison.md#跨引擎受控比较)。

## 0. 能力实测（决定口径严格程度）

两侧各用 Qwen3-0.6B Q8_0、context 4096、少量 64-token 请求，只做诊断，不作比较结果。
脚本与原件放在 `.tools/engine-compare-probe-20261106/`。

- [x] llama-server：usage 含 `cached_tokens`；`cache_prompt=false` 时为 0。
  未测 `ignore_eos` 是否严格定长（样本均在上限截断）。
- [x] LM Studio：usage 不含缓存命中数，`cache_prompt=false` 被忽略（重复请求
  362ms → 80–85ms）。该组合因此无法证明冷缓存，见 ADR 修订。
- [x] 同一 messages 在两侧的 `prompt_tokens` 相等（13、675）。

2026-11-06 第二次探测按操作者指示只记录温度（98–110°C）。

## 1. 契约与 schema

- [x] [契约](../contracts/engine-comparison.md)：门槛、阻断原因与判定规则。
- [x] `experiment.comparison.mode` 与 `compare --mode` 增加 `engine`，schema 已重新导出。
- [x] 冻结调优声明 `engine.tuning`；LM Studio 不可观察，声明即拒绝。
- [ ] 客户端解码间隔进入指标目录（`scripts/export_catalogue.py`，不重排既有 ID）；
  目前结果中暂以 `client_decode_interval` 标识。

## 2. 运行时

- [x] 逐请求 `prompt_tokens`／`cached_tokens` 取自已复核的 `lab_usage`，
  暴露为请求行 `usage_detail`；缺失不补 0。
- [x] llama-server 调优参数从已绑定 argv 读取；`prompt_cache = disabled` 时发送
  `cache_prompt: false`。
- [ ] 严格定长：当前改为只配对输出 token 数相等的请求并报告排除数；
  是否在 engine 模式放开 `strict_fixed_length` 待第 0 步实测。
- [ ] 块间温度等待写入证据。操作者已要求忽略温度：候选可冻结温度停止覆盖
  （读数照常记录），比较附 `temperature_stop_overridden`。
- 不做：CLI 停止或重启 LM Studio。替换由操作者执行，比较器核验各块服务不同。
- [x] llama-cpp 对 mlx-lm 跨格式组合：模型资产允许不同，范围 `engine_and_model_format`；
  MLX 冷缓存按正式服务源码结构保证。

## 3. 比较器

- [x] 单对身份门槛（`analysis/engine_comparison.py`），单对始终无性能结论。
- [x] `compare-engines` 多块比较（`analysis/engine_blocks.py`）：交错、每侧至少 3 块、
  同侧配置不变、块服务替换、逐请求配对与冷缓存、每块中位数、方向一致性判定，
  输出 `engine-comparison.v1`。
- [x] 回归：合法判定、方向不一致、各阻断原因、输出长度排除、调优声明与请求体。
- [ ] `engine-comparison.json` 封存与 `verify` 支持。

## 4. 报告与 Studio

- [ ] 离线 HTML 比较页展示条件表、排除数量和结论范围；检查离线渲染。
- [ ] Studio 接入在 CLI 契约稳定后进行，同步模拟流程与一致性测试。

## 5. 真机验收

- [ ] 授权设备上按默认双题包执行 A/B/A/B/A/B，冻结温度与内存停止条件；
  背景负载记录在证据中。软件回归不替代真机证据。
  2026-11-06 llama.cpp 对 MLX-LM：第 1、2 块完成并通过 `verify`；第 3 块第 19 题时
  断开电源，按 `environment_safety_condition_changed` 停止，原件保留，第 3–6 块重新冻结运行。
  6 块均通过 `verify`，但计划冻结为 `side-by-side`，`compare-engines` 按
  `frozen_comparison_mode` 阻断，不出结论；同时暴露本地 profile 旧字段为 unknown 的
  比较器缺口（已改为替代字段核验）。驱动改为冻结 `comparison.mode = engine` 后重跑 6 块。
  重跑 6 块均通过 `verify`，两个题包 `compare-engines` 无阻断，结论见
  [backlog](../backlog/engine-comparison.md#跨引擎受控比较)。LM Studio 组合未实测，本项保持未完成。
- [ ] 合并等待用户明确授权。

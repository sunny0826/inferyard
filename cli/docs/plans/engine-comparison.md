# 跨引擎受控比较实施

目标：用同一个 GGUF 比较 llama.cpp 与 LM Studio 的速度，回答「换引擎是否改善」。
设计理由见 [ADR 056](../decisions/056-engine-comparison.md)，当前状态见
[backlog](../backlog/engine-comparison.md#跨引擎受控比较)。

## 0. 能力实测（决定口径严格程度）

两侧各用 Qwen3-0.6B Q8_0、context 4096、少量 64-token 请求，只做诊断，不作比较结果。
脚本与原件放在 `.tools/engine-compare-probe-20261106/`。

- [ ] llama-server：`ignore_eos` 能否得到固定输出 token 数；`timings.cache_n`
  与 `cache_prompt=false` 能否逐请求证明冷缓存；`/props` 能否观察调优参数。
- [ ] LM Studio：OpenAI 兼容接口是否接受 `ignore_eos` 或等价参数；
  usage 或 `/api/v0` stats 是否报告缓存命中数；`lms ps --json` 能否观察调优参数。
- [ ] 同一 messages 在两侧的 `prompt_tokens` 是否相等。

2026-11-06 首次尝试：llama-server 已启动，发首个请求前温度检查读到 96.9°C
（SMC Tp0W 等 CPU 传感器 87–89°C），按 85°C 阈值停止，未发请求。
当时代理进程 verge-mihomo 占用约 163% CPU，属于外部背景负载，未处理。
llama-server 已停止；只记录了 `/props`（build b10566-bb4caa754，n_ctx 4096）。
待背景负载消除、温度回落后重试一次，不放宽阈值。

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
- [ ] 块间温度等待写入证据。
- 不做：CLI 停止或重启 LM Studio。替换由操作者执行，比较器核验各块服务不同。

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
- [ ] 合并等待用户明确授权。

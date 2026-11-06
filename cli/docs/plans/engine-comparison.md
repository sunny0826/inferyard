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

- [x] 新增[契约](../contracts/engine-comparison.md)：已实现门槛与待实现阻断原因。
  胜负判定与合法／非法样本随第 3 步补齐。
- [x] `experiment.comparison.mode` 与 `compare --mode` 增加 `engine`，schema 已重新导出。
- [ ] 冻结调优参数声明与 LM Studio 接管授权声明。
- [ ] 新增客户端解码间隔指标，运行 `scripts/export_catalogue.py`，不重排既有 ID。

## 2. 运行时

- [ ] `local_profile.safety_policy`：仅 engine 模式放开 `strict_fixed_length`
  与冷缓存协议；按第 0 步结果逐家族声明能力，不支持则拒绝而非静默降级。
- [ ] 两个适配器逐请求保存缓存命中证据与实际输入 token 数；缺失时记原因，不补 0。
- [ ] 服务侧调优参数观察：llama-server `/props`，LM Studio 依第 0 步结果确定来源。
- [ ] LM Studio 接管：仅在冻结授权声明存在时，由 CLI 操作者流程停止并重启后台，
  核验旧进程退出后重新绑定。

## 3. 比较器

- [x] 身份门槛：双侧本地 profile、支持的家族组合、单 GGUF、模型字节相同；
  只放行引擎家族、安装、启动参数与运行库差异（`analysis/engine_comparison.py`）。
- [ ] `compare --mode engine` 接受每侧多个块的 run，核验 A/B 交错、
  每侧至少 3 块、同一计划、块间替换与重绑证据。
- [ ] 逐请求配对，输出 `engine-comparison.v1`：条件表、配对排除数、
  每块中位数、方向一致性和 `winner` / `no_clear_difference`。
- [ ] 回归：同 GGUF 合法比较、模型字节不同、输入 token 不等、调优参数未知、
  缓存未证明、块未交错、方向不一致；旧 run 读取不获得资格。

## 4. 报告与 Studio

- [ ] 离线 HTML 比较页展示条件表、排除数量和结论范围；检查离线渲染。
- [ ] Studio 接入在 CLI 契约稳定后进行，同步模拟流程与一致性测试。

## 5. 真机验收

- [ ] 授权设备上按默认双题包执行 A/B/A/B/A/B，冻结温度与内存停止条件；
  背景负载记录在证据中。软件回归不替代真机证据。
- [ ] 合并等待用户明确授权。

# ADR 057b：跨引擎比较预注册计划与旧产物

状态：Draft（草案）。日期：2026-11-07。核查基线：`6783414ccd73e8f2991adae7d3e1d707bfba9ff7`。
本草案扩展 [ADR 056](056-engine-comparison.md)，等待 D1；Experiment 模型与 D2 见 [ADR 057a](057-experiment-model.md)。
文中“事实”附源码或样本位置；“推导”是设计判断；“未验证”不当作既有能力。
`.tools/...` 均指主 checkout 中的只读样本，不是发行或测试依赖。

## 问题与决定概览

**事实**：`compare-engines` 只接收左右 run 路径，按首事件时间排序，检查交错和每侧至少三块；
没有预期块数、计划身份或计划哈希输入。因此五对一致块也可通过结构门槛，服务复用也在读取全部 run 后才判定
（`cli/src/inferyard/cli/commands/comparison.py:16-21`，`analysis/engine_blocks.py:62-85,186-203`）。

**建议**：CLI 生成并核验 `engine-comparison-plan.v1`，Studio 调用 CLI，不在 Go 中拼计划。
新比较必须绑定计划并输出 `engine-comparison.v2`；任一计划块失败使整个 Experiment 不产生性能判定，但所有原始 run 保留。
合法旧 v1 继续按原边界读取，不能事后补计划升级。

## 生成与两层冻结结构

**建议**：概念接口为 `inferyard plan engine-comparison --left-candidates ... --right-candidates ...
--blocks-per-side 3 --out NEW_DIR`；最终命令拼写属于 T2 的 CLI 参考工作。
默认冻结 `zh-core` 与 `zh-svg-pelican`，单题包仅在用户显式固定时允许。

计划采用两层而不可只冻结“A/B×3”这个意图：

1. CLI 先为每个有序块生成现有 `plan.json`，冻结该侧配置、双题包、case 顺序、trial ID 和预算。
2. `engine-comparison-plan.v1` 引用每个块计划的相对路径与 `plan_sha256`，自身也带 `schema_version = 3`、
   `plan_sha256`、配对、每侧块数、显式槽位顺序、题包顺序、缓存策略和服务替换策略。

每个槽位至少记录 `slot_id / block_index / side / run_plan / trials / output_root`。
题包项绑定 bundle 字节摘要和 case 顺序；缓存项冻结冷缓存要求及各侧允许的证据来源，不把未知缓存补成 0。
服务策略首轮为 `fresh_per_block`：同一块的双题包可共用同一已绑定服务，不同块必须使用不同 PID/start。
计划哈希排除自身字段后按现有 canonical JSON 方式计算（复用 `cli/src/inferyard/config/plan_math.py:7-13`）。

**事实**：真实块计划已有双 workload、逐 trial ID、bundle/config 摘要与 `plan_sha256`；样本 run 也有相同计划哈希和 trial ID
（`.tools/engine-compare-mlx-20261106/blocks-e1/block1-llama-cpp/plan/plan.json`，
`block2-mlx-lm/plan/plan.json`，以及其 `batch/runs/*/inputs.json`）。
因此本设计不需要修改既有 run 证据语义。若实施发现必须在 run 中新增块号或比较身份才能核验，停止该部分并重新评审。

## 执行与 run 集合核验

Studio 只按计划 `output_root` 执行，记录该 Experiment 产生的全部 run。`compare-engines --plan PLAN --out NEW_DIR`
从这些计划槽位读取实际 run 集合，不再接受任意左右集合来产生新结论，也不再以事件时间重排出一个合法顺序。
事件时间用于核对实际执行顺序；槽位用于配对。

新派生格式为 `engine-comparison.v2`，包含计划引用、每题包结果、来源 run，以及：

```text
run_set = { expected, observed, missing, unplanned, mismatched }
```

至少阻断 `comparison_plan_required / comparison_plan_hash_mismatch / planned_run_missing /
unplanned_run_present / run_plan_mismatch / execution_order_differs`。
遗漏不多算，额外 run 不进入指标，错计划或错 trial 不改挂到别的槽位；现有身份、token、冷缓存、块方向和服务门槛继续逐题包执行。
即使不能给出性能判定，也可封存含明确阻断和空指标原因的 v2 结果；不得把不完整子集写成完整实验。

## 中途失败

**建议首轮采用“整体作废”**：任一计划块失败、取消或身份不符，停止后续派发，该 Experiment 不产生性能判定。
“作废”不删除或修改 run；已完成块仍可单独核验和诊断，重试必须新建 Experiment、计划和输出目录，且无自动重试。

“作废并追加”可减少长实验重跑成本，但若在看到结果后选择替换块，会重新引入样本选择。
若用户选择它，必须在执行前冻结可追加次数、触发条件和追加槽位，原失败槽位永久保留；首轮不同时实现两种政策。

## 计划、`verify` 与旧产物

- `verify --path BLOCK_PLAN` 继续核验现有冻结计划；`compare-engines` 在派生时核验比较计划、run 集合和语义门槛。
- `verify --path COMPARISON` 先核对封存字节，再从来源计划和 run 重算 v2，逐字节比对 JSON；`--rerender` 另核对 HTML。
  移动来源继续使用显式 `--source-root OLD=NEW`，不联网、不补跑、不修复缺块。
- `verify` 不替代执行前冻结，也不把旧无计划结果升级成 v2。Studio 可依次调用核验并只读结果，不能自行重算赢家。

**事实**：当前 v1 verifier 会重算 `build(paths, ...)` 并比较 JSON；比较输出已有 `source_runs[].path/run_id/manifest_sha256`
（`cli/src/inferyard/reporting/engine_blocks_verify.py:34-58`，`engine_blocks_report.py:38-53`）。

**建议的旧读取政策**：已封存、符合现行 v1 读取规则的产物继续按 v1 核验和展示，结论保留原范围，但不获得计划资格；
v1 verifier 不追加计划门。新建比较必须走 v2，不提供 `--allow-unplanned` 或通用强制可比开关。

**事实**：`compare-e1/core` 与 `compare-e1/pelican` 各有 6 个 `source_runs` 和 3 对块，JSON 标为 `eligible=true`，
但没有计划引用；目录只有 `engine-comparison.json`，没有当前契约要求的 `report.html` 和 `artifact-manifest.json`，
其指标内容也早于 L08/L09 修订（`.tools/engine-compare-mlx-20261106/compare-e1/{core,pelican}/engine-comparison.json`，
对照 `cli/docs/contracts/engine-comparison.md:100-120`）。
对应 12 个源 run 是封存的 v3/layout2，已有计划/trial/bundle/service 等字段（同上 `source_runs` 所指目录）。

因此 `compare-e1` 原字节只作历史材料直接查看，不宣称通过当前 v1 `verify`，不迁移、不重签、不补计划，也不继承为新 v2 结论。
其源 run 仍按各自原定义读取；不能用事后生成的计划给它们追溯性能资格。

## 契约变更与兼容判断

D1 批准后由 T2 将以下内容落入跨引擎契约和导出 schema：

1. 新增 `engine-comparison-plan.v1`、两层计划引用、默认双题包和上述冻结字段。
2. 新增 `engine-comparison.v2` 及计划/run 集合阻断；保留 v1 读取路径。
3. `compare-engines` 新执行只接受计划；报告按计划槽位和题包展示缺失、额外及不匹配 run。
4. 既有 run 的字段、五终态、分母、缓存证据和身份语义不变；既有 v1 结论不升级。

这是对“新建无计划比较”的阻断性行为变更，但不是对既有 run 证据语义的破坏，故不升级核心 `schema_version = 3`。
新增派生格式版本而不是改写 v1，符合 [ADR 038](038-inferyard-current-format.md) 的当前格式与旧证据边界；
源码或实现身份变化仍按 [ADR 047](047-rerun-recorded-comparability.md) 影响比较资格，不因计划相同而豁免。

## 迁移影响与验证计划

1. **T2 CLI**：实现计划冻结、v2 派生/核验和 schema 导出；保留 v1 verifier。覆盖哈希漂移、错配对/题包/缓存、
   缺块、多块、重复块、错 trial、乱序、跨块服务复用、块内服务共享、失败块不缩小分母。
2. **T6/T7 Studio**：按计划执行并只展示 CLI 状态与产物；覆盖取消、失败块、服务替换和模拟一致性。
3. **T9 真机**：授权后执行默认双题包 A/B/A/B/A/B，分别保存 `compare-engines` 与 `verify` 结果。
4. **旧数据回归**：合法封存 v1 继续核验；`compare-e1` 按上述历史政策处理；其 12 个源 run 的核验语义不变。

本轮没有实现或运行上述验收。维护与迁移成本为路径推导，不是性能测量。

## 开放问题

| 问题 | 需要用户决定 | 本草案建议 |
| --- | --- | --- |
| D1-计划生成 | CLI 子命令生成，还是 Studio 自行生成？ | CLI 生成，Studio 只调用 CLI。 |
| D1-失败 | 块失败后整体无判定，还是预注册作废并追加？ | 首轮整体无判定，原件保留。 |
| D1-旧产物 | v1 是否继续读，`compare-e1` 是否迁移？ | 合法 v1 保留读取；`compare-e1` 不迁移、不授予新资格。 |
| D1-核验 | `verify` 是否承担计划、来源、重算与封存复核？ | 是；执行、派生和离线核验保持分层。 |
| D1-兼容 | 新比较是否必须 v2，核心版本是否保持 3？ | 必须 v2；不改变 run 语义，核心版本保持 3。 |

## 未验证项与现状记录差异

- **未验证**：两层计划的目录布局、移动后 `--source-root` 体验、v2 同时容纳双题包的结果形状，需 T2 用真实实现和样本核验。
- **未验证**：`fresh_per_block` 的双题包服务共享尚未在真实 Studio 比较执行中验收；现有脚本实际逐题包启动和停止服务
  （`.tools/engine-compare-mlx-20261106/engine_pair.py:177-264`）。
- **事实（文档与源码分歧）**：CLI 帮助仍称 same-GGUF，代码和 ADR 056 修订已支持 llama-cpp 对 mlx-lm 跨格式
  （`cli/src/inferyard/cli/commands/comparison.py:16-18`，`analysis/engine_comparison.py:9-13`）。
- **事实（文档内部状态分歧）**：决策索引称 ADR 056“实施未开始”，实施计划已标记比较器和 verify 完成，backlog 后段已记录真机结果
  （`cli/docs/decisions/README.md:53`，`plans/engine-comparison.md:42-66`，`backlog/engine-comparison.md:18-26`）。
  本草案不据此扩大已实现范围，也不在本任务越界修正。

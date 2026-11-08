# ADR 057b：单份完整比较计划与旧产物

状态：Accepted（2026-11-07 用户批准 D1，原话「D1 全批准」）。日期：2026-11-07。原始基线：`6783414ccd73e8f2991adae7d3e1d707bfba9ff7`；本轮基线：`b12eb1b0cee2c34c07ddf3d8b7e651cb310351e2`。
本草案扩展 [ADR 056](056-engine-comparison.md)；专用任务与 D2 见 [ADR 057a](057-comparison-task.md)，交接链和失败停止规则见 [ADR 057c](057-comparison-verification.md)。
文中“事实”附源码或样本位置；“推导”是设计判断；“未验证”不当作既有能力。
`.tools/...` 均指主 checkout 中的只读样本，不是发行或测试依赖。

## 问题

**事实**：`compare-engines` 只接收左右 run 路径，按首事件时间排序，检查交错和每侧至少三块；
没有预期块数或完整计划输入。因此五对一致块也可通过结构门槛，服务复用也在读取全部 run 后才判定
（`cli/src/inferyard/cli/commands/comparison.py:16-21`，`analysis/engine_blocks.py:62-85,186-203`）。

## A、B、C 比较与推荐

### 方案 A：单份完整可执行计划（推荐）

一份现有 `plan.json` 按执行顺序容纳全部 12 个 workload/trial：六块 × 双题包，块间 A/B 交错，块内逐题包执行。
`workload_id` 使用冻结的 `block-01-a-core` 等确定标识表达槽位；不新增 plan 或 run 字段。
`experiment.comparison.mode = engine`，各 workload 引用本槽位的 config、bundle 和 case 顺序。
契约名称 `engine-comparison-plan.v1` 指这种受约束的现有 plan 形状，不增加外层文件或新的 `profile` 字段。

**事实**：`compile_plan` 已按 workload 顺序展开 trial，并以 `experiment_id/workload_id/repeat_index` 生成 trial ID
（`cli/src/inferyard/config/planning.py:39-84`）。batch 按当前 workload 取对应配置，在不同 workload 间要求显式 handoff；
执行支持检查也逐 workload 取 adapter（`runtime/batch_runner.py:163-218`，`registry.py:211-221`）。
因此同一计划可容纳不同引擎家族和不同服务进程。

**事实**：Journal 将完整 `plan` 对象和 `run.plan_sha256` 写入 `inputs.json`；读取时核对该哈希、experiment 和 trial
（`cli/src/inferyard/evidence/journal.py:122-173`，`evidence/ledger.py:74-122`）。
真实样本的 core/pelican 两个 run 共享同一双 trial plan 哈希，而各自 trial 不同
（`.tools/engine-compare-mlx-20261106/blocks-e1/block1-llama-cpp/plan/plan.json` 及其 `batch/runs/*/inputs.json`）。

所以 A 复用既有 run 绑定语义：每个 run 都绑定同一完整范围，缩减计划不能合法化已有 run。
连续执行和跨 batch 防拼接另由 ADR 057c 的封存交接链保证；无需改变 run 字段、五终态或已封存读取语义。

### 方案 B：两层块计划（不采用）

外层只列内层哈希时，内层 run 并未绑定外层，仍可事后补外层。
要形成执行前共同绑定，内层 plan 必须再包含外层协议哈希，或重定义现有 `experiment_id` 的用途；
前者增加新的绑定字段及其读取/迁移规则，后者破坏既有身份含义。两层还增加目录、哈希和恢复路径。
本草案停止这条需要新增绑定语义的设计，不以它修改 run 证据。A 已满足同一目标且无需这些成本，故不选 B。

### 方案 C：只约束 Studio（退路，不启用）

C 只能在 Studio 受控流程中预先固定范围，离线核验不能排除事后挑块。
由于 A 已能用真实现有结构提供完整计划绑定，不需要退回 C；若后续实现证明 A 也必须改变既有 run 语义，则停止 A/B 并回报用户决定是否接受 C。

## 计划生成、手动执行与派生

CLI 提供生成子命令，复用 `compile_plan` / `write_plan`；Studio 收集两侧候选后调用 CLI，不在 Go 中拼 JSON。
默认冻结 `zh-core` 与 `zh-svg-pelican`，单题包仅在用户显式固定时允许。
计划包含全部 config、bundle、缓存声明、case 顺序、预算和确定 trial ID；`plan_sha256` 继续按现有 canonical JSON 计算
（`cli/src/inferyard/config/plan_math.py:7-13`）。

CLI 预先生成计划 → 操作者按槽位执行 → CLI 派生仍是合法手动路径；新规则只阻断未预注册的事后比较。
执行使用一个 batch，`run --plan` 逐槽位调用并显式 handoff。`compare-engines` 改为接收完整 plan 与 batch，
按 trial 集合和交接链核验，不再接受任意左右 run 集合来产生新结论，也不以事件时间重排出合法顺序。

新派生格式为 `engine-comparison.v2`：

```text
plan = { path, sha256 }
run_set = { expected_trials, observed_runs, missing, unplanned, mismatched }
```

至少阻断 `comparison_plan_required / comparison_plan_hash_mismatch / planned_run_missing /
unplanned_run_present / run_plan_mismatch / execution_order_differs`，并加入 ADR 057c 的交接链和历史停止原因。
遗漏不多算，额外 run 不进入指标，错 trial 不改挂槽位；现有身份、token、冷缓存和块方向门槛继续逐题包执行。
v2 先核验整个 batch，再按冻结槽位拆成各题包 A/B 块；不能把 12 个 run 直接交给现有六块比较逻辑。
不能判定时也可封存含阻断和空指标原因的 v2，不把子集写成完整实验。

## 逐题包替换服务

首轮每个 workload/trial 都更换服务进程并重新绑定；删除块内双题包共用服务的许可。
计划生成器把 `require_fresh_process = true` 写入每个 workload 所引用 **config 的 `execution`**，
不是 `experiment.execution`；现有 schema 已接受该字段（`cli/src/inferyard/contracts/schemas_config.py:120-122`）。
执行器停止旧服务、确认退出、启动并绑定新服务；v2 另要求全部槽位的 PID/start 互不相同。

**事实**：现有 `reuse_allowed` 只有 model、engine、generation 等相同才允许同进程复用
（`cli/src/inferyard/runtime/service_reuse.py:10-21`）。真实样本 core 为 `max_tokens=512`、pelican 为 `4096`
（`.tools/engine-compare-mlx-20261106/blocks-e1/block1-llama-cpp/plan/{core,pelican}-config.toml`），
所以块内复用不能沿用一般复用门。逐槽位换服务也与现有脚本一致（`engine_pair.py:177-264`）。

## 中途失败的代价与恢复

首轮采用“整体作废”：任一槽位失败、取消或身份不符，停止后续派发，该比较不产生性能判定。
末块失败可能使此前多小时测量无法形成判定，这是选择简单、避免事后换块所接受的实际成本。
“作废”不删除或修改 run；重跑测量必须新建计划、batch 和输出目录，且无自动重试。

是否停止由 ADR 057c 的 CLI 比较专用历史规则统一判定，执行入口与 v2 派生共用；Studio 只读取结果。
正常 handoff 和模型答错不算执行失败；run 创建前的启动/绑定失败由 Studio 或手动操作者终止，CLI 离线证据不能保证这一段。
若只是派生、HTML 或展示失败，直接复用原 run 和完整计划重新派生到新目录，不重跑测量。
“作废并追加”仍不采用；若未来允许，须先冻结追加次数、触发条件和槽位，不能看到结果后选择替换块。

## `verify` 与旧产物

- `verify --path PLAN` 继续核验冻结计划；`compare-engines` 在派生时核验完整 run 集合和语义门槛。
- `verify --path COMPARISON` 核对封存字节，从来源 plan/run 重算 v2 并逐字节比对；`--rerender` 另核对 HTML。
  移动来源继续显式 `--source-root OLD=NEW`，不联网、不补跑、不修复缺块。
- 合法 v1 继续按原边界核验和展示，不追加计划门，也不获得 v2 资格；新比较必须走 v2，不提供 `--allow-unplanned`。

**事实**：`compare-e5/core` 与 `compare-e5/pelican` 都有 `engine-comparison.json`、`report.html` 和
`presentation-seal.v1` 的 `artifact-manifest.json`。返工第 1 轮实际只读运行 `inferyard verify --path`，两者均返回
`verified / complete`、`source_runs=6`、`semantic_verified=true`、`render_checked=false`
（样本目录 `.tools/engine-compare-mlx-20261106/compare-e5/{core,pelican}/`，本 ADR 记录的是未做 `--rerender` 的检查）。

**事实**：`compare-e1/{core,pelican}` 只有 `engine-comparison.json`，没有上述 HTML 和封存清单，也没有完整计划引用；
其指标内容早于 L08/L09 修订（`.tools/engine-compare-mlx-20261106/compare-e1/{core,pelican}/engine-comparison.json`，
对照 `cli/docs/contracts/engine-comparison.md:100-120`）。
因此 `compare-e1` 只作历史材料直接查看，不宣称通过当前 v1 `verify`，不迁移、不重签、不补计划；其源 run 仍按原定义读取。

## 契约变更与兼容判断

D1 批准后由 T2 落入契约和导出 schema：

1. 定义 `engine-comparison-plan.v1` 为上述现有 plan 的受限形状及确定槽位 ID，不增加外层计划或 run 字段。
2. 新增 `engine-comparison.v2` 和完整 run 集合、交接链及历史停止阻断；保留 v1 读取路径。
3. `compare-engines` 新执行只接受完整 plan/batch；报告按槽位和题包展示缺失、额外、错配及断链 run。
4. 既有 run 字段、五终态、分母、缓存证据、身份和已封存读取语义不变；既有 v1 结论不升级。

这是对“新建无计划比较”的阻断性行为变更，不破坏既有 run 证据语义，核心 `schema_version = 3` 不变。
新增派生格式而非改写 v1，符合 [ADR 038](038-inferyard-current-format.md)；
实现身份差异仍按 [ADR 047](047-rerun-recorded-comparability.md) 处理。

## 验证计划与未验证项

T2 覆盖：一份计划生成全部槽位、所有 run 共享计划哈希、不同家族 handoff、逐槽位 fresh process、
缺/多/重复/错 trial、乱序、手动与 Studio 路径、失败后重新派生不重跑，以及 ADR 057c 的断链与历史停止反例。
T6/T7 覆盖取消清理、绑定/替换失败、原子恢复和模拟一致性；T9 在授权设备执行默认双题包 A/B/A/B/A/B，
分别保存 `compare-engines` 与 `verify` 结果。

- **未验证**：`.tools/engine-compare-mlx-20261106/blocks-e1/` 实际是 6 份双 trial 计划，不是 A 的单份 12 槽位计划；
  单份 12 槽位计划尚未生成与核验，现有样本只支持既有机制判断。
- **未验证**：`require_fresh_process=true` 的 Studio 全链路和逐槽位服务替换未做真实引擎验收。
- **事实（文档与源码分歧）**：CLI 帮助仍称 same-GGUF，代码已支持 llama-cpp 对 mlx-lm 跨格式
  （`cli/src/inferyard/cli/commands/comparison.py:16-18`，`analysis/engine_comparison.py:9-13`）。
- **事实（文档状态分歧）**：决策索引称 ADR 056“实施未开始”，实施计划和 backlog 已记录实现及真机结果
  （`cli/docs/decisions/README.md:53`，`plans/engine-comparison.md:42-66`，`backlog/engine-comparison.md:18-26`）。

## 剩余问题

| 问题 | 建议 |
| --- | --- |
| D1 是否批准方案 A？ | 批准；无需改变 run 语义，不退回 C。 |
| 失败块整体无判定的成本是否接受？ | 接受；展示/派生失败只重新派生，不重跑测量。 |
| v1、`compare-e5`、`compare-e1` 政策是否批准？ | e5 等合法 v1 保留读取；e1 仅作历史材料，均不获得 v2 资格。 |

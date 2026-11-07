# ADR 057a：Experiment 模型与职责边界

状态：Draft（草案）。日期：2026-11-07。核查基线：`6783414ccd73e8f2991adae7d3e1d707bfba9ff7`。
本草案等待 D1、D2；比较计划字段、旧产物与契约变更见 [ADR 057b](057-engine-comparison-plan.md)。
文中“事实”附源码位置；“推导”是设计判断；“未验证”不当作既有能力。

## 问题与决定概览

**事实**：普通测评与长上下文专项已复用 Studio 串行队列，但任务归属、准备和结果读取不同；
跨引擎块顺序目前由操作者脚本拥有，CLI 只做事后比较（`studio/internal/core/runs.go:268-334`，
`context_speed_prepare.go:146-227`，`.tools/engine-compare-mlx-20261106/engine_pair.py:66-113,177-272`）。

**建议（D1）**：采用“有边界的 Experiment 领域模型 + CLI 预注册比较计划”，不建立通用插件框架。
首轮只接入跨引擎比较；普通测评与长上下文专项保持现入口。
CLI 管协议、门槛、产物与核验；Studio 管服务生命周期与编排，不在 Go 中重算判定。
**建议（D2）**：首轮不迁移长上下文专项。

目标数据流：

```text
CLI 冻结协议和有序 run 计划
  → Studio Experiment 执行器逐槽位准备服务并调用 CLI run
  → CLI compare-engines 核验完整 run 集合并生成派生产物
  → CLI verify 离线复核计划、来源、重算结果与封存字节
  → Studio 只读状态和产物
```

## 现有调用者与保留现状的代价

| 流程 | 事实：真实入口、状态与产物 | 保留现状的代价（推导） |
| --- | --- | --- |
| 普通测评 | 页面 `runBenchmarkFlow` 启动、绑定后调用 `StartRuns`；`Service.runQueue` 串行执行 `run --config`，内存 `runEntry` 持有任务，完成后调用 `report`（`studio/internal/pages/candidates_prepare.go:113-225,228-251`，`core/runs.go:92-142,268-334`）。本地安装另由 `StartCandidateRuns` 逐题包替换服务（`core/local_runs.go:23-95,142-273`）。 | 编排阶段和报告归属主要在页面与 `runEntry`，不能直接表达“一个实验包含有序的多块 run”。 |
| 长上下文专项 | `startSpeedFlow` 到 `StartContextSpeed`；CLI 冻结五 trial 计划后仍复用原队列执行 `run --plan`，`speed-results` 认证结果；原子任务记录只存身份、阶段和引用（`pages/context_speed_flow.go:15-20`，`core/context_speed_prepare.go:146-227`，`context_speed_run.go:13-141,164-227`，`context_speed_store.go:49-89`）。 | 已有独立计划、十秒取消宽限和结果协议；首轮强行通用化会同时改动这些边界（取消见 `context_speed_run.go:13-39`）。 |
| 跨引擎比较 | 操作者脚本为每块冻结双题包计划，按 A/B 交替启动、绑定、执行并停止服务；顺序和块号由脚本参数拥有（`.tools/engine-compare-mlx-20261106/engine_pair.py:66-113,177-278`）。产品内没有对应 Experiment。 | 顺序可事后重排，块可事后挑选；Studio 若另写一套判定会复制 CLI 规则。 |

只加 CLI 比较计划、不定义 Experiment，是改动最小的备选，但 Studio 会出现第三种专用任务归属与完成路径。
全量通用化则一次迁移三条流程，回归面最大。选定的最小模型只统一计划引用、槽位、执行状态、run/产物引用和取消边界；
不统一指标、页面、CLI profile 或专项结果解析。

## Experiment 最小形态

Studio `Service` 是执行状态唯一写入者，沿用现有串行队列与引擎所有权；页面只提交命令和读取投影。
长任务以原子记录保存身份和引用，不缓存成绩。建议的逻辑类型如下，具体 Go 名称由实现决定：

```text
Experiment       { id, kind, plan: PlanRef, slots: [RunSlot], runs: [RunRef],
                   artifacts: [ArtifactRef], stage, error }
PlanRef          { path, sha256, definition }
RunSlot          { slot_id, block_index, side, workload_order, run_plan: PlanRef }
RunRef           { slot_id, run_id, trial_id, path, plan_sha256, manifest_sha256 }
ArtifactRef      { kind, path, sha256 }
stage            = preparing | executing | deriving | verifying | done | failed | cancelled
```

公共调用保持窄：`StartExperiment(planRef)`、`CancelExperiment(id)`、`ReadExperiment(id)`。
执行器隐藏逐槽位停止旧服务、启动、CLI 绑定、执行和清理；它向调用者暴露计划顺序、当前槽位、失败与产物引用，
不暴露 Go 内部服务状态，也不允许页面重排槽位。`done` 只表示编排结束，不等于比较 `eligible`。

首轮只有 `kind = engine-comparison`。普通测评继续由 `StartRuns` 完成，专项继续由 `ContextSpeedTask` 完成；
待第二个流程真实迁移后再收回重复接口，不预设 profile 注册表或通用结果解析器。
这符合根 `AGENTS.md:100-109` 的服务与证据边界，以及 `docs/subsystems/studio-panel/responsibilities.md:22-27`
关于队列、报告、证据索引和 CLI 所有权的现有分工。

## 取舍与迁移影响

- **只加比较计划**：首轮代码与迁移成本最低，但不解决 Studio 的实验状态与报告归属；作为拒绝的备选保留。
- **首轮全量抽象**：可减少后续重复接口，但同时触碰普通报告、专项五 trial、取消和结果读取，验证成本最高；不采用。
- **选定的有界模型**：先付出一组 Experiment 引用类型和执行器成本，换取比较链路的单一状态拥有者；
  首轮仍保留另外两条既有路径，不能把共享模型称为已统一所有实验。

T6 只迁移比较到 Experiment，复用队列、取消、服务所有权和原子引用记录；普通与专项入口不变。
覆盖取消清理、绑定/替换失败、无自动重试、重启后引用恢复和报告归属；同轮更新模拟与独立预期的一致性场景。
T7/T9 分别验收页面、原生窗口与真实 CLI/模型，任何一层都不能替代另一层。
D2 若日后改为迁移专项，必须另验五 trial、十秒排空、历史读取和 `speed-results` 认证，不继承比较链路的验收。

本轮是文档设计，没有实现或运行上述验收，也没有测量资源占用或速度；维护与迁移成本均为基于调用路径的推导。

## 开放问题

| 问题 | 需要用户决定 | 本草案建议 |
| --- | --- | --- |
| D1 | 是否批准最小 Experiment 和 Studio/CLI 职责边界？ | 批准；不做通用插件框架。 |
| D1-抽象范围 | 只加比较计划、首轮全量抽象，还是有界模型？ | 有界模型，首轮只接比较。 |
| D1-职责 | 服务生命周期与协议/判定分别归谁？ | Studio 管服务与编排；CLI 管协议、门槛、产物与核验。 |
| D2 | 首轮是否同时迁移长上下文专项？ | 不迁移；比较链路验收后另评。 |

## 未验证项

- **未验证**：Experiment 原子记录的重启恢复、与现有队列的取消衔接，以及派生/核验阶段的页面投影，需 T6/T7 证明。
- **未验证**：最小接口是否足以承载长上下文专项只是结构推导；D2 未实施前不声称它已覆盖专项全部行为。

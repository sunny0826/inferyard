# ADR 057a：跨引擎比较专用任务与职责边界

状态：Accepted（2026-11-07 用户批准 D1，原话「D1 全批准」）。日期：2026-11-07。原始基线：`6783414ccd73e8f2991adae7d3e1d707bfba9ff7`；本轮基线：`b12eb1b0cee2c34c07ddf3d8b7e651cb310351e2`。
本草案采用用户指定的“专用比较任务记录”，不定义通用 Experiment；方案 A 的单份完整计划见 [ADR 057b](057-engine-comparison-plan.md)。
文中“事实”附源码位置；“推导”是设计判断；“未验证”不当作既有能力。

> 局部取代：比较产物的 HTML 文件与封存呈现字节核验由 [ADR 059](059-studio-json-only-report.md) 取代（verify 改为从 plan/batch 重算单文件 `engine-comparison.json`）；任务职责边界与编排规则不变。

## 决定

首轮只增加 `EngineComparisonTask` 及 CLI 预注册比较计划；普通测评与长上下文专项保持现入口。
CLI 管协议、门槛、产物与核验；Studio 管服务生命周期和任务编排，不在 Go 中重算判定。
D2 保持：首轮不迁移长上下文专项。

目标数据流：

```text
CLI 预先生成一份覆盖全部槽位的可执行 plan
  → Studio 专用任务逐槽位换服务、重新绑定并调用 CLI run
  → CLI compare-engines 按完整 plan 核验 run 集合并派生
  → CLI verify 离线复核来源、重算结果与封存字节
  → Studio 专用任务只读状态和产物
```

手动路径同样合法：CLI 预先生成计划 → 操作者按槽位执行 → CLI 派生。
新规则阻断的是未预注册的事后比较，不要求必须经过 Studio。

## 真实调用者与现状代价

| 流程 | 事实：入口、状态与产物 | 保留现状的代价（推导） |
| --- | --- | --- |
| 普通测评 | 页面 `runBenchmarkFlow` 启动、绑定后调用 `StartRuns`；`Service.runQueue` 串行执行 `run --config`，内存 `runEntry` 持有任务，完成后调用 `report`（`studio/internal/pages/candidates_prepare.go:113-225,228-251`，`core/runs.go:92-142,268-334`）。 | 不必为多块比较改变普通任务。 |
| 长上下文专项 | `StartContextSpeed` 冻结五 trial 计划并复用队列，`speed-results` 认证结果；原子记录只存身份、阶段和引用（`studio/internal/core/context_speed_prepare.go:146-227`，`context_speed_run.go:164-227`，`context_speed_store.go:49-89`）。 | 已有专项计划、十秒取消宽限和结果协议；首轮通用化会扩大回归面。 |
| 跨引擎比较 | 操作者脚本按块和题包启动、绑定、执行并停止服务；顺序由脚本拥有（`.tools/engine-compare-mlx-20261106/engine_pair.py:66-113,177-278`）。 | 只靠离线计划不能拥有 Studio 的当前槽位、取消和派生产物归属；需要专用记录，但不足以证明通用抽象必要。 |

## 专用比较任务记录

Studio `Service` 是任务状态唯一写入者，沿用串行队列与引擎所有权；页面只提交命令和读取投影。
原子记录保存身份和引用，不缓存成绩。建议的逻辑形状如下，具体 Go 名称由实现决定：

```text
EngineComparisonTask { id, plan: PlanRef, batch: BatchRef, current_trial_id,
                       runs: [RunRef], artifacts: [ArtifactRef], stage, error }
PlanRef            { path, sha256 }
BatchRef           { path, plan_sha256 }
RunRef             { trial_id, run_id, path, manifest_sha256 }
ArtifactRef        { kind, path, sha256 }
stage              = preparing | executing | deriving | verifying | done | failed | cancelled
```

公共入口采用专用 `StartEngineComparison` / `ReadEngineComparison`，取消复用现有 `CancelRun`。
执行器隐藏逐槽位停止旧服务、启动、CLI 绑定、执行和清理；页面可观察计划顺序、当前 trial、失败和产物，
不能重排槽位。`done` 只表示编排结束，不等于比较 `eligible`。

历史取消、执行失败或身份错误是否停止后续槽位，由 CLI 比较专用规则统一判定，Studio 只读取结果；
正常 handoff 和模型答错不由 Go 另行解释为失败。run 创建前的启动／绑定失败由 Studio 立即终止本任务；
手动路径由操作者终止，CLI 离线证据不能保证这一段。规则见 [ADR 057c](057-comparison-verification.md)。

这份专用记录直接解决本次 Studio 状态与报告归属；不再保留“只有通用 Experiment 才能解决归属”的假设。
CLI 计划负责完整测量范围，专用记录负责执行状态，两者不互相替代。

## 被拒绝的抽象与重新评估条件

- **有界通用 Experiment**：被拒绝。首轮只有一种新流程，另建通用类型、公共 `StartExperiment` 和结果接口会扩大未验证表面。
- **首轮迁移全部流程**：被拒绝。普通报告、专项五 trial、取消与读取协议不同，迁移和验收成本最高。
- **只加 CLI 计划、不加 Studio 专用记录**：仅适用于操作者手动流程；Studio 会继续把当前槽位和产物归属留在页面或脚本中，不作为 Studio 方案。

当第二条流程真实需要迁移、且两条实现都证明存在相同的计划引用、槽位执行、取消和产物归属时，再评估提取通用 Experiment。
不得为了预先满足该抽象改动长上下文专项；届时另验五 trial、十秒排空、历史读取与 `speed-results` 认证。

## 迁移与验证边界

T6 只接入专用比较任务，复用队列、取消、服务所有权和原子引用记录。
覆盖取消清理、绑定/替换失败、无自动重试、重启后引用恢复、手动与 Studio 两条执行路径及报告归属；
同轮更新模拟与独立预期的一致性场景。T7/T9 分别验收页面、原生窗口与真实 CLI/模型，不能相互替代。

本轮是文档设计，未实现上述验收，也未测量资源或速度；维护与迁移成本是路径推导。

## 决定与剩余问题

| 项目 | 结论 |
| --- | --- |
| 首轮模型 | 用户已决定专用比较任务记录；通用 Experiment 为被拒绝备选。 |
| 职责 | Studio 管服务与编排；CLI 管协议、门槛、产物与核验，符合根 `AGENTS.md:100-109`。 |
| D2 | 首轮不迁移长上下文专项；第二条真实流程迁移时再评估通用抽象。 |
| D1 收尾 | 仍需用户批准方案 A、失败政策和旧产物政策，见 ADR 057b。 |

## 未验证项

- 专用任务的原子恢复、队列取消衔接和派生/核验页面投影需 T6/T7 证明。
- 手动执行与 Studio 执行是否产生同一组合法的 CLI 状态转换，需用同一独立预期分别验收，不能从某一实现生成预期。

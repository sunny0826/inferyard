# Studio 实验模型与设计层重构

目标：先定领域模型与证据方法，再建 Studio 设计层，最后接入跨引擎比较与新视觉。
背景：现有 Studio 只有颜色一层可换主题；一次任务只对应一次 benchmark；跨引擎比较的块编排在
`.tools/` 脚本里，`compare-engines` 只能事后核验块结构，没有预注册计划。
依据：[ADR 056](../decisions/056-engine-comparison.md)、[跨引擎比较契约](../contracts/engine-comparison.md)、
[跨引擎比较实施](engine-comparison.md)第 4、5 节。

## 用户决策点

| 编号 | 决策 | 阻塞任务 |
| --- | --- | --- |
| D1 | 批准 ADR 057（实验模型 + 比较预注册计划）及其开放问题的取舍 | T2、T6、T7 |
| D2 | 实验模型首轮是否迁移长上下文专项，或只承载跨引擎比较 | T6 范围 |
| D3 | 状态词表的唯一来源位置（CLI 包内数据 + 导出脚本，或其他） | T3 |
| D4 | 视觉方案（在调试窗口内用真实页面比选） | T5 收尾 |

## 公共规则（所有 agent）

必读：仓库根 `AGENTS.md`、[README](../../../README.md)、[测试策略](../../../docs/testing.md)。
命令一律 `rtk proxy mise exec -- …`；不推送、不合并、不开 PR；在协调者指定的分支内工作，
只改本任务「写入范围」内的文件，越界先向协调者报告文件与理由。
不下载模型、不启动模型服务、不发送推理请求（T9 除外）。
完成时报告：改动文件、实际运行的检查、未验证范围、发现的文档与源码分歧。

## 任务总览

| 任务 | 执行角色 | 把关（独立会话） | 依赖 | 可并行 |
| --- | --- | --- | --- | --- |
| T0 清理与分工 | 协调者 | — | — | — |
| T1 ADR 057 草案 | 架构师 | 挑刺者（T1R），之后用户 D1 | T0 | T4 |
| T2 CLI 比较计划实现 | worker | Reviewer | T1 + D1 | T4、T5 |
| T3 共享状态词表 | worker | Reviewer | D3；模板改动在 T2 之后 | T4 |
| T4 Studio 设计 token 与组件层 | worker | Reviewer | T0 | T1、T2 |
| T5 视觉方案比选与落地 | worker | 用户 D4；Reviewer | T4 | T2 |
| T6 Studio 实验模型与比较编排 | worker | Reviewer + 审核员 | T2、T4 + D1、D2 | — |
| T7 跨引擎比较页面、模拟与一致性 | worker | Reviewer；另一 worker 会话跑原生验收 | T5、T6 | — |
| T8 文档同步与整分支审查 | writer，之后 Reviewer | 见 T8 | T2–T7 各自完成后 | — |
| T9 真机验收 | worker（需操作者授权） | 协调者核对证据 | T7、T8 | — |

角色职责、派发规则与适用技能见[角色与派发](studio-experiment-roles.md)。

## T0 清理与分工（协调者）

- 删除临时文件 `studio/internal/designlab/`、`studio/internal/pages/zz_shots_tmp_test.go`。
- 从 `main` 建分支，把本计划提交到该分支，作为各 worktree 的基线；本计划未提交时 worktree 读不到它。
- 按下文写入范围分配所有权，记录在协调者的任务状态记录中（不提交）。
- 读：本计划、`AGENTS.md`、`multi-agent-file-ownership-boundary` 技能。

## T1 ADR 057 草案（架构师，只写文档）

目标：定义 Experiment 模型（冻结协议 → 有序 run 计划 → 执行器 → 派生产物 → 核验），
以及 `engine-comparison-plan` 预注册计划：配对、每侧块数、顺序、题包、缓存策略、
计划哈希；`compare-engines` 按计划核验 run 集合，包括遗漏和多出的 run。

写入范围：`cli/docs/decisions/057-*.md`、`cli/docs/decisions/README.md`。
契约改动写成 ADR 内的「契约变更」一节；D1 批准后由 T2 落入 `contracts/engine-comparison.md`，
未批准的规则不进入现行契约。

设计依据真实样本：核对主 checkout 下 `.tools/engine-compare-mlx-20261106/compare-e5`（当前格式）与 `compare-e1`（早期未封存）
的产物与其 `source_runs`（只读），不基于假想字段出方案。

必读：
- [ADR 056](../decisions/056-engine-comparison.md) 全文含修订与兼容性节；[ADR 038](../decisions/038-inferyard-current-format.md)、[ADR 047](../decisions/047-rerun-recorded-comparability.md)。
- [跨引擎比较契约](../contracts/engine-comparison.md)、[实施计划](engine-comparison.md)、[backlog](../backlog/engine-comparison.md)。
- [数据契约 · 证据血缘与比较结论](../data-contract.md#证据血缘与比较结论)、[实验设计](../experiments/design.md)、[指标](../experiments/metrics.md)。
- [Studio 职责与事实源](../../../docs/subsystems/studio-panel/responsibilities.md)、[运行路径](../../../docs/subsystems/studio-panel/runtime.md)、[本地引擎](../../../docs/subsystems/studio-panel/local-engines.md)、[长上下文专项](../../../docs/subsystems/studio-panel/context-speed.md)。

只读上下文：`cli/src/inferyard/analysis/engine_blocks.py`、`analysis/engine_comparison.py`、
`reporting/engine_blocks_verify.py`、`cli/commands/comparison.py`；
`studio/internal/core/runs.go`、`local_runs.go`、`context_speed_*.go`；
`.tools/engine-compare-mlx-20261106/engine_pair.py`、`bind_loop.py`（只作现状参考，不迁入）。

须回答并列为开放问题：计划由谁生成（CLI 子命令或 Studio 调 CLI）；中途失败的块如何记录
（作废并追加，还是整体作废）；旧的无计划比较产物的读取政策；计划与 `verify` 的关系；
Studio 与 CLI 的职责切分（服务生命周期归 Studio，协议、门槛、产物与核验归 CLI）。

检查：本地链接与锚点；不改代码。停止条件：发现需要改动既有 run 证据语义时，先报告协调者。

## T1R ADR 反证审查（挑刺者，只读）

目标：在用户 D1 之前反证 ADR 057 草案。重点回答：
Experiment 抽象是否超出当前两三种实验的需要，能否只加比较计划而不抽象；
预注册是否有更简单的替代；是否与 ADR 056 或既有块门槛重复；D2 的最小范围。

输入：指定 commit 的 ADR 057 草案及 T1 必读。写入范围：无，只交报告。
命令边界：只读命令；不运行模型、不写仓库。

## T2 CLI 比较计划实现（worker）

目标：按批准的 ADR 057 实现计划冻结、`compare-engines` 按计划核验及新阻断原因；
旧产物按 ADR 的读取政策处理。

写入范围：`cli/src/inferyard/analysis/engine_blocks.py`、新增计划模块（`contracts/` 或
`config/` 下，按 ADR）、`cli/commands/comparison.py`、`application/types.py` 中相关类型、
`reporting/engine_blocks_report.py`、`reporting/engine_blocks_verify.py`、
`templates/engine_comparison.html`、对应 `cli/tests/unit/test_engine_blocks*.py`、
导出的 schema（只经 `scripts/export_schemas.py` 生成）、契约与 CLI 参考文档。

必读：T1 产物；[CLI 参考](../cli-surface.md)、[架构](../architecture.md)、
[贡献指南 · 变更与验证](../../CONTRIBUTING.md#changes-and-verification)、
[评分与报告子系统](../../../docs/subsystems/scoring-reporting.md)、
[scripts/README](../../scripts/README.md)（运行 export 脚本前）。

检查：`ruff`、相关 pytest、schema 导出漂移核对、对 `.tools/engine-compare-mlx-20261106/compare-e5`
（当前格式的封存产物；`compare-e1` 是早期未封存产物）执行 `verify` 确认旧产物读取符合政策（只读，不改原件）、HTML 离线渲染检查。

## T3 共享状态词表（worker；Studio 小改需协调者授权）

目标：比较判定、限制、阻断原因等枚举的中文措辞只保留一处，HTML 模板与 Studio 共用。

写入范围：词表数据文件与导出/核对脚本（位置按 D3）、`templates/engine_comparison.html`
（T2 完成后）、`studio/internal/pages/dashboard_feedback.go` 中 `human()` 的数据来源。

必读：[catalogue 生成](../../../docs/subsystems/catalogue-generation.md)及其 rules/paths 分页、
`cli/scripts/export_catalogue.py`、`studio/internal/pages/dashboard_feedback.go`、
[Studio Design System · 状态、空态与错误](../../../studio/docs/design-system.md#状态空态与错误)。

检查：导出脚本 `--check`；未知枚举在 Studio 保留原码并说明未识别；Go 与 pytest 相关回归。

## T4 Studio 设计 token 与组件层（worker）

目标：建立 token（密度、圆角、卡片样式、字号层级、语义状态色、A/B 两侧色）与组件
（卡片、状态徽章、阶段进度、指标、成对条形）层；页面只经该层取样式。本任务不改变外观。

写入范围：`studio/internal/pages/design_system.go`、`components.go`、`window.go` 的 `card()`、
新增 token 文件；各页面文件仅做调用点替换（与 T6/T7 冲突时以 T4 先合入为准）。

必读：[Studio README](../../../studio/README.md)、[Design System](../../../studio/docs/design-system.md)
及 [components](../../../studio/docs/design-system-components.md)、[measurement](../../../studio/docs/design-system-measurement.md)、
[审查记录](../../../studio/docs/design-system-audit.md)、[UI 调试预览](../../../docs/subsystems/studio-panel/preview.md)、
[Studio 开发](../../../docs/subsystems/studio-development.md)；MyGo v0.7.4 Styling/Layout 文档；
onevra `theme/theme.go`、`components/badge.go`、`shells/sidebar/shell.go`（模块缓存内，只读）。

检查：`go test ./...`、`go test -tags ui_debug . ./internal/pages -count=1`；
迁移前后用内存渲染导出同一组场景图做对照（临时文件，不提交）；文件不超过约 500 行。

## T5 视觉方案比选与落地（worker）

目标：在 `ui_debug && !release` 构建的调试窗口加入方案切换，使用真实页面渲染候选 token 集；
用户选定（D4）后设为默认，同步 Design System 文档、原型 CSS 与调色板表。
修复已知问题：深色下标题旁徽章不可读；A/B 两侧色在深色下需可区分。

写入范围：T4 的 token 文件、`preview*.go`、`studio/docs/design-system*.md`、`studio/docs/prototypes/`。

必读：T4 产物与其必读；[调试预览规则](../../../docs/subsystems/studio-panel/preview.md)。

检查：同 T4；普通构建与 `ui_debug,release` 均拒绝调试入口；浅色、深色、高对比度、
文字 1.5/2.0 的内存渲染目检；原生窗口目检交由 T9 或单独记录为未验证。

## T6 Studio 实验模型与比较编排（worker；Reviewer 与审核员各自独立审查）

目标：在 core 引入 Experiment 抽象与共享执行器（队列、取消、重试、报告归属），
首先承载跨引擎比较：按 CLI 冻结计划逐块停止旧服务 → 启动 → 经 CLI 身份核验绑定 →
运行双题包 → 全部块后调用 `compare-engines`；D2 决定是否同时迁移长上下文专项。
Studio 不重算判定，只读封存产物。

写入范围：`studio/internal/core/` 新增 experiment 文件、`runs.go`、`local_runs.go`、
`engine_service.go`、`engine_owner*.go`（仅编排所需）、`types.go`，及其测试。

必读：T1、T2 产物；[Studio 面板](../../../docs/subsystems/studio-panel.md)全部分页，尤其
[关键状态转换](../../../docs/subsystems/studio-panel/preview.md#关键状态转换)与
[本地引擎 · 串行执行与所有权](../../../docs/subsystems/studio-panel/local-engines.md#串行执行与所有权)；
`studio/internal/core/README.md`；[运行时执行子系统](../../../docs/subsystems/runtime-execution.md)。

检查：`go test ./...`；取消、中途失败块、服务替换失败、绑定失败的真实失败路径回归；
主机锁与所有权不放宽。停止条件：需要 CLI 新接口而 T2 未提供时，报告协调者，不自行绕过。

## T7 跨引擎比较页面、模拟与一致性（worker；原生验收由另一 worker 会话执行）

目标：侧栏新增入口；设置（配对、协议、顺序预览）、运行（交错块、当前块步骤、
已完成块仅观察中位数）、结果（题包切换、范围、判定与成对条形、仅展示指标及原因）三态；
报告页列出派生产物。模拟流程与生产共用页面构建器，经 `previewIntent` 分流。

写入范围：`studio/internal/pages/` 新增比较页面文件、`window.go` 导航、`reports*.go`、
`preview_*.go` 新增比较模拟、`preview_parity*_test.go` 共享场景、`studio/tests/ui/` 的新增用例。

必读：T4–T6 产物；[UI 调试预览与测试边界](../../../docs/subsystems/studio-panel.md#ui-调试预览)、
[原生 UI E2E](../../../studio/tests/ui/README.md)、[页面参考](../../../studio/docs/pages.md)、
`AGENTS.md`「Studio UI 与模拟行为一致性」全部条款；现有 `preview_local_parity_test.go` 作范式。

检查：带标签页面回归；一致性测试用故意错误证明会失败；`mise run test:studio-ui:full`
（桌面可独占时；否则报告未验证）。分别报告页面测试、编排对照、原生窗口与真实 CLI 结果。

## T8 文档同步与整分支审查（writer，之后 Reviewer）

文档与审查分两个会话，审查对象包含 writer 的改动。

T8a（writer）：核对 T1–T7 与契约、ADR、子系统文档的一致性；`grep -rn` 清扫改名残留；
更新 backlog 与 [studio-panel](../../../docs/subsystems/studio-panel.md) 参考。
写入范围：仅文档（`docs/subsystems/`、`cli/docs/backlog*.md`、本计划进度）。代码问题只报告。

T8b（Reviewer）：审查整分支 diff 的指定 commit，包括文档、脚本与生成物。写入范围：无。

必读：全部上述产物与 diff；`AGENTS.md`「Documentation and contracts」节。

## T9 真机验收（worker；需操作者授权）

在授权设备上由 Studio 执行冻结计划 A/B/A/B/A/B（默认双题包），`compare-engines` 与 `verify` 通过；
设备、预算和停止条件先核对。软件回归与模拟窗口不替代本项证据。

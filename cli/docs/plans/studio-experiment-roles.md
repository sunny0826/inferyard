# Studio 实验模型与设计层重构：角色与派发

本页是[实施计划](studio-experiment-architecture.md)的角色分工与派发规则。

角色来自 Lody 目录（2026-11-07 读取）。全部在注册了本项目且在线的 macOS 开发机上运行。
按角色 ID 和目标机器派发，不覆盖角色的模型。角色 ID、机器 ID 和本机路径只写进任务包，不进入本计划。

| 角色 | 本计划职责 | 不承担 |
| --- | --- | --- |
| 协调者 | T0、派发、写入归属、集成、证据收口、最终审核 | 产品代码、专业判断 |
| 架构师 | T1 | 实施、运行测量 |
| 挑刺者 | T1R | 修改被审内容 |
| worker | T2–T7、T9 与独立验证会话 | 再次派发 |
| Reviewer | 各代码任务的快照审查、T8 整分支审查 | 修改被审源码 |
| 审核员 | T6 跨模型独立复核 | 读取其他审阅者的首轮结论 |
| writer | T8 文档同步 | 产品代码 |

## 派发规则

- 协调者向用户展示本批任务、角色、范围、产物和验收方式，用户明确确认后才派发。
  用户可以一次确认一批任务；新增任务或改变范围、角色时重新确认。
- 只有协调者派发任务。执行角色不转派、不创建子 Agent；需要协助时向协调者提出。
- 每次派发遵循 agent-conductor 技能的派发契约（`references/dispatch.md`）：
  任务包落盘，发送前后运行 `dispatch-gate.mjs` 的 `pre` 与 `post`。任务包写明基线 commit、
  写入范围、命令边界、失败基线和完成条件。
- 实施任务各用一个新 worktree，基线为 T0 提交的分支。执行者在 worktree 内先运行 mise 版本核对。
  `.tools/` 与 `validation/` 被 Git 忽略，不在 worktree 中；任务包用主 checkout 的绝对路径引用，只读。
- 审查与验证用新会话，针对协调者指定的 commit；审查期间不改被审范围。
  返工优先续用原 worker，每轮审查仍用新会话。
- 任务包写明每个技能在执行机器上的绝对路径，并要求回传采用证据。

## 适用技能

| 角色 | 本计划适用的技能 |
| --- | --- |
| 架构师 | `architect`、`how`、`blast-radius` |
| 挑刺者 | `principle-subtract-before-you-add`、`principle-laziness-protocol`、`how` |
| worker | `rigor`、`how`、`tdd`；T7 原生操作加 `control-ui` |
| Reviewer | `how`、`blast-radius`、`rigor/references/code-review.md` |
| 审核员 | `interrogate/references/reviewer-prompt.md`、`how` |
| writer | `technical-writing`、`unslop` |

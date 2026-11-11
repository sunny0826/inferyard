# ADR 057c：v2 封存交接链与比较专用停止规则

状态：Accepted（2026-11-07 用户批准 D1，原话「D1 全批准」）。日期：2026-11-07。本轮基线：`b12eb1b0cee2c34c07ddf3d8b7e651cb310351e2`。
单份完整计划见 [ADR 057b](057-engine-comparison-plan.md)，专用任务见 [ADR 057a](057-comparison-task.md)。
本部分只复用现有 run、batch 和 `service_binding` 证据，不新增批次身份字段、第二份顺序日志或通用任务状态系统。

## 封存交接链

**事实**：同一计划可在不同空目录创建 batch；`history()` 校验 run 身份后按计划顺序组织结果，不能证明实际交接顺序。
`batch_runner` 已把前一 run 写入 `service_binding.previous_run_id`，但当前 reader 尚未核验该链
（`cli/src/inferyard/runtime/batch_state.py:51-87,189-209,227-286`，`runtime/batch_runner.py:207-218`）。

v2 必须按冻结 plan 的槽位逐一选择 run，并读取各 run 已封存的 `service_binding`：

1. 首槽位的 `previous_run_id` 必须为 null。
2. 后续槽位的 `previous_run_id` 必须精确等于前一冻结槽位所选 run 的 `run_id`。
3. 观测缺失、前驱字段缺失、指向不存在或非相邻槽位均阻断；不能用集合相等、reader 返回顺序或事件时间重排替代。

阻断原因至少为（命名存在重叠，T2 冻结契约前必须收敛为每种条件一个规范原因，或写明互斥条件与优先级）：

- `comparison_handoff_missing`：缺少封存交接观测或应有前驱为空。
- `comparison_handoff_broken`：前驱不是前一槽位的选中 run。
- `comparison_handoff_slot_mismatch`：首槽位有前驱，或链指向其他槽位。

**事实**：真实样本 block1 core 的封存前驱为 null，同批 pelican 的前驱为该 core run ID
（`.tools/engine-compare-mlx-20261106/blocks-e1/block1-llama-cpp/batch/runs/*/observations.jsonl` 中的
`service_binding`）。这证明现有证据能表达链，但不等于单份 12 槽位计划已验证。

## batch 读取、污染诊断与拆分

v2 先核验整个 batch，再按冻结槽位拆成各题包 A/B 块；不能直接把 12 个 run 交给现有六块比较逻辑。
计划顺序只提供期望槽位，实际连续性由上节交接链独立证明。随后才复用逐请求、冷缓存和指标计算。

正常 `run --plan` 不产生未知 trial；外来 run 可因目录复制，或普通 `run --config` 的 `output.root`
指向 batch 的 `runs/` 而出现（`cli/src/inferyard/runtime/runner.py:178`）。
当前 reader 会报 `batch_run_identity_mismatch`（`cli/src/inferyard/runtime/batch_state.py:189-209`）。
v2 应把它映射为可封存的阻断诊断并保留原证据错误，不得静默丢弃：

- 未知 trial：`comparison_unplanned_trial`。
- 错完整计划：`run_plan_mismatch`。
- 同 trial 多个根或多次尝试：`comparison_duplicate_trial_attempt`。

这只能检测所提交 batch 内的污染，不是全机额外运行审计。缺少计划槽位仍使用 `planned_run_missing`。

## 比较专用的 CLI 历史停止规则

**事实**：通用 `_next()` 只查未完成窗口和 `not_executed`，可在历史取消或失败后推进；
正常中间槽位完成也返回退出码 3 `awaiting_handoff`（`cli/src/inferyard/runtime/batch_runner.py:69-82,121-135,293-301`）。
因此不能用“非零即作废”或 Go 另写一套判定。

CLI 新增一个比较专用判定来源，逻辑接口为
`comparison_history_stop(plan, selected_runs) -> { trial_id, run_id, reason } | null`。
`run --plan` 的比较模式在执行下一槽位前调用它，v2 派生调用同一实现；执行入口返回 `comparison_history_stopped`
及具体原因，v2 写入同一原因。Studio 只读取 CLI 结果，不拥有第二份规则。

按冻结槽位顺序检查已封存终态：

- 证据、计划、trial、run 或身份核验失败：`comparison_history_identity_error`。
- `summary.stop_reason = user_cancelled` 或存在 cancelled 终态：`comparison_history_cancelled`。
- 其他非 `plan_finished` 停止，或存在 failed/invalid/not_executed：`comparison_history_failed`。
- 完整成功要求 scope/evidence complete 且请求执行终态均为 completed；模型答错但执行 completed 不算执行失败，
  `unscorable` 也只按质量语义处理。正常 `awaiting_handoff` 不是 run 终态，不触发停止。

一旦出现上述停止结果，不再派发后续槽位；已存在的后续 run 在 v2 中同样阻断。
run 创建前的启动或绑定失败由 Studio／手动操作者立即终止本次流程并记录自身错误；
这部分没有 run 封存终态可供 CLI 离线保证，不能声称 CLI 已排除它。

## 保证边界

run 封存完整 plan 及其哈希，因此不能事后另造缩减计划来合法化已有 run
（`cli/src/inferyard/analysis/ledger.py`）；交接链进一步阻止跨 batch 拼选槽位。
但 v2 仍不能证明操作者没有执行过其他完整实验、也没有保留其他未提交证据。
预注册只保证所提交计划、run 集合和交接链满足规则，不扩大为全机行为证明。

## 验证与未验证项

T2 必须先收敛诊断命名：`comparison_handoff_broken` 与 `comparison_handoff_slot_mismatch` 在前驱指向非相邻槽位时重叠；
`comparison_unplanned_trial` 与 ADR 057b 的 `unplanned_run_present` 指同一条件；`execution_order_differs` 与交接链原因的关系也要写明，避免重复报告。

T2 必须覆盖：同计划跨 batch 拼槽位、首槽位有前驱、缺观测、断链、跳槽位、reader 顺序与时间顺序相反、
未知 trial、错计划、同 trial 多次尝试、历史取消后继续调用、正常 handoff、模型答错和 unscorable。
执行入口和 v2 必须分别对照同一明确预期，Studio 只断言它展示 CLI 结果。

上述链规则、共享停止函数和错误映射均未实现；单份 12 槽位计划尚未生成与核验。
本轮只完成源码路径、真实双 trial 样本及交接证据核对，未运行真实引擎或 Studio 验收。

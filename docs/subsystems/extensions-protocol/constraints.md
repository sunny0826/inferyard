# 约束与失败行为

本页是[扩展协议与受控工作流](../extensions-protocol.md) 的子文档，收录「约束与失败行为」的正文；内容不变，入口保留原节标题与锚点。

## 独立守护进程（independent guard）

`IndependentGuard` 在独立 OS 进程运行 `_capture` 循环，按 `POLICY`（默认 0.5 秒间隔、90°C 温度上限、CPU 20% 上限，macOS 用 `spawn`、其余 `fork`）逐样本调用 `SafetyGuard.check` 并额外检查输出根磁盘余量；每样本经 `open_nofollow` 独占创建写入 `guardian.jsonl` 并 flush+fsync，不安全即置 `unsafe` 位。收到 shutdown 后再取一个终止样本，括住臂的 flush/seal/close 完整生命周期。`clock_id` 为 boot id 加 `:CLOCK_MONOTONIC`；`start()` 五秒内未就绪即失败，`close()` 不干净退出会终止进程并抛证据错误，`evidence()` 读回样本，存在截断/缺测或非零退出码即拒绝。臂与 guard 的时钟绑定、进程身份（guard PID 不得等于客户端 PID）、策略哈希认证由 `assess_total` 逐项核验。

## 四臂对照的硬错误与缺陷

`assess_total` 区分两类失败：臂数不是 4、顺序不是 off/on/on/off、臂身份哈希或 `protocol_sha256` 不符、臂窗口非法、请求时长之和超出臂窗口、相邻臂时间重叠是硬错误（`EvidenceError`，重算即拒）；guard 样本少于 2 个、采样间隔超过 `max_guard_gap_ns`、样本不安全、环境身份缺失或中途变化（live 还要求 macOS 电源策略或 Linux governor/EPP 字段完整、策略哈希与冻结值一致、进程启动 ticks 为正）、臂覆盖清单缺项、输出在臂间不一致、冻结墙钟预算超限等记为 `reasons` 缺陷，使 `valid_control: false`。v2 额外要求 on 臂 `measurement_path == "run_trial.v1"` 且 `trial_plan_sha256` 绑定、覆盖清单含 `offline_reconstruction` 项。全部无缺陷后才计算 (off1,on2)、(off4,on3) 两对的请求时长中位变化比与全生命周期变化比，`within_tolerance` 要求两对四项全部落在 `tolerance_ratio` 内；`hardware_qualified` 还要 `evidence_kind == "live"`。`run_total` 不替调用者重试：守护进程停止即中断后续臂，出错的臂保留未完成状态交由调用者处置，总墙钟受 `max_wall_seconds` 约束。

## v2 行级核验（verify_control_rows）

`verify_packet` 对 v2 packet 调 `verify_control_rows`（[`trial_control_evidence.py`](../../../cli/src/inferyard/extensions/trial_control_evidence.py)）：保存的 `measurement-plan.json` 必须是合法 plan 且 `plan_sha256` 等于 `spec.trial_plan_sha256`；`guardian.jsonl` 原始样本必须与 packet 内 guard 证据完全一致；每个 on 臂的 trial 目录按 manifest 哈希绑定，其 plan 内容与源码身份重算一致；每个 off 臂的 baseline 文件定义、`plan.json` 快照与 `clock_id` 绑定一致；两臂冻结输入（config/bundle）哈希等于 spec；每条请求的 `t_send_ns`/`t_terminal_ns` 必须落在所属臂窗口内；对 trial 行跑 `request_projection`、对 baseline 行按事件重建，结果必须与 packet 行一致（`total_control_arm_requests_do_not_replay`）；`child-evidence.json` 清单与期望的子证据集合完全一致。`read_trial_plan` 在冻结与 live 两侧校验 trial 计划：workload 协议必须为 `fixed`、无 `output_mode`/`cache_protocol` 覆盖、`case_order` 与 spec 一致、config/bundle 摘要一致。

## 其余约束

- **资格边界**：`replay` 拒绝 live packet（`offline_import_cannot_grant_live_qualification`）；反向地，fixture 证据无论归约结果如何都不会产出 `hardware_qualified: true`。闭集负载的 `hardware_qualified` 还要求完整执行、客户端调度与容量双合格；对照协议的还要求四对臂全部在容差内且 guard 证据无缺陷。
- **冻结绑定**：计划、配置、题包、源码身份、守护策略与 trial 计划的任何哈希不匹配都拒绝执行或核验；live packet 的 `identity.json` 必须 `verification == "verified"`，子证据 manifest 哈希必须一致。已删除的命令别名与旧参数不接受。
- **槽位与并发**：`concurrency` 不得超过冻结的 `server_slots`；槽位身份（id 连续、`is_processing` 布尔）每次读取都重验；取消后必须证明 idle，否则保留 dirty 并停止发送。闭集负载一次执行失败即置停止位，不重试、不改样本量；worker 共享冻结题序队列，每请求记 `dispatch_delay_ns` 调度滞后，总墙钟受 `max_wall_seconds` 约束，超时置停止位并保留已落盘行。
- **客户端容量**：`qualified` 要求证明区间完整包含资源窗口、事件循环样本至少 2 个、进程 CPU 占比不超过 `max_client_cpu_fraction`、最大事件循环滞后不超过 `max_dispatch_delay_ns`，时钟 id 与进程身份齐全；闭集归约的窗口从首个请求发起到最后排空结束计算，goodput 分子要求 completed、时延在 `latency_limit_ms` 内且质量合格（`require_quality` 时），`N03_token_ITL` 恒为 `null`（无已核验逐 token 事件）。
- **有界预算**：工具链调用次数、轮数、case 数与参数 JSON 大小（4096 字节）均有硬上限；SSE 组装字节预算 1 MiB；非流式响应体超过 1 MiB 判证据错误。所有上限在 schema 或 `validate_spec` 处强制。工具 mock 是进程内确定性函数，`limitations` 标注 `deterministic_mock_functions_only`；`reviewed_formal` track 需要 `content_review_sha256` 等于 cases 摘要，CLI 只执行 `capability_diagnostic`。
- **有效参数核验**：每个请求后核验该槽位可见的 `seed`/`temperature`/`top_k`/`top_p`/`min_p`/`penalty` 与冻结生成参数一致（1e-6 容差），`n_predict`、`stop` 精确相等；工具请求还核验模板 token 计数与 context 预算。答案经 redactor 脱敏，发现凭据即不评分（quality 记 `null`）。
- **平台限制**：live 在 Windows 直接拒绝（`windows_phase2_live_not_supported`）；守护进程证据不完整或其进程非零退出即整体失败。
- **报告诚实性**：packet/summary 以 `evidence_kind`（`fixture`/`live`）与 `hardware_qualified` 区分软件验证与真机资格（`hardware_qualified` 仅在 live 且对照通过时为真）；对照归约的 `limitations` 明示共享守护成本未估计、仅声明 case 与单机、非总体置信、方法墙钟占比不是因果减速；闭集归约明示非开放到达负载。
- **终态守恒**：闭集负载沿用五个终态（completed/failed/cancelled/invalid/not_executed），`not_executed` 不得携带请求身份或时间戳；工具协议终态不含 `invalid`。答案被判错不视为协议失败，质量不合格只影响 goodput 分子；`reduce_tools` 重算四项检查与 packet 不一致即拒绝（`tool_checks_do_not_replay`）。

归约失败统一抛 `EvidenceError`（退出码 4）；输入/预检阻断抛 `PreflightError` 或 `ContractError`（退出码 2）；协议执行完成但安全停止或排空未证实也按证据错误处理。扩展容器不套用 v2 run 布局规则，独立封存协议保持不变，见 [ADR 048](../../../cli/docs/decisions/048-run-evidence-containers.md)。

# 运行路径

本页是[运行时执行与引擎适配](../runtime-execution.md) 的子文档，收录「运行路径」的正文；内容不变，入口保留原节标题与锚点。

单次命令 `check`/`run`（`runner.execute_async`）与批量 `run --plan`/`resume`（`batch_runner.execute_async`）都汇入同一个 `run_trial`，差别在锁的持有范围与计划来源；分派表见 [CLI 与应用分发](../cli-dispatch.md)。

1. 单次入口编译 `compile_single_plan` 为一个 workload、一个 trial，创建 `TrialJournal`（含脱敏器与配置来源快照）后进入 `run_trial`；带 `--from-run` 的重跑先经 `load_rerun` 校验父运行 manifest 与身份、替换 `--host`/`--port` 启动参数并要求新服务进程。批量入口在 `HostLock` 上下文中打开批次根目录、校验冻结计划与历史，逐 trial 调用 `run_trial` 并传入共享锁。批量计划的语义见 [配置、契约与冻结计划](../config-plans.md)，批次历史投影规则见 [数据契约](../../../cli/docs/data-contract.md)。
2. `run_trial` 取主机锁（或复用批量传入的锁），读取/创建主机状态；凭据只经 `endpoint.api_key_env` 环境变量引用，缺失即 `credential_reference_unavailable`。正式（非诊断）单次运行先经人工题包审核（`require_review`，事实源在配置侧），审核绑定题目、答案与规则哈希。
3. 身份预检 `run_preflight` 调 `static_preflight`（事实源在 [平台采集与设备身份](../platform-sampling.md)）：模型/引擎/模板文件哈希、运行时库 manifest、PID 与启动时间、监听端口归属，并生成环境快照；运行时库 manifest 的哈希表并入 identity 观察随证据保存。此后每个 guard（`runner._guard`）在关键节点重验文件未变且进程身份未漂移。lab 适配器（kvmem/ninfer）改走 Windows lab 身份绑定。
4. 按 `adapter_factory(config["engine"]["adapter"])` 创建适配器；批量路径按适配器选择配套采集器（`adapter_collector_id`）。
5. `prepare_service`：lab 适配器先 `verify_properties` 再处理 dirty——`verify_properties` 核对引擎 build、模型 sha256/字节数与模板哈希（不符即 `lab_service_properties_mismatch`）；有 `recovery_confirm` 时调 `lock.verify_manual_recovery` 比对 token 与恢复说明，核验旧进程已消失且新进程身份不同；状态 dirty 且端点身份与脏记录不一致时拒绝。随后做初始空闲观察 `observe_service`，失败则写入 `initial_idle_unknown` 脏记录并抛 `initial_service_not_idle`；空闲确认后 `clean_observed` 清 dirty，恢复记录以 `recovery` 观察落盘。
6. `collect_budgets`（事实源在证据包）计算模板后输入预算：Prism 系走 `count_template` 本地套用模板加 tokenize（来源 `apply-template+tokenize`），lab 适配器走 `/lab/v1/token-budget` 服务端计数；批量路径再做 `bind_token_counts` 与 `check_input_target`，输入长度不达标即阻断。
7. 创建 `TrialRequests` 与资源采样任务，进入 `probe_service`：普通与流式各发一次探针请求，每次都用 `verify_effective` 从服务端参数端点核对 `requested` 与 `effective`；结果按 ordinary/stream 阶段记录为 `effective_parameters` 观察。
8. `warmup_and_baseline` 发完配置的预热请求后进入基线静默期；`run_formal` 按冻结顺序串行执行正式请求（索引与 prompt 与计划不一致即 `formal_request_out_of_order`/`formal_prompt_differs_from_plan`）。持续负载改走 `run_duration`：按 `admission_deadline_ns` 决定可否发送、按 `drain_deadline_ns` 停止，`window_completed` 只在正常到时且排空闭合时为真（`planned = null`、窗口与排空语义见 [实验设计](../../../cli/docs/experiments/design.md)）；`capacity_stop: first_failed_request` 在首个失败请求处停止扫描（`capacity_scan_failed_request`）。单次 `check` 类入口（`profile.kind != "run"`）跳过正式执行并以 `check_only_no_formal_cases` 收尾；诊断运行完成正式请求后仍以 `diagnostic_run_not_formal` 退出 3。
9. 每个请求的串行生命周期在 `TrialRequests.one` 内：guard 重验身份 → 采样任务存活 → 请求前空闲观察（失败写 `pre_request_idle_unknown` 脏记录并抛 `service_not_idle`）→ `store.prepare_request` 把请求身份落盘（key 形如 `<run_id>-<phase>-<序号>`）→ `lock.dirty` 标记 → 发送并流式记录事件 → 终态 → `request_finished` 事件 → 排空期空闲观察 → 空闲确认后 `clean_observed` 清 dirty → 评分并记录 `score` 事件。发送前锁被释放或丢失即 `host_lock_not_held`。日志写序与落盘要求见 [证据、封存与离线读取](../evidence-storage.md)。
10. `finish_trial` 停止采样器、关闭适配器、补记安全终检/环境终检/执行预算观察，`seal_stopped` 封存后释放锁；采样器或适配器收尾失败把结果改为退出码 4。批量循环按 `_next` 选择下一个 trial；切换到新 workload 或配置要求新进程、而请求未提供 `--workload`/`--handoff-note` 与新端点绑定时，返回 `awaiting_handoff` 等待显式交接，恢复 token 在首个 trial 后清除。任一已跑 trial 含 `unscorable` 请求时批量结果也以 3 收尾，已完成 trial 的证据不回改。

返回侧：单次路径在封存后从账本重建 summary（`read_trial`）随结果返回；批量路径读运行投影并附剩余预算、进度与重复指标。退出码与状态词的映射在 CLI 层完成，见 [CLI 与应用分发](../cli-dispatch.md)。

Prism 的模板服务核验还生成 `template_identity`，由 `run_trial` 并入 identity 观察；
`prepare-length` 将同一证明写入封存的 identity.json。离线读取不访问模板外部路径，
按 [模板身份契约](../../../cli/docs/contracts/prism-template-contract.md) 重算。

overhead、length、engine-fit 三条辅助通路只在约束节标注与正式通路的差异，本文不展开其内部细节。

# 运行时执行与引擎适配

`runtime/` 与 `adapters/` 在操作者已启动并声明身份的本地模型服务上执行冻结的请求序列：主机共享锁与 crash 持久的 dirty 恢复、服务身份预检驱动的执行准入、请求身份先落盘再发送、取消与按冻结期限排空、引擎传输与响应/用量解析。它不编译配置、不写证据日志本体、不采集平台指标，也不启动或停止外部服务。

边界：命令参数与分派见 [CLI 与应用分发](cli-dispatch.md)；单次/批量配置编译为 workload 与 trial 见 [配置、契约与冻结计划](config-plans.md)；日志追加、封存与账本见 [证据、封存与离线读取](evidence-storage.md)；进程身份核验、空闲来源与平台采集见 [平台采集与设备身份](platform-sampling.md)；engine-fit 诊断路径复用本参考的主机锁与恢复机制，其余见 [engine-fit 引擎诊断](engine-fit.md)。通用层职责见 [架构](../../cli/docs/architecture.md) 的「请求与运行」节。

本参考覆盖 `runtime/` 与 `adapters/` 内的正式与 lab 通路。`runtime/engine_fit.py` 与 `adapters/engine_fit.py` 是 engine-fit 独立诊断通路：同样经过 `HostLock` 与恢复核验，但产物独立封存、不进入正式评分，细节归 [engine-fit 引擎诊断](engine-fit.md)。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 主机共享锁 | 主机级非阻塞独占锁与 dirty 状态文件；文件身份（dev/inode/属主）变更即拒绝 | `HostLock`、`LOCK_PATH`、`STATE_PATH`、`LOCK_FORMAT_VERSION` 在 [`cli/src/inferyard/runtime/lock.py`](../../cli/src/inferyard/runtime/lock.py)；状态解码/校验/发布 `validate_state`、`publish_state` 在 [`cli/src/inferyard/runtime/host_files.py`](../../cli/src/inferyard/runtime/host_files.py) |
| 单次入口 | `check`/`run` 单次命令的异步执行；单次计划编译、凭据环境变量解析、Journal 创建 | `execute_async`、`execute` 在 [`cli/src/inferyard/runtime/runner.py`](../../cli/src/inferyard/runtime/runner.py)；`compile_single_plan` 在 [`cli/src/inferyard/config/single_plan.py`](../../cli/src/inferyard/config/single_plan.py) |
| 批量入口 | 冻结计划下逐 trial 串行执行、服务绑定与 handoff 交接、wall 预算检查 | `execute_async`、`execute` 在 [`cli/src/inferyard/runtime/batch_runner.py`](../../cli/src/inferyard/runtime/batch_runner.py)；批次快照与绑定 `open_batch`、`bind_service`、`require_replaced_service` 在 [`cli/src/inferyard/runtime/batch_state.py`](../../cli/src/inferyard/runtime/batch_state.py) |
| trial 生命周期 | 锁内预检、服务准备、探针、预热/基线、正式执行、终检与封存的编排 | `run_trial` 在 [`cli/src/inferyard/runtime/trial_runner.py`](../../cli/src/inferyard/runtime/trial_runner.py)；阶段函数 `prepare_service`、`probe_service`、`warmup_and_baseline`、`run_formal` 在 [`cli/src/inferyard/runtime/trial_lifecycle.py`](../../cli/src/inferyard/runtime/trial_lifecycle.py)；收尾 `finish_trial`、`seal_trial` 在 [`cli/src/inferyard/runtime/trial_finalization.py`](../../cli/src/inferyard/runtime/trial_finalization.py) |
| wall 预算 | 单调时钟预算与到期取消 | `TrialDeadline`、`SingleProfile` 在 [`cli/src/inferyard/runtime/trial_profile.py`](../../cli/src/inferyard/runtime/trial_profile.py) |
| 请求生命周期 | 每个串行请求的准入检查、落盘、发送、终态、空闲确认与评分衔接 | `TrialRequests.one`、`TrialRequests.idle` 在 [`cli/src/inferyard/runtime/request_execution.py`](../../cli/src/inferyard/runtime/request_execution.py) |
| 服务空闲/清洁观察 | 区分 lab 全引擎空闲观察与 native 仅客户端准入；清 dirty 的条件 | `observe_service`、`clean_observed`、`native` 在 [`cli/src/inferyard/runtime/service_observation.py`](../../cli/src/inferyard/runtime/service_observation.py) |
| 服务复用与交接 | 同进程复用条件、替换判定、恢复与 drain 证据要求 | `same_process`、`reuse_allowed`、`require_transition`、`record_reuse` 在 [`cli/src/inferyard/runtime/service_reuse.py`](../../cli/src/inferyard/runtime/service_reuse.py) |
| 安全停止 | 冻结策略的周期温度/外部 CPU/环境检查，一次停止无重试 | `SafetyGuard`、`monitor` 在 [`cli/src/inferyard/runtime/safety.py`](../../cli/src/inferyard/runtime/safety.py) |
| duration 窗口 | 固定时窗循环、发送准入截止与排空截止 | `run_duration` 在 [`cli/src/inferyard/runtime/duration_driver.py`](../../cli/src/inferyard/runtime/duration_driver.py) |
| 采样调度 | 资源样本按声明周期调度、1 秒周期环境观测、调度延迟记录 | `run_sampler`、`advance` 在 [`cli/src/inferyard/runtime/environment_schedule.py`](../../cli/src/inferyard/runtime/environment_schedule.py) |
| 信号处理 | 单次与批量进程的信号捕获与恢复 | `install_termination_handler` 在 [`cli/src/inferyard/runtime/signals.py`](../../cli/src/inferyard/runtime/signals.py)；安全停止轨迹 `safety_trace.py` 同目录 |
| 适配器注册 | 适配器/采集器工厂、Windows 批量适配器白名单 | `adapter_factory`、`collector_factory`、`adapter_collector_id`、`WINDOWS_BATCH_ADAPTERS` 在 [`cli/src/inferyard/registry.py`](../../cli/src/inferyard/registry.py) |
| 请求构造与终态 | 按适配器分派的请求体构造、空闲证据来源、引擎无关的终态记录 | `request_body`、`idle_evidence` 在 [`cli/src/inferyard/adapters/requests.py`](../../cli/src/inferyard/adapters/requests.py)；`ResponseState` 在 [`cli/src/inferyard/adapters/response_state.py`](../../cli/src/inferyard/adapters/response_state.py) |
| Prism 系适配器 | PrismML llama-server 的 HTTP/SSE 普通与流式解析、`/slots` 空闲观察、属性核验 | `PrismAdapter`、`SSEDecoder`、`StreamProtocol` 在 [`cli/src/inferyard/adapters/prism.py`](../../cli/src/inferyard/adapters/prism.py)；`LlamaCppAdapter` 在 [`cli/src/inferyard/adapters/llama_cpp.py`](../../cli/src/inferyard/adapters/llama_cpp.py) |
| Lab 适配器 | lab 协议服务实例绑定、普通/流式生成解码、取消与生命周期观察、有效参数核验 | `LabAdapter` 在 [`cli/src/inferyard/adapters/lab_adapter.py`](../../cli/src/inferyard/adapters/lab_adapter.py)；`GenerationDecoder` 在 [`cli/src/inferyard/adapters/lab_generation.py`](../../cli/src/inferyard/adapters/lab_generation.py)；`LabParameters.verify_properties`、`verify_effective` 在 [`cli/src/inferyard/adapters/lab_parameters.py`](../../cli/src/inferyard/adapters/lab_parameters.py)；`NativeObservation.discover`、`wait_ready` 在 [`cli/src/inferyard/adapters/native_observation.py`](../../cli/src/inferyard/adapters/native_observation.py) |
| 长度准备 | `prepare-length` 的模板计数拟合，只调管理端点与 token 预算、不做生成 | `prepare`/`execute` 在 [`cli/src/inferyard/runtime/length_prepare.py`](../../cli/src/inferyard/runtime/length_prepare.py)；`fit_length`、`validate_spec` 在 [`cli/src/inferyard/runtime/length_builder.py`](../../cli/src/inferyard/runtime/length_builder.py)；`count_template` 在 [`cli/src/inferyard/runtime/template_tokens.py`](../../cli/src/inferyard/runtime/template_tokens.py) |
| 固定输出 | strict-output 兼容性导入、达标率计算 | `target_rate` 在 [`cli/src/inferyard/runtime/fixed_output.py`](../../cli/src/inferyard/runtime/fixed_output.py)；模式判定在 [`cli/src/inferyard/config/fixed_output.py`](../../cli/src/inferyard/config/fixed_output.py)，探针核验在证据包 |
| 采集开销测量 | `overhead` 命令的 ABBA 协议执行与开销评估，复用 trial 生命周期 | `run_overhead`、`read_overhead` 在 [`cli/src/inferyard/runtime/overhead_runner.py`](../../cli/src/inferyard/runtime/overhead_runner.py)；`assess_overhead`、`freeze_protocol`、`ORDER` 在 [`cli/src/inferyard/runtime/collector_overhead.py`](../../cli/src/inferyard/runtime/collector_overhead.py) |

主机状态的事实源是锁旁的状态文件，不是任何运行目录：`HostLock.dirty` 写入 `dirty`、`dirty_token`、`run_id`、`kind`、`endpoint`、`server_pid`、`process_start_ticks`、`request_id`；`HostLock.clean` 只翻转 `dirty` 位。主机互斥状态不是测量文档，不进运行证据。状态文件格式与旧状态处置见 [当前格式契约](../../cli/docs/contracts/inferyard-current-format.md#主机状态)，理由见 [ADR 039](../../cli/docs/decisions/039-remove-host-state-migration.md)。单次入口先创建 Journal 与冻结输入再取锁（`SingleProfile` 的创建顺序在 `trial_profile.py` 注释中声明）；批量入口由 `batch_runner.execute_async` 持锁跨多个 trial。

适配器层次：`LabAdapter` 组合 `LabParameters`（参数核验）、`NativeObservation`（能力发现）与 `LabTransport`（HTTP 端点读取）；`KVMemAdapter`、`NInferAdapter` 继承 `LabAdapter`，`LlamaCppAdapter` 继承 `PrismAdapter`。选择由 `adapter_factory` 按 `engine.adapter` 标识完成，新增适配器不改动调用方。`overhead` 命令同样汇入 `run_trial`，但把采样器换成开关组合（`ResourceOffSampler`、`BoundaryOnlySampler`、`EnvironmentOnlySampler`）以测量采集器自身开销，产物与正式运行分开。

## 运行路径

单次命令 `check`/`run`（`runner.execute_async`）与批量 `run --plan`/`resume`（`batch_runner.execute_async`）都汇入同一个 `run_trial`，差别在锁的持有范围与计划来源；分派表见 [CLI 与应用分发](cli-dispatch.md)。

1. 单次入口编译 `compile_single_plan` 为一个 workload、一个 trial，创建 `TrialJournal`（含脱敏器与配置来源快照）后进入 `run_trial`；带 `--from-run` 的重跑先经 `load_rerun` 校验父运行 manifest 与身份、替换 `--host`/`--port` 启动参数并要求新服务进程。批量入口在 `HostLock` 上下文中打开批次根目录、校验冻结计划与历史，逐 trial 调用 `run_trial` 并传入共享锁。批量计划的语义见 [配置、契约与冻结计划](config-plans.md)，批次历史投影规则见 [数据契约](../../cli/docs/data-contract.md)。
2. `run_trial` 取主机锁（或复用批量传入的锁），读取/创建主机状态；凭据只经 `endpoint.api_key_env` 环境变量引用，缺失即 `credential_reference_unavailable`。正式（非诊断）单次运行先经人工题包审核（`require_review`，事实源在配置侧），审核绑定题目、答案与规则哈希。
3. 身份预检 `run_preflight` 调 `static_preflight`（事实源在 [平台采集与设备身份](platform-sampling.md)）：模型/引擎/模板文件哈希、运行时库 manifest、PID 与启动时间、监听端口归属，并生成环境快照；运行时库 manifest 的哈希表并入 identity 观察随证据保存。此后每个 guard（`runner._guard`）在关键节点重验文件未变且进程身份未漂移。lab 适配器（kvmem/ninfer）改走 Windows lab 身份绑定。
4. 按 `adapter_factory(config["engine"]["adapter"])` 创建适配器；批量路径按适配器选择配套采集器（`adapter_collector_id`）。
5. `prepare_service`：lab 适配器先 `verify_properties` 再处理 dirty——`verify_properties` 核对引擎 build、模型 sha256/字节数与模板哈希（不符即 `lab_service_properties_mismatch`）；有 `recovery_confirm` 时调 `lock.verify_manual_recovery` 比对 token 与恢复说明，核验旧进程已消失且新进程身份不同；状态 dirty 且端点身份与脏记录不一致时拒绝。随后做初始空闲观察 `observe_service`，失败则写入 `initial_idle_unknown` 脏记录并抛 `initial_service_not_idle`；空闲确认后 `clean_observed` 清 dirty，恢复记录以 `recovery` 观察落盘。
6. `collect_budgets`（事实源在证据包）计算模板后输入预算：Prism 系走 `count_template` 本地套用模板加 tokenize（来源 `apply-template+tokenize`），lab 适配器走 `/lab/v1/token-budget` 服务端计数；批量路径再做 `bind_token_counts` 与 `check_input_target`，输入长度不达标即阻断。
7. 创建 `TrialRequests` 与资源采样任务，进入 `probe_service`：普通与流式各发一次探针请求，每次都用 `verify_effective` 从服务端参数端点核对 `requested` 与 `effective`；结果按 ordinary/stream 阶段记录为 `effective_parameters` 观察。
8. `warmup_and_baseline` 发完配置的预热请求后进入基线静默期；`run_formal` 按冻结顺序串行执行正式请求（索引与 prompt 与计划不一致即 `formal_request_out_of_order`/`formal_prompt_differs_from_plan`）。持续负载改走 `run_duration`：按 `admission_deadline_ns` 决定可否发送、按 `drain_deadline_ns` 停止，`window_completed` 只在正常到时且排空闭合时为真（`planned = null`、窗口与排空语义见 [实验设计](../../cli/docs/experiments/design.md)）；`capacity_stop: first_failed_request` 在首个失败请求处停止扫描（`capacity_scan_failed_request`）。单次 `check` 类入口（`profile.kind != "run"`）跳过正式执行并以 `check_only_no_formal_cases` 收尾；诊断运行完成正式请求后仍以 `diagnostic_run_not_formal` 退出 3。
9. 每个请求的串行生命周期在 `TrialRequests.one` 内：guard 重验身份 → 采样任务存活 → 请求前空闲观察（失败写 `pre_request_idle_unknown` 脏记录并抛 `service_not_idle`）→ `store.prepare_request` 把请求身份落盘（key 形如 `<run_id>-<phase>-<序号>`）→ `lock.dirty` 标记 → 发送并流式记录事件 → 终态 → `request_finished` 事件 → 排空期空闲观察 → 空闲确认后 `clean_observed` 清 dirty → 评分并记录 `score` 事件。发送前锁被释放或丢失即 `host_lock_not_held`。日志写序与落盘要求见 [证据、封存与离线读取](evidence-storage.md)。
10. `finish_trial` 停止采样器、关闭适配器、补记安全终检/环境终检/执行预算观察，`seal_stopped` 封存后释放锁；采样器或适配器收尾失败把结果改为退出码 4。批量循环按 `_next` 选择下一个 trial；切换到新 workload 或配置要求新进程、而请求未提供 `--workload`/`--handoff-note` 与新端点绑定时，返回 `awaiting_handoff` 等待显式交接，恢复 token 在首个 trial 后清除。任一已跑 trial 含 `unscorable` 请求时批量结果也以 3 收尾，已完成 trial 的证据不回改。

返回侧：单次路径在封存后从账本重建 summary（`read_trial`）随结果返回；批量路径读运行投影并附剩余预算、进度与重复指标。退出码与状态词的映射在 CLI 层完成，见 [CLI 与应用分发](cli-dispatch.md)。

overhead、length、engine-fit 三条辅助通路只在约束节标注与正式通路的差异，本文不展开其内部细节。

## 约束与失败行为

### 主机锁与 dirty 恢复

- 锁是非阻塞独占的（Linux `flock`，Windows `msvcrt.locking`），争用即 `host_lock_unavailable`；锁与状态文件要求安全文件身份，且每次写入前后重验 dev/inode 与父目录身份，漂移即 `host_state_identity_changed`。锁文件永不删除，见 [用法](../../cli/docs/usage.md#取消与-dirty-恢复)。锁路径按平台选择：Linux 为 `/var/tmp/inferyard-host.{lock,state.json}`，Windows 为 Common AppData 下同名文件。
- 状态文件读写有硬边界：`open_nofollow` 打开，POSIX 上要求属主正确且权限 `0600`、无 reparse、硬链接数为 1；读取限 1 MiB 且要求读前读后文件身份与大小时间戳一致（`host_state_changed_during_read`）；发布走证据包的同步原子写（`atomic_bytes`）。字段校验在 `validate_state`：dirty 为真或带脏字段时，`dirty_token`/`run_id`/`kind`/`endpoint`/`request_id` 必须是非空字符串，`server_pid`/`process_start_ticks` 必须是正整数。
- `HostLock.write` 要求持锁（`host_lock_not_held`），写入前重验全部已锁文件的身份；恢复核验通过也只返回恢复记录，清 dirty 仍由调用方在新服务空闲确认后执行。
- dirty 语义：请求发送前必须置 dirty；只有空闲证据确认后才允许 clean。初始空闲、请求前空闲、取消/超时后的排空空闲任一环节无法确认，都保留 dirty 并给出对应原因（`initial_idle_unknown`、`pre_request_idle_unknown` 或保留请求级脏记录）。
- 人工恢复绑定当前 `dirty_token` 与非空说明；程序仍核验旧进程消失（启动 ticks 比对）、新进程身份不同，且恢复后仍须通过新服务的身份预检与空闲观察。批量路径中恢复 token 一次性，首个 trial 后清除。单次路径在取消发生时若初始空闲仍未确认，会补写带 `kind` 的 `initial_idle_unknown` 脏记录，证据写入失败再把结果降为退出码 4。
- 服务复用受 `reuse_allowed` 约束：任一侧 `require_fresh_process`、model/engine/generation 或关键 conditions/端点变化都禁止同进程复用；非串行续测复用还要求上次运行的 drain 证据（`service_reuse_drain_evidence_missing`）。同进程复用遇上 dirty 状态必须走绑定恢复（`dirty_service_requires_bound_recovery`）。

### dirty 状态转换

主机状态机只有 `dirty` 一个布尔位，转换如下（写入方均为 `HostLock` 的方法）：

| 转换 | 触发 | 证据条件 |
| --- | --- | --- |
| clean → dirty | 每次请求发送前（`dirty(run_id, endpoint, request_id, kind=…)`，request_id 为该请求 key） | 请求身份已落盘（`prepare_request` 先于 dirty） |
| clean → dirty | 初始空闲观察失败（`initial_idle_unknown`）或请求前空闲观察失败（`pre_request_idle_unknown`） | 空闲观察超时未确认 |
| dirty → clean | 空闲确认（`clean_observed` → `lock.clean`） | lab/Prism：引擎级空闲观察通过；native：持有 `client_http` 完成收据 |
| dirty → clean | 绑定恢复（`verify_manual_recovery` + `completion_scope: "process_replacement"`） | token 与说明匹配、旧进程消失、新进程身份不同且通过身份预检与空闲观察 |
| dirty 保持 | 取消/超时后空闲无法确认、native 无收据、证据写入失败 | 下一次运行必须携带恢复声明或复用同一服务身份 |

### 请求准入与执行

- 请求身份（`prepare_request` 落盘 + dirty）必须先于发送；必要证据写入失败即停发并保留 dirty，不产生无证据结果。duration 准入下 `before_send` 在发送点重查准入截止，逾期返回 `None` 且不创建持久尝试；准入截止前请求被跳过属证据错误（`duration_request_skipped_before_deadline`）。
- 没有隐式重试：`request_started` 固定 `attempt: 1`；正式请求按冻结索引顺序逐个执行，顺序或 prompt 不符即证据错误。五个终态与守恒等式的语义见 [架构](../../cli/docs/architecture.md) 与 [运行证据布局契约](../../cli/docs/contracts/run-evidence-layout.md)。
- 取消（`CancelledError`，含 wall 预算到期与安全停止触发）后取消生成任务；已发送的请求以最后一个已知状态合成 `cancelled` 终态落盘，未发送的只留脏记录。安全停止保留原因与计数、不当作模型失败，映射为退出码 3，见 [CLI 与应用分发](cli-dispatch.md)。
- 采样任务与请求并发竞争（`asyncio.wait` FIRST_COMPLETED）：采样器先结束即取消生成并按证据错误处理；请求进行中采样器停止为 `sampler_stopped_early`。工具/IO 失败不虚构模型终态，持久化的尝试保留为中断状态（终态 `invalid`，归属见 [架构](../../cli/docs/architecture.md) 的状态表）。
- wall 预算由 `TrialDeadline` 以单调时钟记账：批量 trial 的允许时长取 trial 预算与批次剩余 wall 的较小者，非正即 `experiment_wall_budget_exhausted`；到期取消当前任务并给请求标记 `single_wall_budget_exhausted`（单次）或 `trial_wall_budget_exhausted`（批量）原因；操作者信号取消映射为退出码 130，二者都不缩小质量分母。批量路径还把剩余预算（`remaining_budget`）随结果返回。
- 批量 resume 只续跑最近一次未完成运行的缺口（`resume_selection` 与 not_executed 身份，事实源在证据账本），不重跑已完成请求；恢复源必须是批次 runs 目录下的最新中断运行（`resume_requires_latest_unfinished_run`）。
- 请求边界事件（`arrival_capture`、`block_arrived` 与采样器的 `boundary("request_start"/"request_end"/"idle")` 回调）把线数据到达与请求归属传给采集器，跨边界样本的归因规则见 [平台采集与设备身份](platform-sampling.md)。
- 预检类失败按阶段定级：正式执行已开始后的 `PreflightError`/`ContractError` 归为退出码 3（运行不完整），之前归为 2（预检阻断）；证据与内部故障为 4，见 [CLI 与应用分发](cli-dispatch.md) 的退出码表。
- Windows 上非 `WINDOWS_BATCH_ADAPTERS` 白名单内的适配器在批量实时路径阻断（`windows_phase2_live_not_supported`）；白名单当前为 `kvmem`、`ninfer`、`prism_llama_server_v1`。lab 适配器在非 Windows 平台阻断（`lab_adapter_requires_windows`）。模拟/软件验证不授予真机资格，见 [数据契约](../../cli/docs/data-contract.md#身份与测量资格分派)。
- `prepare-length` 是操作者绑定的辅助通路：复用锁、身份预检与空闲观察，但只做模板计数拟合，不发送生成请求；服务 dirty 时阻断（`length_preparation_requires_clean_service`），spec 触到脱敏内容即拒绝。产物自标 `preparation_not_benchmark`，不进入正式测评。
- 环境准入只影响准入、不过滤观察：`run_preflight` 传入的 `environment_admission` 策略把差异分为 `differences`（全部记录）与 `blockers`（阻断项），诊断运行不阻断；环境事实源在 [平台采集与设备身份](platform-sampling.md)。
- 批量路径把 `store.cache_protocol` 交给适配器，请求体按冻结序号注入 `cache_prompt`/`n_cache_reuse`（`cache_execution.request_value`）；缓存语义与资格限制见 [数据契约](../../cli/docs/data-contract.md) 与 [实验设计](../../cli/docs/experiments/design.md)，运行层只按冻结值转发。
- 适配器在构造时把 URL 重新解析并固定为 origin（`resolve_loopback_origin`）；`transport_config` 用该 origin 替换 guard 用配置中的 URL，但不改变冻结输入，guard 据此重验监听归属。
- strict-output 模式在请求体加 `ignore_eos`，探针响应经 `verify_probe` 核验输出预算达标后才进入正式执行；达标率由 `target_rate` 按 completed/failed 终态与端点用量口径计算，口径细则见 [指标定义](../../cli/docs/experiments/metrics.md)。

### 适配器边界

- 适配器只做传输与解析：构造请求体、按超时收发、解析普通/流式响应、用量与结束证据、空闲观察、取消；不下载模型、不启动/停止/重启服务。服务生命周期归操作者（批量 handoff）或 Studio，见 [Studio 面板](studio-panel.md)。
- 响应解析引擎无关地汇总到 `ResponseState`：`terminal()` 以 `finish_reason in ("stop", "length")` 且协议完整判定 `completed`，否则 `failed`；计时用单调时钟，首内容与首答案分列，总截止不随新事件续期。端点未报用量时 token 字段为 `null` 且不补零，计量口径见 [架构](../../cli/docs/architecture.md) 的「计量与比较」节。
- `requested` 不自动等于 `effective`：lab 模式经 `/lab/v1/requests/ID/parameters` 逐项核对生成参数与 conditions，不匹配即 `lab_effective_parameter_mismatch`；native 模式没有该端点，有效参数记 `effective: null`、`verification: "not_observed"`，模板精确预算同样不可核验。`LlamaCppAdapter` 复用 Prism 核验并从 `/slots` 参数证据核对 `n_predict` 与 `stop`。探测后模板 token 数与 usage 不一致即 `template_token_count_mismatch`；模板后的实际输入预算不静默截断，超限阻断。
- 空闲证据分级：lab 模式以 `/lab/v1/lifecycle` 加请求释放状态判定引擎级空闲，观察前后须为同一 `server_instance_id`；Prism 系以 `/slots` 单槽 `is_processing` 判定，且 `verify_properties` 核对 build_info、槽数、`n_ctx`、模型路径与 chat 模板哈希；native 模式的 `wait_ready` 只确认客户端 HTTP 往返结束（`native_uncertain` 清零），不主张引擎内部排空。连接关闭、低 CPU 或 `/health` 成功都不替代可核验空闲证据。
- native 模式的 clean 收据 `completion_scope: "client_http"` 只覆盖已核验的 HTTP 往返；未完成该收据时 dirty 只能经绑定恢复清除（`native_dirty_requires_bound_recovery`）。native 适配器上报 `failed` 终态即阻断（`native_generation_failed:<category>`），但此前已写入的模型失败终态不被擦除。
- 凭据只在适配器构造时进入内存中的 `Authorization` 头与 `Redactor`，不落配置快照、日志或 Git；lab 请求体注入 `lab_request_id`/`lab_server_instance_id` 把响应绑定到服务实例，实例不一致即拒绝。
- 解析有界：lab 通路单请求线数据上限 2 MiB（超限 `lab_generation_size`），native 观察端点与 lab 观察端点响应同样限 2 MiB；Prism SSE 事件缓冲上限 8 MiB（`sse_event_too_large`），EOF 时未闭合事件为 `incomplete_sse_event`。
- 传输客户端固定 `trust_env=False`、`follow_redirects=False`、无默认超时；总截止由 `execution.timeout_seconds` 经 `asyncio.timeout_at` 施加，连接与协议层失败按上述分类进入终态，客户端不做隐式重试。
- 请求体由适配器按引擎契约构造并固定 `n: 1`，流式时请求 `stream_options.include_usage`；配置里的 `seed_support` 等引擎不识别的键由适配器剔除或改写（如 reasoning 模式改写为 `chat_template_kwargs`），不改写冻结配置本身。
- 失败分类直接进终态的 `error_category`：总超时为 `total_timeout`、HTTP 层错误为 `transport_error`、协议违例为具体协议原因；错误后取消请求未获确认时在类别后缀 `_cancel_unconfirmed`。事件回调本身抛错统一包装为 `event_sink_failed`，证据写入失败即中断。
- 文本三路独立过脱敏流（content、reasoning、线数据），任一路触发脱敏即置 `redacted`，评分输入随之以 `redacted_scoring_input` 记缺测而不是给出分数；用量事件的 `source`/`scope` 标注为端点口径，与计时口径分开。
- lab 空闲观察在生命周期快照前后各读一次 identity，观察期间服务实例变化即拒绝；请求与观察经 `X-Lab-Instance-ID`/`X-Lab-Request-ID` 头绑定到服务实例与请求，绑定后重读 identity 不一致即 `lab_identity_changed`。取消经 `/lab/v1/requests/ID/cancel`，未获确认为 `lab_cancel_unconfirmed`。native 模式没有取消端点，取消动作只是关闭客户端 HTTP 上下文并置 `native_uncertain`，释放仍由 `wait_ready` 独立确认。
- native 能力发现 `discover` 逐个探测固定端点集（`/lab/v1/identity`、`/v1/models`、`/health` 等）形成能力矩阵：identity 端点满足 lab 协议且生命周期可用时进入 lab 模式，否则降级 native 并记录 `missing_reason`；`engine.observation_mode: lab_required` 拒绝降级。
- 评分衔接在请求路径内完成：builtin scorer 用 `ScoringContext` 预取题目，score 事件的类目或评分器哈希与冻结身份不符即 `scorer_identity_mismatch`；出现 `unscorable` 时运行完成仍映射为退出码 3（单次原因 `unscorable_case`），评分故障不撤销已写入的执行终态、不缩小分母。
- `halt_on_failed` 适配器（lab 系）在 failed 终态时直接中断 trial（`lab_generation_failed`），把模型服务/协议失败与工具故障区分开；该中断同样保留已落盘的请求终态。
- 旧格式拒绝不归本层裁决：`run_trial` 对 `UnsupportedFormat` 原样上抛，由入口层映射，拒绝规则见 [证据、封存与离线读取](evidence-storage.md) 与 [当前格式契约](../../cli/docs/contracts/inferyard-current-format.md)。

## 验证

以下命令在仓库根的 `cli/` 目录内执行（与 [mise 任务](../../mise.toml) `test`/`lint` 同一形式）。单元测试全部使用 fixtures 与模拟服务，属软件级回归，不授予真机资格。`cli/scripts/` 下的 `verify_*` 脚本可能启动服务或发送真实请求，本子系统的常规验证不依赖它们，故不列入。

```sh
# 主机锁、dirty 恢复与状态兼容
uv run --frozen pytest tests/unit/test_lock.py tests/unit/test_host_lock_compatibility.py

# 请求生命周期、trial 编排、duration 窗口与排空、安全停止、服务 drain
uv run --frozen pytest tests/integration/test_trial_requests.py \
  tests/integration/test_trial_runner.py tests/integration/test_trial_profiles.py \
  tests/unit/test_duration_driver.py tests/unit/test_duration_send_admission.py \
  tests/unit/test_safety.py tests/unit/test_safety_trace.py tests/unit/test_service_drain.py

# 适配器解析：Prism SSE、lab 生成/观察/参数、native 观察
uv run --frozen pytest tests/unit/test_prism.py tests/unit/test_lab_generation.py \
  tests/unit/test_lab_generation_plain.py tests/unit/test_lab_observation.py \
  tests/unit/test_lab_observation_tracker.py tests/unit/test_lab_adapters.py \
  tests/unit/test_native_adapters.py

# 长度准备与固定输出
uv run --frozen pytest tests/unit/test_length_builder.py tests/unit/test_fixed_output.py \
  tests/unit/test_input_lengths.py

# 批量执行编排与容量/缓存协议（集成级，含模拟服务）
uv run --frozen pytest tests/integration/test_batch_runner.py \
  tests/integration/test_capacity_stop.py tests/integration/test_cache_protocol.py \
  tests/integration/test_batch_process_interruptions.py

# 样式与全量回归
uv run --frozen ruff check src tests scripts
uv run --frozen pytest
```

## 与既有文档的差异

- [架构](../../cli/docs/architecture.md)「请求与运行」称无法核验的有效参数为 `unknown`；实现中（`NativeObservation.native_parameters`）这些字段记为 `effective: null`、`verification: "not_observed"`、`missing_reason: "effective_parameter_unavailable"`，证据内不存在字面 `unknown` 值。本文按实现描述。

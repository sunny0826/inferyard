# 扩展协议与受控工作流

`cli/src/inferyard/extensions/` 实现与正式 v3 证据通路隔离的独立实验协议：固定上游构建的并发与工具传输、闭集并发负载、四臂 ABBA 观测开销对照，以及把对照绑定到正式 trial 的完整控制工作流。它与正式测评的边界是：扩展协议自成 `extension_packet.v1` 封存容器、自己的 journal 与离线核验入口，产物不进入正式评分或性能比较资格；正式执行与证据语义见 [运行时执行与引擎适配](runtime-execution.md) 与 [证据、封存与离线读取](evidence-storage.md)，容器布局归属见 [run-evidence-layout 契约](../../cli/docs/contracts/run-evidence-layout.md)。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| CLI 工作流入口 | `extension freeze/run/replay` 三个动作的冻结、执行与离线重放分派；`extension-check` 由 `verify` 按 `packet.json` 识别后路由到此 | `execute`、`freeze`、`live`、`read_plan` 在 [`workflow.py`](../../cli/src/inferyard/extensions/workflow.py)；命令行参数 `--spec/--plan/--packet` 在 [`extensions.py`](../../cli/src/inferyard/cli/commands/extensions.py)；`verify` 产物识别在 [`verification.py`](../../cli/src/inferyard/application/verification.py) |
| 固定传输 | 绑定 llama.cpp 固定构建的槽位并发传输、能力/模板/参数核验、取消探测、答案评分 | `ExtensionTransport` 在 [`extension_transport.py`](../../cli/src/inferyard/extensions/extension_transport.py)，继承 [`PrismAdapter`](../../cli/src/inferyard/adapters/prism.py)（`management`、`token_budget`、`verify_properties`），固定版本常量 `BUILD` 在 [`llama_cpp.py`](../../cli/src/inferyard/adapters/llama_cpp.py) |
| 闭集并发协议 | 固定并发槽位上的闭集请求组执行、逐槽排空证明与 goodput 归约 | `run_closed`、`reduce_closed`、`validate_spec` 在 [`closed_concurrency.py`](../../cli/src/inferyard/extensions/closed_concurrency.py)；规格 schema `CLOSED_CONCURRENCY` 在 [`schemas_extensions.py`](../../cli/src/inferyard/contracts/schemas_extensions.py) |
| 原生工具协议 | 白名单 mock 函数（`add`/`lookup`）驱动的有界工具调用链、SSE 工具流组装、四项检查归约 | `run_tool_case`、`score_tool_case`、`reduce_tools`、`call_mock` 在 [`native_tools.py`](../../cli/src/inferyard/extensions/native_tools.py)；`ToolStream` 在 [`native_tool_stream.py`](../../cli/src/inferyard/extensions/native_tool_stream.py)；schema `NATIVE_TOOLS`/`NATIVE_ARGS`/`NATIVE_LOOKUP` 在 `schemas_extensions.py` |
| 总观测开销对照 | off/on/on/off 四臂 ABBA 执行驱动与归约资格判定 | `run_total`、`assess_total`、`validate_spec`、`ORDER`、`COVERAGE` 在 [`total_observer_control.py`](../../cli/src/inferyard/extensions/total_observer_control.py)；v1 的观测接线 `Observer`/`execute_total` 在 [`total_control_runtime.py`](../../cli/src/inferyard/extensions/total_control_runtime.py) |
| 完整 trial 控制 | v2 把四臂对照绑定到正式 `run_trial` 通路：on 臂跑真实 trial，off 臂用内存 journal 复现同一 workload | `execute_complete`、`read_trial_plan` 在 [`complete_trial_control.py`](../../cli/src/inferyard/extensions/complete_trial_control.py)；off 臂 `BaselineJournal`/`CommonGuardSafety` 在 [`trial_control_baseline.py`](../../cli/src/inferyard/extensions/trial_control_baseline.py)；行级核验 `verify_control_rows` 在 [`trial_control_evidence.py`](../../cli/src/inferyard/extensions/trial_control_evidence.py) |
| 独立守护进程 | 独立 OS 进程内的只读安全/环境守护，为两臂提供共同采样与停止信号 | `IndependentGuard`、`POLICY`、`_capture` 在 [`independent_guard.py`](../../cli/src/inferyard/extensions/independent_guard.py)，依赖 [`SafetyGuard`](../../cli/src/inferyard/runtime/safety.py) |
| live 主机编排 | v2 live 的主机锁、dirty 拒绝、身份预检、守护进程与封存编排 | `live_complete` 在 [`complete_control_workflow.py`](../../cli/src/inferyard/extensions/complete_control_workflow.py)；v1 与其余定义的编排内联于 `workflow.live` |
| 客户端容量证据 | 闭集负载期间事件循环滞后与进程 CPU 的保守自证 | `ClientCapacity`、`qualified` 在 [`client_capacity.py`](../../cli/src/inferyard/extensions/client_capacity.py) |
| 扩展封存与离线核验 | `extension_packet.v1` 容器的 journal、归约分派、保存、渲染与重放核验 | `ExtensionJournal`、`reduce_packet`、`save_packet`、`verify_packet`、`render` 在 [`extension_evidence.py`](../../cli/src/inferyard/extensions/extension_evidence.py)，复用 [`EvidenceStore`](../../cli/src/inferyard/evidence/storage.py) 的 snapshot/append/seal 与 `verify_manifest` |

关键事实源分三层。扩展定义的当前格式集合（`closed_concurrency.v1`、`native_tools.v1`、`total_observer_control.v1/v2`、`extension_event.v1`、`trial_control_baseline.v1`）以 [当前格式契约](../../cli/docs/contracts/inferyard-current-format.md) 的支持矩阵为准；各 spec 的字段级语义以 `schemas_extensions.py` 中的 `obj` 定义与 `validate_spec` 的追加检查为准，本文不复制。归约产物的 `hardware_qualified`、`complete`、`within_tolerance` 等资格字段由上述归约函数计算，是模拟（fixture）与真机（live）证据的唯一区分点：`hardware_qualified` 仅在 `evidence_kind == "live"` 且全部条件满足时为真，模拟验证不授予真机资格。冻结输入的绑定哈希（`plan_sha256`、`config_sha256`、`bundle_sha256`、`tool_source_sha256`、v2 的 `guardian_policy_sha256`/`trial_plan_sha256`）在 `workflow.freeze` 写入，读取侧由 `read_plan`、`verify_packet` 与 `verify_control_rows` 各自重算核对；v2 的测量计划合法性（fixed 协议、题序、输入摘要）由 `read_trial_plan` 在冻结与核验两侧共同强制。

## 运行路径

以下路径对应 `extension run`（live）的主流程；命令语法见 [CLI 接口](../../cli/docs/cli-surface.md)。

1. **冻结**（`extension freeze`，离线）：`workflow.freeze` 计算源码身份（`tool_source_hash`）与 config/bundle 摘要，为 total_observer_control 定义补写四个绑定哈希，按 `definition` 经 `VALIDATORS` 分派到对应 `validate_spec`，校验 case 属于已审核题包（`native_tools.v1` 例外，且只允许 `capability_diagnostic` track），写入 `plan.json`（`extension_plan.v1`，含 spec、三个输入哈希与 `plan_sha256`）；输出目录必须事先不存在（`exist_ok=False`），写入走原子字节替换。
2. **计划核验**（live 起点）：`workflow.read_plan` 重算 `plan_sha256` 及全部冻结输入哈希，任一不匹配即拒绝；随后进入主机锁与预检。
3. **主机锁与身份预检**：获取 [`HostLock`](../../cli/src/inferyard/runtime/lock.py)，dirty 状态要求走既有恢复流程；`static_preflight` 核验服务身份。守护进程启动后，v1 与其余定义以 `lock.dirty` 标记扩展运行身份（kind 为 `extension`），封存完成才 `lock.clean`；v2 复用正式 trial 通路自己的 dirty 语义。v2 额外要求冻结输入经 `Redactor` 脱敏后无变化（凭据不得进入冻结输入）。
4. **传输核验**：`ExtensionTransport.verify` 要求 adapter 为 `llama_cpp_b11146_v1` 且 `release` 等于固定 `BUILD`、`slots_debug` 开启、`cache_policy == "disabled"`、启动参数不含服务端工具开关（`--agent`/`--tools`/`--tools-runtime`/`--mcp-server`）；再核验 properties、聊天模板与槽位身份/空闲，任一槽位忙即拒绝。工具协议另验 `supports_tool_calls`。
5. **守护进程启动**：`IndependentGuard.start` 以独立进程（macOS 用 `spawn`、其余 `fork`）开始逐样本安全/环境检查，每个样本同步 flush+fsync 到 `guardian.jsonl`；`watch_guard` 任务在守护进程停止时取消主流程。
6. **取消能力探测**：`probe_cancellation` 占用槽位 0 后断开连接，必须观察到 busy 且随后证明 idle，否则预检失败。
7. **协议执行**（按 `definition` 分派）：
   - 公共推理路径：`ExtensionTransport.infer` 先以 `/apply-template` + `/tokenize` 做输入 token 预算，完成时比对 `usage.prompt_tokens`；`finish_reason` 非 `stop` 判证据错误；答案按 bundle 的 `answer_policy` 走确定性 `score_case` 评分，回复同时经 redactor 脱敏后才入证据。三个执行定义共用这条路径，槽位由适配器独占。
   - `closed_concurrency.v1`：`run_closed` 以 `concurrency` 个 worker 消费冻结题序，每请求记录开始行并同步落盘（`request_started`/`request_finished` 为同步事件），逐槽等待 `wait_idle` 排空并记 `drain_end_ns`；同时运行 `ResourceSampler` 与 `ClientCapacity`。任一非 completed 终态、排空失败或取消都置全局停止位。
   - `native_tools.v1`：`run_tool_case` 逐 case 执行有界工具链（`max_calls`/`max_rounds` 上限由 schema 与 `validate_spec` 强制），每次回复与工具执行记录事件，最终 `score_tool_case` 计算 selection/arguments/execution/final_answer 四项检查。
   - `total_observer_control.v1`：`execute_total` 经 `run_total` 跑四臂，on 臂用 `total_control_runtime.Observer` 接 ExtensionJournal、`ResourceSampler`、`SafetyGuard`/`SafetyTrace`，off 臂完全不落盘；每臂 close 必须 flush+seal（Observer 细节见本节下文「v1 观测接线」）。
   - `total_observer_control.v2`：`live_complete` 转交 `complete_control_workflow.live_complete`，四臂经 `execute_complete` 各跑一次正式 `run_trial`（on 臂真实封存 trial，off 臂 `BaselineJournal` 在测量臂关闭后才把缓冲写入父容器）；臂间以 `control_checkpoint` 事件持久化臂状态。
8. **收尾判定**：全部槽位 `wait_idle` 确认排空、守护进程退出码为 0 且未置 unsafe，否则标记 stopped；`reduce_packet` 归约（闭集负载附带 `client_capacity` 证明，v1 附带 guard 证据），不一致即失败。任一停止位或排空未知都抛 `extension_safety_stop_or_drain_incomplete`，走异常收尾。
9. **封存**：snapshot `run.json`/`plan.json`/`config.frozen.json`/`bundle.json`/`identity.json`（v2 加 `measurement-plan.json`）、`packet.json`、确定性重算的 `summary.json` 与自包含 `report.html`、`child-evidence.json`（绑定每个 observer 子运行的 manifest 哈希），最后 `seal()` 并 `lock.clean()`。
10. **异常收尾**：未封存时复用正式通路的 dirty 语义——等待排空确认后写 `stop.json`（`completed: false`、`automatic_retries: 0`、保留原始事件），确认排空才清 dirty；守护进程残留会被终止。

### v1 观测接线（total_control_runtime.Observer）

`Observer` 是 v1 on 臂的全部观测路径，off 臂不实例化任何存储：

- `open()`：snapshot `config.frozen.json`、`bundle.json` 与 `environment.start.json`，启动 `ResourceSampler` 与按策略间隔运行的 `safety_loop` 两个异步任务；`mode == "off"` 时直接返回，不建 store。
- `before(row)`/`after(row)`：请求边界切换采样 phase、记录 `request_start`/`request_end` 边界，并在前后各执行一次安全 trace 检查；采样或守护任务若已异常结束，在下一请求边界经 `task.result()` 重抛进入该臂的异常路径。
- `close()`：停止采样、取消两个任务并把非取消异常重新抛出，snapshot `environment.end.json`，`flush(sync=True)` 后 `seal()`；无论成败都在 `finally` 中 `store.close()`。

离线路径：`extension replay` 只接受 `evidence_kind == "fixture"` 的 packet，`save_packet` 经同一 `reduce_packet` 归约后封存（origin 记为 `offline_fixture`）；`verify`（或 `extension-check`）由 `verify_packet` 重算 manifest、核对 live 绑定的 plan/identity/子证据哈希、重放归约并与已存 summary/report 逐字节比对，其 `verified_trials` 可选参数把已核验 trial 数据回填给调用方以避免重复读取。`workflow.guarded` 把每个协议任务与停止位等待竞速：停止位胜出即取消任务并汇合，保证已开始的适配器任务不泄漏。`reduce_packet` 还分派 `conditional_events.v1` 到 [`conditional_events.reduce_import`](../../cli/src/inferyard/evidence/conditional_events.py)，支持外部条件事件的离线导入。

命令分派上，`extension freeze/run/replay` 的双词形式在 [`dispatch.py`](../../cli/src/inferyard/application/dispatch.py) 映射为 `extension-freeze/run/replay` 后进入本包；`--help`、schema 等元数据查询不加载本包。`workflow.execute` 按命令返回状态：`extension-check` 为 `verified`、`extension-replay` 为 `software_verified`、live 执行为 `executed`。

## 协议定义一览

`VALIDATORS` 与 `reduce_packet` 共同定义了支持的扩展定义集合（字段语义见 `schemas_extensions.py`）：

- `closed_concurrency.v1`：冻结题序在固定并发槽位上的闭集请求组。`run_closed` 执行、`reduce_closed` 归约，产出五终态计数、观察峰值并发、窗口秒数、N01 completed/s、N02 goodput/s、排空与完整性标志；`N03_token_ITL` 固定缺测。
- `native_tools.v1`：模型经原生 tool_calls 驱动的白名单 mock 函数链。`run_tool_case` 执行、`reduce_tools` 重算四项检查并给出通过率；`ToolStream` 独立组装 SSE 工具增量，与正式通路的 answer-only 解析器不共用代码。
- `total_observer_control.v1`：合成扩展日志的四臂观测开销对照。on 臂由 `total_control_runtime.Observer` 完整接 ExtensionJournal/采样/安全轨迹，off 臂零落盘；`assess_total` 归约。
- `total_observer_control.v2`：正式 `run_trial` 通路上的同构四臂对照，on 臂是真实封存 trial、off 臂是测量后落盘的 baseline；归约仍为 `assess_total`，另加 `verify_control_rows` 行级核验。
- `conditional_events.v1`：外部条件事件的离线导入定义，经 `reduce_import` 归约，不属于扩展执行协议，live 不生产。

各定义的当前格式地位与旧版本拒绝策略见 [当前格式契约](../../cli/docs/contracts/inferyard-current-format.md) 的「扩展及开销」行。

### 工具链细节（native_tools）

`native_tools` 的执行与判定要点：

- 可调用函数白名单只有 `add` 与 `lookup` 两个进程内 mock，catalogue 是固定的 `{"alpha": "北京", "beta": "上海"}` fixture；任何其他函数名直接拒绝（`tool_not_allowlisted`），不触网络与外部进程。
- 回复语义由 `parse_reply` 强制：`finish_reason == "tool_calls"` 必须带非空 `tool_calls`，`finish_reason == "stop"` 必须无 tool_calls 且有字符串 content；usage 的 token 计数必须是非负整数。
- 循环把 assistant message 与每个 tool result 追加回 messages 再发起下一轮；tool_calls 增量经 `ToolStream` 按 index（0-7）拼接 id/name/arguments 片段，缺 `[DONE]`、finish 后仍有增量或 usage 重复都判证据错误。
- 超时保留已执行的调用记录与失败终态；预算超限、参数非合法 JSON、调用 id 重复或缺失都终止该 case 并记 `failed`，归约时 `reduce_tools` 对每行重算 `score_tool_case` 与已存 checks 比对。

## 事件、容器与封存布局

`ExtensionJournal`（[`extension_evidence.py`](../../cli/src/inferyard/extensions/extension_evidence.py)）是扩展容器的事实源，行为要点：

- 每条事件写 `events.jsonl`，`definition` 固定 `extension_event.v1`，字段含 `seq`、`clock_id`、`phase`、`event_type`、`request_id`、`data`、`monotonic_ns`、`utc`，写入前过内联 schema 校验。`request_started`、`request_finished`、`tool_terminal`、`control_checkpoint` 四类事件以 `sync=True` 追加（同步 flush+fsync），其余事件可延迟落盘；`phase` 在 live 编排中固定为 `formal`。live 编排把 `store.event` 挂到适配器的 `record` 回调，capability、取消探测、逐请求有效参数与工具参数事件由此进入同一日志。
- `observation` 对 `external-cpu.jsonl`、`request-environment.jsonl`、`safety-checks.jsonl` 三个名字惰性建文件并追加，其余名字委托 `EvidenceStore.observation`；`sample` 按当前 `sample` 文档校验后写 `memory.jsonl`，并强制补写 `schema_version`、`run_id`、`experiment_id="extension"`、`trial_id=run_id` 与 `clock_id`。
- live 成功路径的容器文件：`run.json`（`extension_packet.v1` 标识与 origin）、`plan.json`、`config.frozen.json`、`bundle.json`、`identity.json`、v2 加 `measurement-plan.json`；运行期日志 `events.jsonl`、`memory.jsonl`、`guardian.jsonl`、观测文件与 v1 的 `observer-arms/` 子目录；收尾时 `packet.json`、`summary.json`、`report.html`、`child-evidence.json`；最后 `manifest.json` 封存（哈希身份与来源，不哈希自身，语义与正式通路一致）。异常路径多一个 `stop.json`。
- `child-evidence.json` 逐条记录 v1 observer 子运行目录及其 `manifest.json` 的哈希，`verify_packet` 对每个子目录重算 manifest 并核验封存，子证据被改动即拒绝（`extension_child_observer_evidence_changed`）。
- off 臂的 `BaselineJournal`（[`trial_control_baseline.py`](../../cli/src/inferyard/extensions/trial_control_baseline.py)）实现 `RunRecords` 的对应领域方法但全程只写内存：snapshot/event/observation 都经 redactor 清洗后缓存，`seal()` 仅置内存标志，`close()` 不动作；`data()` 从 formal 阶段的 `request_started`/`request_finished` 事件对重建请求行。臂关闭后 `run_total` 调 `deferred_evidence()`，由父容器把缓冲保存为 `baseline-<n>.json`（`definition="trial_control_baseline.v1"`，`preservation` 字段标注 `memory_during_arm_file_after_measured_close`）。`CommonGuardSafety.check` 是空操作，其 metadata 标注 scope 为独立绑定的共同守护，即 off 臂把 in-process 观测换成守护进程证据、其余请求生命周期保持原样。
- 扩展容器不进入通用 run 布局：`evidence_layout_version=2` 与核心 3 的快照规则不套用，`BaselineJournal` 不加 run 容器文件，`ExtensionJournal` 也不增加 run 容器成员，归属见 [run-evidence-layout 契约](../../cli/docs/contracts/run-evidence-layout.md)。snapshot 仍是原子发布；凭据 redactor 在 journal 构造时传入，写入侧统一脱敏。

## 状态转换

### 闭集负载的请求行

`run_closed` 中每请求的行经历：初始 `not_executed`（冻结题序预置）→ 派发时先以占位 `invalid` 行同步落盘 `request_started`（此时已有 `request_id`、`slot_id`、`send_ns`、`dispatch_delay_ns`）→ 执行结果为 `completed`，或 `failed`（超时/适配器异常）/`cancelled`（停止位或调用者取消）→ 记 `end_ns` 后等待排空得 `idle_confirmed` 与 `drain_end_ns`，最后以 `request_finished` 同步落盘定稿。排空未证实置全局停止位，后续题保持 `not_executed`；总墙钟超时同样只保留已落盘行。归约侧 `not_executed` 行不得带请求身份，`invalid` 行存在即 `complete: false`。

### 四臂与守护进程

`run_total` 每臂：建臂（`finalized: False`、记 `begin_ns` 与协议/身份哈希）→ `observer.open()` → workload → `observer.close()` 记 `after_close_ns` 并挂 `deferred_evidence` → `finalized: True`。守护进程停止即中断后续臂，已建未完成的臂原样保留——臂状态经 `control_checkpoint` 事件持续落盘，异常路径另有 `stop.json` 汇总残留状态——驱动本身不重试。守护进程样本单调时钟必须严格递增且首尾括住各臂窗口，否则对应缺陷记 `reasons`。

## 约束与失败行为

### 独立守护进程（independent guard）

`IndependentGuard` 在独立 OS 进程运行 `_capture` 循环，按 `POLICY`（默认 0.5 秒间隔、90°C 温度上限、CPU 20% 上限，macOS 用 `spawn`、其余 `fork`）逐样本调用 `SafetyGuard.check` 并额外检查输出根磁盘余量；每样本经 `open_nofollow` 独占创建写入 `guardian.jsonl` 并 flush+fsync，不安全即置 `unsafe` 位。收到 shutdown 后再取一个终止样本，括住臂的 flush/seal/close 完整生命周期。`clock_id` 为 boot id 加 `:CLOCK_MONOTONIC`；`start()` 五秒内未就绪即失败，`close()` 不干净退出会终止进程并抛证据错误，`evidence()` 读回样本，存在截断/缺测或非零退出码即拒绝。臂与 guard 的时钟绑定、进程身份（guard PID 不得等于客户端 PID）、策略哈希认证由 `assess_total` 逐项核验。

### 四臂对照的硬错误与缺陷

`assess_total` 区分两类失败：臂数不是 4、顺序不是 off/on/on/off、臂身份哈希或 `protocol_sha256` 不符、臂窗口非法、请求时长之和超出臂窗口、相邻臂时间重叠是硬错误（`EvidenceError`，重算即拒）；guard 样本少于 2 个、采样间隔超过 `max_guard_gap_ns`、样本不安全、环境身份缺失或中途变化（live 还要求 macOS 电源策略或 Linux governor/EPP 字段完整、策略哈希与冻结值一致、进程启动 ticks 为正）、臂覆盖清单缺项、输出在臂间不一致、冻结墙钟预算超限等记为 `reasons` 缺陷，使 `valid_control: false`。v2 额外要求 on 臂 `measurement_path == "run_trial.v1"` 且 `trial_plan_sha256` 绑定、覆盖清单含 `offline_reconstruction` 项。全部无缺陷后才计算 (off1,on2)、(off4,on3) 两对的请求时长中位变化比与全生命周期变化比，`within_tolerance` 要求两对四项全部落在 `tolerance_ratio` 内；`hardware_qualified` 还要 `evidence_kind == "live"`。`run_total` 不替调用者重试：守护进程停止即中断后续臂，出错的臂保留未完成状态交由调用者处置，总墙钟受 `max_wall_seconds` 约束。

### v2 行级核验（verify_control_rows）

`verify_packet` 对 v2 packet 调 `verify_control_rows`（[`trial_control_evidence.py`](../../cli/src/inferyard/extensions/trial_control_evidence.py)）：保存的 `measurement-plan.json` 必须是合法 plan 且 `plan_sha256` 等于 `spec.trial_plan_sha256`；`guardian.jsonl` 原始样本必须与 packet 内 guard 证据完全一致；每个 on 臂的 trial 目录按 manifest 哈希绑定，其 plan 内容与源码身份重算一致；每个 off 臂的 baseline 文件定义、`plan.json` 快照与 `clock_id` 绑定一致；两臂冻结输入（config/bundle）哈希等于 spec；每条请求的 `t_send_ns`/`t_terminal_ns` 必须落在所属臂窗口内；对 trial 行跑 `request_projection`、对 baseline 行按事件重建，结果必须与 packet 行一致（`total_control_arm_requests_do_not_replay`）；`child-evidence.json` 清单与期望的子证据集合完全一致。`read_trial_plan` 在冻结与 live 两侧校验 trial 计划：workload 协议必须为 `fixed`、无 `output_mode`/`cache_protocol` 覆盖、`case_order` 与 spec 一致、config/bundle 摘要一致。

### 其余约束

- **资格边界**：`replay` 拒绝 live packet（`offline_import_cannot_grant_live_qualification`）；反向地，fixture 证据无论归约结果如何都不会产出 `hardware_qualified: true`。闭集负载的 `hardware_qualified` 还要求完整执行、客户端调度与容量双合格；对照协议的还要求四对臂全部在容差内且 guard 证据无缺陷。
- **冻结绑定**：计划、配置、题包、源码身份、守护策略与 trial 计划的任何哈希不匹配都拒绝执行或核验；live packet 的 `identity.json` 必须 `verification == "verified"`，子证据 manifest 哈希必须一致。已删除的命令别名与旧参数不接受。
- **槽位与并发**：`concurrency` 不得超过冻结的 `server_slots`；槽位身份（id 连续、`is_processing` 布尔）每次读取都重验；取消后必须证明 idle，否则保留 dirty 并停止发送。闭集负载一次执行失败即置停止位，不重试、不改样本量；worker 共享冻结题序队列，每请求记 `dispatch_delay_ns` 调度滞后，总墙钟受 `max_wall_seconds` 约束，超时置停止位并保留已落盘行。
- **客户端容量**：`qualified` 要求证明区间完整包含资源窗口、事件循环样本至少 2 个、进程 CPU 占比不超过 `max_client_cpu_fraction`、最大事件循环滞后不超过 `max_dispatch_delay_ns`，时钟 id 与进程身份齐全；闭集归约的窗口从首个请求发起到最后排空结束计算，goodput 分子要求 completed、时延在 `latency_limit_ms` 内且质量合格（`require_quality` 时），`N03_token_ITL` 恒为 `null`（无已核验逐 token 事件）。
- **有界预算**：工具链调用次数、轮数、case 数与参数 JSON 大小（4096 字节）均有硬上限；SSE 组装字节预算 1 MiB；非流式响应体超过 1 MiB 判证据错误。所有上限在 schema 或 `validate_spec` 处强制。工具 mock 是进程内确定性函数，`limitations` 标注 `deterministic_mock_functions_only`；`reviewed_formal` track 需要 `content_review_sha256` 等于 cases 摘要，CLI 只执行 `capability_diagnostic`。
- **有效参数核验**：每个请求后核验该槽位可见的 `seed`/`temperature`/`top_k`/`top_p`/`min_p`/`penalty` 与冻结生成参数一致（1e-6 容差），`n_predict`、`stop` 精确相等；工具请求还核验模板 token 计数与 context 预算。答案经 redactor 脱敏，发现凭据即不评分（quality 记 `null`）。
- **平台限制**：live 在 Windows 直接拒绝（`windows_phase2_live_not_supported`）；守护进程证据不完整或其进程非零退出即整体失败。
- **报告诚实性**：`report.html` 固定标注「Fixture代表软件验证；真机资格按独立证据标记」；对照归约的 `limitations` 明示共享守护成本未估计、仅声明 case 与单机、非总体置信、方法墙钟占比不是因果减速；闭集归约明示非开放到达负载。
- **终态守恒**：闭集负载沿用五个终态（completed/failed/cancelled/invalid/not_executed），`not_executed` 不得携带请求身份或时间戳；工具协议终态不含 `invalid`。答案被判错不视为协议失败，质量不合格只影响 goodput 分子；`reduce_tools` 重算四项检查与 packet 不一致即拒绝（`tool_checks_do_not_replay`）。

归约失败统一抛 `EvidenceError`（退出码 4）；输入/预检阻断抛 `PreflightError` 或 `ContractError`（退出码 2）；协议执行完成但安全停止或排空未证实也按证据错误处理。扩展容器不套用 v2 run 布局规则，独立封存协议保持不变，见 [ADR 048](../../cli/docs/decisions/048-run-evidence-containers.md)。

## 验证

以下命令在仓库根执行；live 通路需要操作者已启动并绑定真实服务，模拟验证使用 fixture：

```sh
# 扩展协议全部单元测试（含 fixture 级 freeze/replay/live 编排演练）
cd cli && mise exec -- uv run --frozen pytest -q \
  tests/unit/test_closed_concurrency.py \
  tests/unit/test_extension_evidence.py \
  tests/unit/test_extension_runtime.py \
  tests/unit/test_extension_transport.py \
  tests/unit/test_native_tools.py \
  tests/unit/test_total_observer_control.py

# lint
cd cli && mise exec -- uv run --frozen ruff check src tests scripts

# 扩展 wire schema 的生成物核对（closed_concurrency / native_tools / total_observer_control）
cd cli && mise exec -- uv run --frozen python scripts/export_schemas.py --check

# 全量测试（影响面较大时；仓库根任务 mise run test 的等价命令）
cd cli && mise exec -- uv run --frozen pytest
```

测试与行为的对应：`test_closed_concurrency.py` 演练调度绑定槽位、goodput/窗口归约、坏证据与缺排空拒绝资格；`test_extension_evidence.py` 演练封存重放、篡改拒绝、离线 freeze 绑定与 fixture 不继承 live 资格；`test_extension_runtime.py` 演练取消汇合、独立守护进程边界与四臂 seal；`test_extension_transport.py` 演练预检 fail-closed、有效参数与原生工具流；`test_native_tools.py` 演练四项检查、调用预算与 SSE 组装拒绝；`test_total_observer_control.py` 演练软件验证与真机资格区分、guard 身份环境绑定、臂重叠与部分臂保留。

端到端手工路径（需真实 llama.cpp 固定构建服务，不属于默认验证）：`inferyard extension freeze --spec SPEC.json --config CONFIG.toml --out EXT_PLAN`、`inferyard extension run --plan EXT_PLAN/plan.json --config CONFIG.toml --out EXT_RUNS`，离线核验用 `inferyard verify --path EXT_RUNS/<evidence_dir>`（重放归约并比对 summary/report），语法见 [使用指南](../../cli/docs/usage.md)。

核对方式：pytest 测试文件均存在于 `cli/tests/unit/`；ruff 与 pytest 由 `cli/pyproject.toml` 的依赖组提供，`--frozen` 与 `uv` 版本由 [mise.toml](../../mise.toml) 固定；`export_schemas.py --check` 覆盖 `schemas.py` 惰性引入的 `schemas_extensions.py` 三个 wire schema；仓库根的 `mise run test`/`lint`/`schemas` 任务分别是上述三条命令的封装。macOS 上 `test_extension_runtime.py` 有 2 条 Linux fork 守护进程用例按平台跳过，属预期；本次核对该测试子集为 53 passed、2 skipped，ruff 与 schema 核对均通过。live 通路（步骤 3-9 的真实服务执行）在本机未经真机演练，真机资格以独立证据标记为准。

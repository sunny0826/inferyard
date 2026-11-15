# 运行路径

本页是[扩展协议与受控工作流](../extensions-protocol.md) 的子文档，收录「运行路径」与「状态转换」的正文；内容不变，入口保留原节标题与锚点。

以下路径对应 `extension run`（live）的主流程；命令语法见 [CLI 接口](../../../cli/docs/cli-surface.md)。

1. **冻结**（`extension freeze`，离线）：`workflow.freeze` 计算源码身份（`tool_source_hash`）与 config/bundle 摘要，为 total_observer_control 定义补写四个绑定哈希，按 `definition` 经 `VALIDATORS` 分派到对应 `validate_spec`，校验 case 属于已审核题包（`native_tools.v1` 例外，且只允许 `capability_diagnostic` track），写入 `plan.json`（`extension_plan.v1`，含 spec、三个输入哈希与 `plan_sha256`）；输出目录必须事先不存在（`exist_ok=False`），写入走原子字节替换。
2. **计划核验**（live 起点）：`workflow.read_plan` 重算 `plan_sha256` 及全部冻结输入哈希，任一不匹配即拒绝；随后进入主机锁与预检。
3. **主机锁与身份预检**：获取 [`HostLock`](../../../cli/src/inferyard/runtime/lock.py)，dirty 状态要求走既有恢复流程；`static_preflight` 核验服务身份。守护进程启动后，v1 与其余定义以 `lock.dirty` 标记扩展运行身份（kind 为 `extension`），封存完成才 `lock.clean`；v2 复用正式 trial 通路自己的 dirty 语义。v2 额外要求冻结输入经 `Redactor` 脱敏后无变化（凭据不得进入冻结输入）。
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
9. **封存**：snapshot `run.json`/`plan.json`/`config.frozen.json`/`bundle.json`/`identity.json`（v2 加 `measurement-plan.json`）、`packet.json`、确定性重算的 `summary.json`、`child-evidence.json`（绑定每个 observer 子运行的 manifest 哈希），最后 `seal()` 并 `lock.clean()`。
10. **异常收尾**：未封存时复用正式通路的 dirty 语义——等待排空确认后写 `stop.json`（`completed: false`、`automatic_retries: 0`、保留原始事件），确认排空才清 dirty；守护进程残留会被终止。

## v1 观测接线（total_control_runtime.Observer）

`Observer` 是 v1 on 臂的全部观测路径，off 臂不实例化任何存储：

- `open()`：snapshot `config.frozen.json`、`bundle.json` 与 `environment.start.json`，启动 `ResourceSampler` 与按策略间隔运行的 `safety_loop` 两个异步任务；`mode == "off"` 时直接返回，不建 store。
- `before(row)`/`after(row)`：请求边界切换采样 phase、记录 `request_start`/`request_end` 边界，并在前后各执行一次安全 trace 检查；采样或守护任务若已异常结束，在下一请求边界经 `task.result()` 重抛进入该臂的异常路径。
- `close()`：停止采样、取消两个任务并把非取消异常重新抛出，snapshot `environment.end.json`，`flush(sync=True)` 后 `seal()`；无论成败都在 `finally` 中 `store.close()`。

离线路径：`extension replay` 只接受 `evidence_kind == "fixture"` 的 packet，`save_packet` 经同一 `reduce_packet` 归约后封存（origin 记为 `offline_fixture`）；`verify`（或 `extension-check`）由 `verify_packet` 重算 manifest、核对 live 绑定的 plan/identity/子证据哈希、重放归约并与已存 `summary.json` 比对，其 `verified_trials` 可选参数把已核验 trial 数据回填给调用方以避免重复读取。`workflow.guarded` 把每个协议任务与停止位等待竞速：停止位胜出即取消任务并汇合，保证已开始的适配器任务不泄漏。`reduce_packet` 还分派 `conditional_events.v1` 到 [`conditional_events.reduce_import`](../../../cli/src/inferyard/evidence/conditional_events.py)，支持外部条件事件的离线导入。

命令分派上，`extension freeze/run/replay` 的双词形式在 [`dispatch.py`](../../../cli/src/inferyard/application/dispatch.py) 映射为 `extension-freeze/run/replay` 后交给 [`application/extensions.py`](../../../cli/src/inferyard/application/extensions.py) 的 `execute`，由它调用本包的 `freeze`/`live`/`read_plan`/`save_packet`；`--help`、schema 等元数据查询不加载本包。命令状态由 `application/extensions.py` 的 `execute` 确定并返回：`extension-check` 为 `verified`、`extension-replay` 为 `software_verified`、live 执行为 `executed`。扩展包本身不返回命令状态，`application/` 是命令结果的唯一构造点。

## 状态转换

### 闭集负载的请求行

`run_closed` 中每请求的行经历：初始 `not_executed`（冻结题序预置）→ 派发时先以占位 `invalid` 行同步落盘 `request_started`（此时已有 `request_id`、`slot_id`、`send_ns`、`dispatch_delay_ns`）→ 执行结果为 `completed`，或 `failed`（超时/适配器异常）/`cancelled`（停止位或调用者取消）→ 记 `end_ns` 后等待排空得 `idle_confirmed` 与 `drain_end_ns`，最后以 `request_finished` 同步落盘定稿。排空未证实置全局停止位，后续题保持 `not_executed`；总墙钟超时同样只保留已落盘行。归约侧 `not_executed` 行不得带请求身份，`invalid` 行存在即 `complete: false`。

### 四臂与守护进程

`run_total` 每臂：建臂（`finalized: False`、记 `begin_ns` 与协议/身份哈希）→ `observer.open()` → workload → `observer.close()` 记 `after_close_ns` 并挂 `deferred_evidence` → `finalized: True`。守护进程停止即中断后续臂，已建未完成的臂原样保留——臂状态经 `control_checkpoint` 事件持续落盘，异常路径另有 `stop.json` 汇总残留状态——驱动本身不重试。守护进程样本单调时钟必须严格递增且首尾括住各臂窗口，否则对应缺陷记 `reasons`。

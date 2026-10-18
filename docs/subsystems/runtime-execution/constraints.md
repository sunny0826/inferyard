# 约束与失败行为

本页是[运行时执行与引擎适配](../runtime-execution.md) 的子文档，收录「约束与失败行为」「主机锁与 dirty 恢复」「dirty 状态转换」「请求准入与执行」「适配器边界」的正文；内容不变，入口保留原节标题与锚点。

## 主机锁与 dirty 恢复

- 锁是非阻塞独占的（Linux `flock`，Windows `msvcrt.locking`），争用即 `host_lock_unavailable`；锁与状态文件要求安全文件身份，且每次写入前后重验 dev/inode 与父目录身份，漂移即 `host_state_identity_changed`。锁文件永不删除，见 [用法](../../../cli/docs/usage.md#取消与-dirty-恢复)。锁路径按平台选择：Linux 为 `/var/tmp/inferyard-host.{lock,state.json}`，Windows 为 Common AppData 下同名文件。
- 状态文件读写有硬边界：`open_nofollow` 打开，POSIX 上要求属主正确且权限 `0600`、无 reparse、硬链接数为 1；读取限 1 MiB 且要求读前读后文件身份与大小时间戳一致（`host_state_changed_during_read`）；发布走证据包的同步原子写（`atomic_bytes`）。字段校验在 `validate_state`：dirty 为真或带脏字段时，`dirty_token`/`run_id`/`kind`/`endpoint`/`request_id` 必须是非空字符串，`server_pid`/`process_start_ticks` 必须是正整数。
- `HostLock.write` 要求持锁（`host_lock_not_held`），写入前重验全部已锁文件的身份；恢复核验通过也只返回恢复记录，清 dirty 仍由调用方在新服务空闲确认后执行。
- dirty 语义：请求发送前必须置 dirty；只有空闲证据确认后才允许 clean。初始空闲、请求前空闲、取消/超时后的排空空闲任一环节无法确认，都保留 dirty 并给出对应原因（`initial_idle_unknown`、`pre_request_idle_unknown` 或保留请求级脏记录）。
- 人工恢复绑定当前 `dirty_token` 与非空说明；程序仍核验旧进程消失（启动 ticks 比对）、新进程身份不同，且恢复后仍须通过新服务的身份预检与空闲观察。批量路径中恢复 token 一次性，首个 trial 后清除。单次路径在取消发生时若初始空闲仍未确认，会补写带 `kind` 的 `initial_idle_unknown` 脏记录，证据写入失败再把结果降为退出码 4。
- 服务复用受 `reuse_allowed` 约束：任一侧 `require_fresh_process`、model/engine/generation 或关键 conditions/端点变化都禁止同进程复用；非串行续测复用还要求上次运行的 drain 证据（`service_reuse_drain_evidence_missing`）。同进程复用遇上 dirty 状态必须走绑定恢复（`dirty_service_requires_bound_recovery`）。

## dirty 状态转换

主机状态机只有 `dirty` 一个布尔位，转换如下（写入方均为 `HostLock` 的方法）：

| 转换 | 触发 | 证据条件 |
| --- | --- | --- |
| clean → dirty | 每次请求发送前（`dirty(run_id, endpoint, request_id, kind=…)`，request_id 为该请求 key） | 请求身份已落盘（`prepare_request` 先于 dirty） |
| clean → dirty | 初始空闲观察失败（`initial_idle_unknown`）或请求前空闲观察失败（`pre_request_idle_unknown`） | 空闲观察超时未确认 |
| dirty → clean | 空闲确认（`clean_observed` → `lock.clean`） | lab/Prism：引擎级空闲观察通过；native：持有 `client_http` 完成收据 |
| dirty → clean | 绑定恢复（`verify_manual_recovery` + `completion_scope: "process_replacement"`） | token 与说明匹配、旧进程消失、新进程身份不同且通过身份预检与空闲观察 |
| dirty 保持 | 取消/超时后空闲无法确认、native 无收据、证据写入失败 | 下一次运行必须携带恢复声明或复用同一服务身份 |

## 请求准入与执行

- 请求身份（`prepare_request` 落盘 + dirty）必须先于发送；必要证据写入失败即停发并保留 dirty，不产生无证据结果。duration 准入下 `before_send` 在发送点重查准入截止，逾期返回 `None` 且不创建持久尝试；准入截止前请求被跳过属证据错误（`duration_request_skipped_before_deadline`）。
- 没有隐式重试：`request_started` 固定 `attempt: 1`；正式请求按冻结索引顺序逐个执行，顺序或 prompt 不符即证据错误。五个终态与守恒等式的语义见 [架构](../../../cli/docs/architecture.md) 与 [运行证据布局契约](../../../cli/docs/contracts/run-evidence-layout.md)。
- 取消（`CancelledError`，含 wall 预算到期与安全停止触发）后取消生成任务；已发送的请求以最后一个已知状态合成 `cancelled` 终态落盘，未发送的只留脏记录。安全停止保留原因与计数、不当作模型失败，映射为退出码 3，见 [CLI 与应用分发](../cli-dispatch.md)。
- 采样任务与请求并发竞争（`asyncio.wait` FIRST_COMPLETED）：采样器先结束即取消生成并按证据错误处理；请求进行中采样器停止为 `sampler_stopped_early`。工具/IO 失败不虚构模型终态，持久化的尝试保留为中断状态（终态 `invalid`，归属见 [架构](../../../cli/docs/architecture.md) 的状态表）。
- wall 预算由 `TrialDeadline` 以单调时钟记账：批量 trial 的允许时长取 trial 预算与批次剩余 wall 的较小者，非正即 `experiment_wall_budget_exhausted`；到期取消当前任务并给请求标记 `single_wall_budget_exhausted`（单次）或 `trial_wall_budget_exhausted`（批量）原因；操作者信号取消映射为退出码 130，二者都不缩小质量分母。批量路径还把剩余预算（`remaining_budget`）随结果返回。
- 批量 resume 只续跑最近一次未完成运行的缺口（`resume_selection` 与 not_executed 身份，事实源在证据账本），不重跑已完成请求；恢复源必须是批次 runs 目录下的最新中断运行（`resume_requires_latest_unfinished_run`）。
- 请求边界事件（`arrival_capture`、`block_arrived` 与采样器的 `boundary("request_start"/"request_end"/"idle")` 回调）把线数据到达与请求归属传给采集器，跨边界样本的归因规则见 [平台采集与设备身份](../platform-sampling.md)。
- 预检类失败按阶段定级：正式执行已开始后的 `PreflightError`/`ContractError` 归为退出码 3（运行不完整），之前归为 2（预检阻断）；证据与内部故障为 4，见 [CLI 与应用分发](../cli-dispatch.md) 的退出码表。
- Windows 上非 `WINDOWS_BATCH_ADAPTERS` 白名单内的适配器在批量实时路径阻断（`windows_phase2_live_not_supported`）；白名单当前为 `kvmem`、`ninfer`、`prism_llama_server_v1`。lab 适配器在非 Windows 平台阻断（`lab_adapter_requires_windows`）。模拟/软件验证不授予真机资格，见 [数据契约](../../../cli/docs/data-contract.md#身份与测量资格分派)。
- `prepare-length` 是操作者绑定的辅助通路：复用锁、身份预检与空闲观察，但只做模板计数拟合，不发送生成请求；服务 dirty 时阻断（`length_preparation_requires_clean_service`），spec 触到脱敏内容即拒绝。产物自标 `preparation_not_benchmark`，不进入正式测评。
- 环境准入只影响准入、不过滤观察：`run_preflight` 传入的 `environment_admission` 策略把差异分为 `differences`（全部记录）与 `blockers`（阻断项），诊断运行不阻断；环境事实源在 [平台采集与设备身份](../platform-sampling.md)。
- 批量路径把 `store.cache_protocol` 交给适配器，请求体按冻结序号注入 `cache_prompt`/`n_cache_reuse`（`cache_execution.request_value`）；缓存语义与资格限制见 [数据契约](../../../cli/docs/data-contract.md) 与 [实验设计](../../../cli/docs/experiments/design.md)，运行层只按冻结值转发。
- 适配器在构造时把 URL 重新解析并固定为 origin（`resolve_loopback_origin`）；`transport_config` 用该 origin 替换 guard 用配置中的 URL，但不改变冻结输入，guard 据此重验监听归属。
- strict-output 模式在请求体加 `ignore_eos`，探针响应经 `verify_probe` 核验输出预算达标后才进入正式执行；达标率由 `target_rate` 按 completed/failed 终态与端点用量口径计算，口径细则见 [指标定义](../../../cli/docs/experiments/metrics.md)。

## 适配器边界

- 适配器只做传输与解析：构造请求体、按超时收发、解析普通/流式响应、用量与结束证据、空闲观察、取消；不下载模型、不启动/停止/重启服务。服务生命周期归操作者（批量 handoff）或 Studio，见 [Studio 面板](../studio-panel.md)。
- 响应解析引擎无关地汇总到 `ResponseState`：`terminal()` 以 `finish_reason in ("stop", "length")` 且协议完整判定 `completed`，否则 `failed`；计时用单调时钟，首内容与首答案分列，总截止不随新事件续期。端点未报用量时 token 字段为 `null` 且不补零，计量口径见 [架构](../../../cli/docs/architecture.md) 的「计量与比较」节。
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
- 旧格式拒绝不归本层裁决：`run_trial` 对 `UnsupportedFormat` 原样上抛，由入口层映射，拒绝规则见 [证据、封存与离线读取](../evidence-storage.md) 与 [当前格式契约](../../../cli/docs/contracts/inferyard-current-format.md)。

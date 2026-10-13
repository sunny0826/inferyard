# InferYard Studio API 契约

版本：v0 已冻结（T1 产出；2026-10-12 冻结审查修订 + reviewer 复核 diff 通过；冻结后变更须 coordinator 仲裁 + reviewer 重审）。
补丁 2026-10-12：Q4 dirty 判据落地（§4.4）、Q5 实测结论记录（§9）；无字段/语义变更，reviewer 复核见 T8。
补丁 2026-10-13：DeviceGpuDevice.index 改可选（Darwin 无此字段，L2 真实证据）、§4.6 理由句勘误（S10）、§4.7 .part 生命周期澄清（L2-3）；均为放宽/澄清，无破坏性变更。
权威级别：前后端唯一契约。前端 `web/src/api/types.ts` 与 Go struct 的 json tag 均以
[`docs/api-types.ts`](api-types.ts) 的字段名为准。

## 1. 传输与通用约定

- 单 Go 进程，`net/http` 监听 `127.0.0.1:<随机端口>`；桌面 webview 与浏览器走同一地址。
- 客户端→服务端：REST/JSON；服务端→客户端：SSE 单流。不使用 WebSocket 与 `mygo.Bind`。
- 请求与响应体均为 `application/json; charset=utf-8`。**字段命名统一 snake_case**，
  与 inferyard CLI 的 JSON 输出一致。
- 时间：UTC ISO 8601 字符串（如 `2026-10-12T09:28:41Z`）。来自证据目录的时间原样透传，
  不重解析。
- 字节数：非负整数，单位一律 bytes；速率单位 bytes/s（浮点）。显示换算（GiB 等）只发生在前端。
- **缺测语义**：未知/不适用/未就绪一律 `null`，不用 `0`、空字符串或伪造百分比冒充
  （对齐 inferyard「缺测为 null 并说明原因」规则）。
- 所有路径字段为绝对路径字符串；`null` 表示未配置/未侦测到。

## 2. 鉴权

- 会话 token：每进程随机 32 字节 hex（64 字符），不持久化、不写入日志；重启即失效。
- REST：请求头 `Authorization: Bearer <token>`。M0 spike 为裸 token 直接比对，T4 起按
  Bearer scheme 实现（见 §8）。
- 以下无法自定义请求头的入口使用 query 参数 `?token=<token>`：
  - `GET /api/events`（浏览器 `EventSource`；服务端同时接受 Authorization 头，供非浏览器客户端）；
  - `GET /reports/**`（报告顶层导航）；
  - `GET /` 与 `GET /index.html`（前端入口页；`/assets/**` 静态资源不校验，对齐 M0 行为）。
- query token 泄露面收敛（Q6 裁决升格为正式条款）：全部响应携带
  `Referrer-Policy: no-referrer`；服务端访问日志不记录 query 串/URL（token 永不落日志）。
- 失败统一 `401` + 错误体（§3），`code="unauthorized"`。token 只绑定本机回环；
  LAN 暴露为 P2 显式开关，本契约不覆盖其鉴权模型（LAN 开关实现时必须重审鉴权模型，保持本节声明）。

## 3. 错误模型

### 3.1 错误体

所有非 2xx 响应（含 SSE 建立前拒绝）使用同一结构：

```json
{
  "error": {
    "code": "cli_blocked",
    "message": "inferyard 拒绝执行：输出目录已存在",
    "limitations": ["output_exists"],
    "details": null
  }
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `code` | string | 机器可读固定码，见 3.2 |
| `message` | string | 服务端写死的面向用户摘要（按 code/场景固定文案），**不回显原始异常、stderr 或签名 URL** |
| `limitations` | string[] | inferyard CommandResult 的固定原因数组**原样透传**（如 `invalid_input`、`device_preflight_blocked`、`model_acquire_incomplete`）；非 CLI 错误为 `[]` |
| `details` | object \| null | 可选结构化补充（如冲突的 task_id）；不使用自由文本堆叠 |

### 3.2 HTTP 状态码 × code

| HTTP | code | 触发场景 |
| --- | --- | --- |
| 400 | `invalid_request` | 请求体/参数校验失败（未调用 CLI 前）：缺字段、类型错、`out_name` 非法、resume 只传了 `endpoint_url`/`server_pid` 之一、路径 containment 越界/形态非法（`from_run`、`run_paths`、`/reports/**` 静态路径等）等 |
| 401 | `unauthorized` | token 缺失或不匹配 |
| 404 | `not_found` | 路由不存在；task_id / report_id / run 路径在索引中不存在；静态文件不存在 |
| 409 | `conflict` | 状态冲突：`details.reason` 细分——`queue_duplicate`（同 config 已在队列）、`invalid_state`（对已终态任务取消）、`output_exists`（下载目录名已占用，含失败保留目录）、`not_resumable`（来源非批次内 run、非最新未完成轮次等）、`refresh_in_progress`（device 刷新进行中）、`settings_incomplete`（必要设置未配置）、`output_root_outside_results`（config 冻结 `output.root` 不在 `results_root` 之内） |
| 422 | `cli_blocked` | inferyard 退出码 2（输入/预检阻断）；`limitations` 原样透传 |
| 500 | `internal_error` | app 自身故障（索引扫描失败、事件总线异常等） |
| 500 | `cli_error` | inferyard 退出码 4（工具/证据错误）；`limitations` 原样透传 |
| 503 | `cli_unavailable` | inferyard 二进制未探测到/不可执行/版本查询失败 |

### 3.3 inferyard 退出码 → UI 语义

异步任务（run/download）不直接返回非 2xx，终态经任务对象与 SSE 呈现；同步接口
（settings 探测、device 手动刷新走 SSE、report 生成）按下表映射。

| 退出码 | CLI status | 契约语义 | 呈现规则 |
| --- | --- | --- | --- |
| 0 | complete / prepared / recommended / inspected | 成功 | run 任务 `done`；报告 201；下载任务 `done`（bytes/sha256 为实测值） |
| 2 | blocked | 输入/预检阻断 | 同步接口 422 `cli_blocked`；run 任务 `failed`，error.code=`cli_blocked`；**limitations 固定原因原样展示** |
| 3 | incomplete | 运行不完整 | run 任务 `done` 且 `exit.completeness="incomplete"`；历史状态 `partial`；UI 如实呈现 partial/dirty，不当作失败弹错 |
| 4 | error | 工具/证据错误 | 同步接口 500 `cli_error`；任务 `failed`，error.code=`cli_error`；只展示固定原因 |
| 130 | interrupted | 已取消 | 任务 `cancelled`；历史状态 `cancelled`；不报错 |

注：「CLI status」列为**示例**（report 成功为 `rendered`，批量另有 `finished`/`resumed_subset`
等），**映射以退出码为准**。

其他规则：stderr 诊断不进 `message`（只可落服务端日志）；下载无总量，任何位置不得伪造百分比。

## 4. REST 路由

汇总（全部需鉴权；「事件」列指操作成功后广播的 SSE 事件，见 §5）：

| 方法 | 路径 | 用途 | 成功 | 事件 |
| --- | --- | --- | --- | --- |
| GET | `/api/status` | 服务与 inferyard 探测状态 | 200 | — |
| GET | `/api/settings` | 读设置 + 生效值 | 200 | — |
| PUT | `/api/settings` | 更新设置（部分字段） | 200 | — |
| GET | `/api/device` | 设备快照缓存 | 200 | — |
| POST | `/api/device/refresh` | 触发 device-check 刷新 | 202 | `device.updated` |
| GET | `/api/runs` | 扫描结果根：batch/run 历史 | 200 | — |
| POST | `/api/runs` | 启动 run（一至两个已绑定 config） | 202 | `run.state` |
| POST | `/api/runs/resume` | resume 未完成 run | 202 | `run.state` |
| GET | `/api/run-tasks` | 任务队列快照 | 200 | — |
| GET | `/api/run-tasks/{task_id}` | 单任务详情（进度+资源） | 200 | — |
| POST | `/api/run-tasks/{task_id}/cancel` | 取消任务 | 202 | `run.state` |
| GET | `/api/reports` | 报告列表 | 200 | — |
| POST | `/api/reports` | 生成报告（一至多个 run） | 201 | `report.created` |
| GET | `/reports/{report_id}/report.html` | 报告顶层静态服务 | 200 | — |
| GET | `/api/downloads` | 下载任务列表 | 200 | — |
| POST | `/api/downloads` | 创建下载（source URL + out 目录名） | 202 | `download.state` |
| POST | `/api/downloads/{task_id}/cancel` | 取消下载 | 202 | `download.state` |
| GET | `/api/events` | SSE 单流 | 200 | — |

### 4.1 GET /api/status

响应 `StatusResponse`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `status` | string | 固定 `"ok"`（兼容 M0） |
| `version` | string | app 版本 |
| `inferyard` | object | `{ path, version, status }`：`status` 为 `ok`/`not_found`/`not_executable`/`version_failed`；`path`/`version` 不可得时为 `null` |
| `active_run_tasks` | integer | 未终态 run 任务数（含排队） |
| `active_downloads` | integer | 进行中下载数 |

### 4.2 设置

`GET /api/settings` → `SettingsResponse = { settings: UserSettings, effective: EffectiveSettings }`；
`PUT /api/settings` 接收 `UpdateSettingsRequest`（三个字段均可选，仅更新出现字段），响应同 GET
（更新后重探测生效值）。

`UserSettings`（持久化在 well-known dir 的单个 JSON，重启保留）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `inferyard_path` | string \| null | inferyard 二进制绝对路径；`null` = 自动探测 PATH |
| `results_root` | string \| null | 结果根（run/batch 所在）；`null` = 未配置 |
| `models_root` | string \| null | 模型根（device-check 扫描与下载落盘根）；`null` = 未配置 |

`EffectiveSettings`（每次请求现算，不持久化）：`inferyard_path`/`inferyard_version`/
`inferyard_status`（同 §4.1）、`results_root`、`models_root`、`reports_root`。
`reports_root` 派生为 `dirname(results_root) + "/reports"`，`results_root` 未配置时为 `null`
（Q1 裁决：接受派生而非第四项设置，见 §9）。

校验：提供的路径须为非空绝对路径；`results_root`/`models_root` **只能改值、不能清空**——
不接受 `null`，空串同样非法；仅 `inferyard_path` 可置 `null`（恢复自动探测 PATH）。
非法 → 400 `invalid_request`。设置变更后：结果根变化触发历史索引重建（重建期间
`GET /api/runs` 继续服务旧索引，完成后**原子替换内存索引**）；模型根变化使 device 缓存失效
（下次刷新更新）。设置变更不广播 SSE 事件（P0 已接受限制）：桌面+浏览器双开时，另一端的
设置/历史视图需手动刷新。

### 4.3 设备

- `GET /api/device` → `DeviceResponse`：`{ snapshot, captured_at, refreshing, last_error }`。
  `snapshot` 为 `device-check --json` 的 `details` 对象（`device_preflight.v1`）**原样透传**，
  字段见 `api-types.ts` 的 `DevicePreflight`；从未刷新时为 `null`。启动时服务端自动刷新一次。
- `POST /api/device/refresh` → 202 `DeviceRefreshResponse = { "refreshing": true }`；进行中重复
  调用 → 409 `refresh_in_progress`。完成（含失败）经 SSE `device.updated` 推送；快照含硬件、
  本地 GGUF 列表（大小/SHA256 来源记录/chat_template 可用性）、推荐配置与阻断原因，分区展示由前端负责。
- 刷新命令：`inferyard device-check --json --models-root <models_root>`；`models_root`
  未配置 → 409 `settings_incomplete`。退出 2（如显式模型阻断）不算错误：`snapshot` 照常更新，
  `recommendation.status="blocked"` 与原因由 UI 展示；退出 4 → `device.updated` 携带 `error`。

### 4.4 运行历史：GET /api/runs

扫描 `results_root` 重建（内存索引重启即丢，证据目录为唯一事实源）：

- run 目录 = 含 `inputs.json` 的目录；batch 目录 = 含 `batch.json` 且 `runs/` 子目录内为
  run 目录的目录。单个 `run --config` 产物直接位于结果根下；`run --plan --output-root` 产物为 batch。
- 响应 `RunHistoryResponse`：`{ results_root, scanned_at, batches, standalone_runs }`；
  `BatchSummary = { batch_id, path, runs }`，按开始时间倒序。
- `results_root` 未配置 → 200 且各列表为空、`results_root=null`（首页可用，不算错误）。

`RunSummary` 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `run_id` / `path` | string | 证据身份与绝对路径 |
| `batch_id` | string \| null | 所属批次；单 run 为 `null` |
| `status` | string | `complete`/`partial`/`cancelled`/`dirty`，派生规则见下 |
| `sealed` | boolean | manifest 封存标记（不代表全量核验通过） |
| `completeness` | string \| null | 事件重建的 `complete`/`incomplete`；不可重建时 `null` |
| `stop_reason` | string \| null | 事件重建的停止原因原样值 |
| `model_name` / `model_path` | string \| null | 冻结输入中的模型 |
| `engine` | string \| null | 适配器/引擎标识（如 `kvmem`/`ninfer` 或二进制名） |
| `bundle` | string \| null | 题包名（如 `zh-core`、`zh-svg-pelican`） |
| `score` | object \| null | `{ passed, valid, rate }`：事件聚合的通过数/有效数/通过率；分母为 0 时 `rate=null`；证据不足时整体 `null`。**不读可缺失的 summary.json** |
| `relation` | string \| null | `initial`/`repeat`/`resume`/`rerun`/`derived` |
| `parent_run_id` | string \| null | resume/rerun 来源 |
| `started_at` / `finished_at` | string \| null | 首/末事件 utc |
| `resumable` | boolean | 服务端预判（仅供 UI 置灰）：批次内 run 未完成且为其批次**计划序**最新未完成轮次时为 `true`；standalone run（`batch_id=null`，`run --config` 产物）恒为 `false`。最终以 CLI 裁决为准（§4.5） |

`status` 派生（实现属 evidence 层，契约固定枚举、判据与呈现语义）：
`cancelled`（停止原因为取消/退出 130）＞ `dirty`（判据见下）＞
`partial`（未封存或执行不完整）＞ `complete`（封存且完整）。UI 如实呈现，不合并、不粉饰。

`dirty` 判据（T4 落地，实现 `internal/evidence/dirty.go`；Q4 裁决补丁回写）：读取时主机
持久状态 `inferyard-host.state.json` 满足 `schema_version=1`、`dirty=true`、`kind="run"` 且
`run_id` 与该 run 匹配。Unix 状态目录为 `/var/tmp`，Windows 为系统 Common AppData 已知目录。
状态文件缺失表示当前无可匹配标记（不 dirty）；状态不可读或格式非法返回 `internal_error`，
不推断为 clean。标记经 clean 或被后续任务替换后，历史 run 不再因该标记呈现 dirty；这不证明
其历史 drain 已核验，也不代表证据完整。`cancelled` 优先级仍高于 `dirty`。

### 4.5 运行任务（启动 / 队列 / 详情 / 取消 / resume）

app 内存任务表（重启即丢）；单槽队列串行执行（inferyard 主机锁，不做并行 run）。

**POST /api/runs** — 请求 `StartRunsRequest`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `config_paths` | string[] | 1–2 个**已绑定** config（`config bind` 产物目录或其 `config.toml`）绝对路径 |
| `allow_single_bundle` | boolean（可选，默认 false） | 单 config 时必须显式 `true`（对齐「默认双 bundle，单 bundle 须显式选择」） |

规则：`results_root` 未配置 → 409 `settings_incomplete`。服务端解析 config 得冻结的
`output.root`，作为该任务 run 目录的**侦测根**（`run --config` 产物落在
`<output.root>/<run_id>`，不接受 `--output-root` 覆盖）；预校验 `output.root` 必须位于
`results_root` 之内，否则 409 `output_root_outside_results`（落在界外的 run，侦测与历史索引
均不可见）。两个 config 须绑定不同题包（服务端经 config 解析校验）；UI 默认选择 `zh-core` +
`zh-svg-pelican`。每个 config 生成一个任务，按数组顺序入队串行执行（CLI 不自动连跑两包，
双 bundle 由队列表达）；同次响应的 `tasks` 数组即双包成组依据，T5 进度视图按它客户端成组，
无服务端聚合实体（Q3 裁决）。响应 202 `StartRunsResponse = { tasks: RunTask[] }`。
同 config 已有未终态任务 → 409 `queue_duplicate`。

**POST /api/runs/resume** — 请求 `ResumeRunRequest`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `from_run` | string | 来源 run：绝对路径，或相对 `results_root` 的路径；必须解析在结果根之内（越界 → 400） |
| `endpoint_url` | string（可选） | 服务 endpoint 覆盖；与 `server_pid` **必须同时提供或同时省略**（CLI 规则） |
| `server_pid` | integer（可选） | 服务 PID 覆盖，正整数 |

校验：`results_root` 未配置 → 409 `settings_incomplete`。resume 仅接受**批次内 run**——来源
必须位于 `<批次根>/runs/` 下且未完成；standalone run（`run --config` 产物，`batch_id=null`）
不构成批次结构，`resumable=false`，一律 409 `not_resumable`。来源还须为该批次**计划序**上
最新未完成轮次（`resume_requires_latest_unfinished_run`）：**以 CLI 为最终裁决**，服务端预判
（§4.4 `resumable`）仅供 UI 置灰。响应 202 `{ task: RunTask }`（`kind="resume"`，新 run 目录
出现在同一批次根下，复用同一 tail 通路）。固定时窗负载不可 resume 的情形不在预校验范围：
CLI 返回 status=`duration_requires_new_window`、退出码 3，按 §3.3 映射任务 `done` 且
`exit.completeness="incomplete"`，如实呈现原因（不是 `failed`，也不是 409）。

**GET /api/run-tasks** → `{ tasks: RunTask[] }`（排队中 + 进行中 + 最近终态；创建时间倒序）。

**GET /api/run-tasks/{task_id}** → `RunTaskDetail = { task, progress, resources }`：
`progress` 见 `RunProgress`（starting 早期可为 `null`）；`resources` 为内存环形缓冲的最近
采样点（`ResourceSample[]`，建议 ≥600 点），断线补数据用 SSE 重连 + 本路由重建。

**POST /api/run-tasks/{task_id}/cancel** → 202 `{ task }`：排队中直接 `cancelled`（无退出码，
`exit=null`，`finished_at` 落取消时刻）；running 向子进程发 SIGINT → `cancelling`，待退出 130
落 `cancelled`；已终态 → 409 `invalid_state`。取消不产生错误提示，证据按 partial/dirty 如实呈现。

`RunTask` 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `task_id` | string | app 生成（ULID/UUID） |
| `kind` | string | `run` / `resume` |
| `state` | string | 状态机见 §6 |
| `queued` | boolean | 正在等待执行槽（`state=starting` 的细分，不改枚举） |
| `config_path` | string \| null | `kind=run` 的 config 路径 |
| `resume_from` | string \| null | `kind=resume` 的来源 run 绝对路径 |
| `bundle` | string \| null | 题包名（解析 config 后填入） |
| `run_id` / `run_path` | string \| null | tailer 侦测到新 run 目录后填入（侦测根为 config 冻结的 `output.root`，见上） |
| `created_at` | string | 创建即生成，非空 |
| `started_at` / `finished_at` | string \| null | 任务级时间；排队中取消时 `finished_at` 落取消时刻 |
| `exit` | object \| null | 终态时填（排队中取消无退出码，为 `null`）：`{ exit_code, cli_status, completeness, stop_reason, limitations }`；退出 3 时 `completeness="incomplete"` |
| `error` | ApiError \| null | `failed` 时填（`cli_blocked`/`cli_error`/`cli_unavailable`/`internal_error`） |

`RunProgress`：`{ phase, done, total, current_case_id, last_seq, cases }`。
`total` 取自该 run `inputs.json` 冻结的选中题集合分母——resume run 的 Journal 以
`resume_case_ids` 冻结续测子集，分母天然是续测子集而非全 plan `case_order`；未读到时为 `null`
（不猜）；`done` 为已终态题数；`last_seq` 为已消费 `events.jsonl` 的最大 seq（tail 健康观测）。
`CaseProgress`：
`{ case_id, plan_index, state, category, quality_state, started_at, finished_at, duration_ms, error_category }`，
`state` ∈ `pending`/`running`/`completed`/`failed`/`cancelled`/`invalid`（`pending`/`running`
为 app 聚合态；证据终态仅 `completed`/`failed`/`cancelled`/`invalid` 四值），
`quality_state` ∈ `pass`/`fail`/`unscorable`/`not_applicable`/`null`，缺测为 `null`。

`ResourceSample`（来自 `memory.jsonl` 每秒采样，服务端聚合为 UI 就绪点）：
`{ utc, mem_available_bytes, service_rss_bytes, service_cpu_percent, temperature_celsius }`；
逐项可 `null`（缺测），`service_cpu_percent` 由 ticks 差分计算，不可算时 `null`。

### 4.6 报告

- `GET /api/reports` → `ReportListResponse = { reports_root, reports }`；扫描 `reports_root`
  下含 `report.html` 的目录。`ReportSummary = { report_id, path, created_at, run_ids, run_paths, html_url }`；
  `report_id` = 目录名；`created_at` 取报告**目录 mtime**（index.json 与封存均无生成时间戳，
  跨平台 birthtime 不可靠），取不到时为 `null`；`run_paths` 为 index.json 内报告→run 相对来源
  链接解析出的绝对路径——报告搬移或来源 run 删除后链接失效，对应项为 `null`，UI 呈现
  「来源不可达」占位（不报错、不影响打开报告）；`html_url` =
  `/reports/{report_id}/report.html`（相对路径，前端拼接 token）。
- `POST /api/reports` — 请求 `CreateReportRequest = { run_paths: string[] }`（**1..100 个**，
  对齐 CLI 上限；须解析在 `results_root` 之内）。服务端预校验：数量越界、含重复 run、路径
  越界/形态非法 → 400 `invalid_request`（避免 CLI `duplicate_report_run` 等落入 500）。服务端
  先创建 `reports_root` 父目录（理由勘误：CLI 实际会自动创建父目录——report_common.py:83
  `out.parent.mkdir(parents=True, exist_ok=True)`；服务端主动保证目录存在，不依赖 CLI 该行为，
  S10 挂账），再在 `reports_root` 下生成唯一
  新目录：**`report-YYYYMMDDTHHMMSSZ`**（UTC，文件系统安全、无冒号；同秒碰撞追加序号后缀
  唯一化，如 `report-20261012T092841Z-2`），**同步**执行
  `inferyard report --runs ... --out NEW`：
  成功 201 `CreateReportResponse = { report }` 并广播 `report.created`；
  退出 2 → 422 `cli_blocked`（如 `unsupported_format`），退出 4 → 500 `cli_error`。
  客户端断连/超时**不中断**生成，完成后仍广播 `report.created`（其他客户端经列表刷新可见）。
  报告生成为离线操作但可达数十秒，前端该请求的超时不得低于 120s（Q2 裁决：接受同步 +
  前端长超时；若 T7 实测普遍超时，按冻结变更流程演进）。
- 打开报告：`GET /reports/{report_id}/report.html`（`?token=`）顶层导航打开（webview 新窗口
  或浏览器新标签），**禁止 iframe 嵌入**（报告 CSP 严格）；报告响应携带
  `Referrer-Policy: no-referrer`（§2 条款）。`/reports/{report_id}/` 下其余文件同路径静态服务；
  路径必须 containment 校验：越界/形态非法 → 400 `invalid_request`，文件不存在 → 404（§3.2）。
  报告含本机私有路径，LAN 暴露（P2）前不得开放。

### 4.7 下载

`POST /api/downloads` — 请求 `CreateDownloadRequest`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `source_url` | string | HF/ModelScope `blob` URL，固定 40 位十六进制 revision，单个 `.gguf`（CLI 硬约束，服务端预校验） |
| `out_name` | string | 输出目录名：**单个路径分段**，`^(?!\.+$)[A-Za-z0-9._-]+$`（显式禁止 `.`/`..` 等纯点分段），落盘为 `<models_root>/<out_name>` |

`models_root` 未配置 → 409 `settings_incomplete`；目录已存在（含失败保留目录）→ 409
`output_exists`，message 提示更换目录名（失败目录不能复用）。执行
`inferyard model acquire --source <url> --out <dir>`。响应 202 `{ task: DownloadTask }`。

`GET /api/downloads` → `{ tasks: DownloadTask[] }`（进行中/完成/失败；进程内存表，重启即丢——
已完成模型经 device 刷新后在本机模型列表可见）。

并发模型：`model acquire` 不取 inferyard 主机锁，多个下载任务**允许并行**，与 run 单槽队列
互不阻塞。

`POST /api/downloads/{task_id}/cancel` → 202 `{ task }`：SIGINT 子进程（退出 130 →
`cancelled`）；已终态 → 409 `invalid_state`。失败/取消目录保留，不自动删除。`.part` 临时文件
生命周期归 CLI：inferyard 在中断（含取消）时主动清理 `.part`（model_source_download.py:78-83）；
契约承诺的是目录保留占名（同名 409 `output_exists`），不含 `.part` 留存（T9-L2 实测确认）。

`DownloadTask`：`{ task_id, state, source_url, out_name, out_dir, file_name, downloaded_bytes,
bytes_per_second, started_at, finished_at, result, error }`。
`state` ∈ `downloading`/`done`/`failed`/`cancelled`；`file_name` 为 `.part` 基名（侦测前 `null`）；
`downloaded_bytes` 为 `.part` 当前大小；`bytes_per_second` 为滑动窗口速率；`result` 成功时填
`{ bytes, sha256 }`（实测值，来自最终 JSON / `source.json`）。**无总量字段，不伪造百分比**。

## 5. SSE：`GET /api/events`

单流推送全部事件。鉴权：`?token=` 或 Authorization 头（§2）。

### 5.1 帧格式与重连

```
id: 1024
event: run.state
data: {"task":{...}}

```

- `id`：uint64 十进制，**每进程单调递增**；计数器以启动种子初始化（进程启动时派生，避免重启后
  id 从 0 回退、旧 `Last-Event-ID` 落入新缓冲被误补发）；`event` 为事件类型；`data` 为单行 JSON。
- 心跳：15s 无事件则发注释行 `: hb\n\n`。
- 断线重连：客户端带 `Last-Event-ID`。服务端保留有界回放缓冲（≥2048 条；初始值经 T4 实测
  复核维持不变，Q5 实测结论见 §9；稳态事件率约 ≤5 ev/s）：`last_id` 在缓冲范围内 → 补发缺失后继续实时；
  **小于缓冲最旧值、或大于当前最新值** → 先发 `sync.reset`，客户端收到后必须经 REST 全量重建状态。
- 事件语义：`*` `.state` 为全量替换（携带完整任务对象），`run.progress` 为全量替换，
  `run.resources` / `download.progress` 为增量追加。替换语义保证断线后幂等收敛。

### 5.2 事件目录

| event | data 类型 | 时机 |
| --- | --- | --- |
| `run.state` | `{ task: RunTask }` | 任务创建、出队、侦测到 run 目录、state 迁移、终态落盘 |
| `run.progress` | `{ task_id, run_id, progress: RunProgress }` | 事件流聚合进度变化时（事件驱动，不高于 2Hz 节流） |
| `run.resources` | `{ task_id, points: ResourceSample[] }` | 资源曲线新增采样点（约 1Hz，可批量） |
| `download.state` | `{ task: DownloadTask }` | 下载任务创建、终态迁移 |
| `download.progress` | `{ task_id, downloaded_bytes, bytes_per_second, utc }` | `.part` 轮询（约 1Hz） |
| `device.updated` | `{ snapshot, captured_at, error }` | 刷新完成（成功与失败均发；失败 `error` 非空、`snapshot` 保留旧值） |
| `report.created` | `{ report: ReportSummary }` | 报告生成成功（含本客户端同步 201 之外的广播，供其他客户端刷新列表） |
| `sync.reset` | `{ reason: "buffer_gap" }` | 回放缓冲缺口/服务端重启，要求客户端 REST 全量重建 |

M0 的演示事件 `count` 删除（见 §8）。

## 6. RunTask 状态机与 phase 枚举

状态（`RunTask.state`）：`starting` → `running` → `cancelling` → 终态
`done` / `cancelled` / `failed`。

- `starting`：入队等待（`queued=true`）与子进程启动直至侦测到 run 目录。
- `running`：tail 通路建立，进度聚合中。
- `cancelling`：已发 SIGINT，等待子进程退出。
- 终态映射：退出 0 → `done`；退出 3 → `done`（`exit.completeness="incomplete"`，partial 如实呈现）；
  退出 130 → `cancelled`；退出 2/4 → `failed`；spawn/tail 故障 → `failed`（`internal_error`）。
  终态不可逆；证据目录（partial/dirty）不因此修改。

阶段（`RunProgress.phase`，对齐证据事件枚举）：
`probe` / `warmup` / `baseline` / `formal` / `scoring` / `residual` / `finalizing`；
未侦测到 run 目录前为 `null`。

## 7. 命名与类型镜像

- 全契约字段 snake_case；与 inferyard 证据/CLI 同名字段保持同义同单位。
- TypeScript 镜像：`docs/api-types.ts`（冻结后前端复制为 `web/src/api/types.ts`，
  Go 侧 struct json tag 与其字段名逐一对应）。
- inferyard 透传对象（`DevicePreflight`）按 `device_preflight.v1` 文档化字段建模，
  未知新增字段必须被两端容忍（向前兼容，不严格拒绝）。

## 8. M0 兼容演进

| M0 现状 | 本契约 |
| --- | --- |
| `GET /api/status` → `{"status":"ok"}` | 保留路由，响应扩展为 `StatusResponse`（向后兼容的超集） |
| `GET /api/events` 推送 `event: count` | 路由保留；`count` 演示事件**废弃并删除**，替换为 §5.2 目录 |
| Authorization 裸 token 比对 | 升级为 `Bearer <token>` scheme（T4 实现；前端同源注入随之更新） |
| `/`、`/index.html`、`/assets/**` 鉴权分工 | 沿用 |

## 9. 裁决记录（2026-10-12 冻结审查）

原未决问题 Q1–Q6 已经 coordinator 裁决并落实于正文，本节留档：

- **Q1 reports_root 派生**：接受派生（`dirname(results_root)/reports`），不设第四项设置 → §4.2；
  `reports_root` 父目录由服务端创建 → §4.6。
- **Q2 报告生成同步化**：接受同步 + 前端 ≥120s 超时 → §4.6；客户端断连/超时不中断生成、
  完成仍广播 `report.created`、`run_paths` 上限 100 服务端预校验，均已写入正文。若 T7 实测
  普遍超时，按冻结变更流程演进。
- **Q3 双 bundle 任务模型**：接受「每 config 一任务、按数组顺序串行」粒度 → §4.5；T5 进度视图
  按同次 `POST /api/runs` 返回的 `tasks` 数组客户端成组，无服务端聚合实体。
- **Q4 `dirty` 判据**：已落地（2026-10-12 补丁）。确切判据已回写 §4.4，实现
  `internal/evidence/dirty.go`（补丁措辞与该实现逐项核对一致）；reviewer 复核见 T8。
- **Q5 SSE 回放缓冲 2048 条 / 心跳 15s**：接受为初始值 → §5.1。T4 实测复核完成
  （2026-10-12，数据见 `internal/server/README.md` Q5 节）：progress 最短间隔约 500ms、
  空闲心跳约 15s、2048 条回放缓冲写入 10,000 条保留 2,048 条、越界重连触发 `sync.reset`；
  结论维持初始值 2048 条 / 15s / 2Hz，无参数与语义变更。
- **Q6 query token**：接受 P0 模型；`Referrer-Policy: no-referrer` 与「服务端访问日志不落
  query 串/URL」升格为正式条款 → §2、§4.6；LAN 开关（P2）实现时必须重审鉴权模型，保持 §2 声明。

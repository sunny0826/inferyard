# T9-L1 验收报告：fixture/模拟态的 P0 全路径验收

验收人：verifier（独立验收员，未参与实现）。
验收时间：2026-10-12 22:10 – 23:55（UTC+8）。
验收对象：仓库 `inferyard-studio`，冻结快照 HEAD `571795b`（`git log --oneline -1` 复核一致）。
验收期间未修改任何被验收文件、未提交 git；唯一写操作为本报告。临时产物全部位于
`/tmp/t9-l1/` 与 `/tmp/t9-l1-{darwin,linux,win.exe}`，用户配置目录未被触碰（服务器进程
以 `HOME=/tmp/t9-l1/home` 等临时 HOME 启动；`/var/tmp/inferyard-host.state.json` 为机器上
既存的真实 inferyard 状态文件，验收前已备份、验收后已按原样恢复）。

## 一、验收基线与环境

| 项 | 值 |
| --- | --- |
| HEAD commit | `571795b` fix(web): align run fixtures and schemas with frozen contract |
| 工作树 | 干净（仅未跟踪 `docs/reviews/`，与 T8 收尾状态一致） |
| OS | macOS 26.6.2（darwin/arm64） |
| Go | go1.27.2 darwin/arm64（命令级 `GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0`，经 mise exec） |
| bun | 1.3.5（registry=npmmirror，未写全局配置） |
| Chrome | 154.0.8037.98（本机安装，Playwright `channel:'chrome'`，无浏览器下载） |
| mise | 2026.8.2 |
| 驱动方式 | 全部命令从仓库根或 `/tmp/t9-l1` 执行，未写任何全局配置 |

### 项目自带检查（基线，全部通过）

| 检查 | 命令（仓库根） | 退出码 | 关键结果 |
| --- | --- | --- | --- |
| go vet | `mise exec -- go vet ./...` | 0 | 无诊断 |
| go test -race | `GOTOOLCHAIN=go1.27.2 … mise exec -- go test -race ./... -count=1` | 0 | 4 包 ok：core 2.641s / evidence 1.161s / inferyard 1.206s / server 18.903s |
| 前端类型检查 | `bun run typecheck` | 0 | tsc strict 通过；svelte-check 0 errors / 0 warnings |
| 前端生产构建 | `bun run build:web` | 0 | JS 230232 B / CSS 5813 B / HTML 439 B（与 server README 记载逐字节一致） |
| 三平台构建 | `GOOS=windows/linux go build` + darwin 原生 | 0 | `/tmp/t9-l1-win.exe` 16.1M、`/tmp/t9-l1-linux` 16.0M、`/tmp/t9-l1-darwin` 15.3M |
| embed 验证 | `python3 scripts/verify-embed.py --binary /tmp/t9-l1-darwin` | 0 | 认证入口 200、资产 230232/5813 B、未认证 401、token 不落日志、进程 exit 0 |
| 浏览器套件 | `bun run web/tests/verify.mjs`（仓库根） | 0 | 6/6 PASS（T3 mock/T5 wire/T6 wire/T3 REST+SSE 回归） |
| 非回环绑定 | `go test ./internal/server -run TestAuthenticationAndLoopback -v -count=1` | 0 | PASS（配合 `internal/server/server.go:47-51` `Listen` 仅接受字面 `127.0.0.1`、`main.go` 硬编码监听地址的代码证据） |

### 独立线束构成（本验收的自建证据，非项目测试复述）

自建组件（全部在 `/tmp/t9-l1/`，证据索引见第五节）：

- **fake inferyard CLI**（`fakecli/main.go` → `bin/inferyard`，Go 编译）：以受控文件（`FAKE_CTRL` 目录）驱动
  `--version` / `run --config` / `resume --from-run` / `device-check --json --models-root` /
  `report --runs --out` / `model acquire --source --out`，可产生全部退出码 0/2/3/4/130、
  逐步追加 evidence `events.jsonl`/`memory.jsonl`、响应 SIGINT。
- **服务器线束**（`harness.py`）：以 `HOME=<临时目录>` + `-session-fd` 匿名管道启动真实二进制
  `/tmp/t9-l1/studio`，从管道读取会话 URL/token；REST 客户端 + 原生 socket SSE 客户端
  （query token、Last-Event-ID 头、心跳注释捕获）。
- **静态证据 fixture**（`fixtures.py` → `results/`、`reports/`）：批次 `batch-alpha`
  （complete/partial/cancelled/dirty 四态 + 损坏 run + 事件流损坏 run）、批次 `batch-resume`
  （计划序最新未完成轮次）、standalone complete/incomplete/noscore、含可达与不可达来源的报告目录。
- **验收驱动**（`t9_acceptance.py`）：单进程多阶段跑完全部 REST/SSE wire 断言。
- **GUI 冒烟**（`gui_test.py`）与 **Chrome 导航**（`nav_test.cjs`、`res_test.cjs`）。

## 二、逐项结论

驱动总结果：**144/144 断言通过，退出码 0**（`run.log` 154 行、`results.json` 逐断言记录）。
分布：item1 21、item2 20、item3 26、item4 10、item5 11、item6 19、item7 17、item9 12、item10 8；
item8 由 GUI 冒烟 + Chrome 导航单独覆盖（见下）。

### 1. 首次使用路径 — PASS（21/21）

- 无设置启动：`GET /api/settings` 三项均 null、`effective.reports_root` 为 null；`GET /api/runs`
  返回 `results_root=null` 空列表；`GET /api/device` snapshot null；`GET /api/status` 显示
  fake CLI 经 PATH 自动探测 `status=ok version=0.0.0-fake`，应用版本 0.1.0。
- 未配置时五个写路径全部 `409 settings_incomplete`（runs/runs/resume/downloads/device refresh/reports）。
- 非法设置更新全部 `400 invalid_request`（results_root 置 null / 相对路径 / 空串、models_root 置 null、
  未知字段、非法 JSON 体）。
- `PUT /api/settings` 配置三项后 `effective` 派生正确：`reports_root = dirname(results_root)/reports`、
  CLI 探测 ok+版本。
- 持久化：SIGINT 停止进程后以同一 HOME 重启，`GET /api/settings` 三项与 effective 全部保留。

### 2. 运行历史 — PASS（20/20）

- 层级：`batch-alpha` 4 个有效 run、`batch-resume`、standalone 3 个；`results_root` 正确回显。
- 状态优先级（自建 dirty 主机标记生命周期实测）：无标记时 run-dirty=complete → 写入
  `{"schema_version":1,"dirty":true,"kind":"run","run_id":"run-dirty"}` 后呈现 `dirty` →
  标记指向 run-cancelled 时该 run 仍为 `cancelled`（cancelled > dirty 实证）→ 清除后恢复 complete；
  标记为非法 JSON 时 `GET /api/runs` 返回 `500 internal_error`（不推断为 clean）→ 清理后恢复 200。
- 派生字段：complete run `sealed=true completeness=complete score={passed:2,valid:2,rate:1.0}`
  model/engine/bundle/batch 元数据齐全；partial run `completeness=null`（无 stop 事件）；
  cancelled run `stop_reason=user_cancelled`；无 quality 事件的 run `score=null`；
  standalone `resumable=false`；batch-resume run-init `resumable=true`。
- 损坏降级：非法 `inputs.json` 的 run 与事件流中途损坏的 run 均被跳过（响应 200，不出现在列表；
  服务器日志 `history scan: skipped 2 entries`），批次排序按开始时间倒序。

### 3. 启动/取消/resume — PASS（26/26）

- 双 bundle 默认：`POST /api/runs`（两 config）202 返回两个任务，bundle 分别为 zh-core /
  zh-svg-pelican；单 config 不带 `allow_single_bundle` 与显式 false 均 400（默认拒绝单 bundle）。
- 队列串行：慢任务期间 `GET /api/run-tasks` 实测第一个 running、第二个 `queued=true`。
- `queue_duplicate` 409；排队中取消 → `cancelled` 且 `exit=null`、`finished_at` 落取消时刻；
  对已终态任务再取消 → 409 `invalid_state`；运行中取消 → `cancelling` → 子进程收 SIGINT →
  `cancelled` + `exit.exit_code=130 / cli_status=interrupted / stop_reason=user_cancelled`。
- 退出码映射（fake CLI 逐码驱动）：0→done/finished/complete；2→failed `cli_blocked`
  （limitations `invalid_input` 原样透传）；3→done 且 `completeness="incomplete"`、
  cli_status=`duration_requires_new_window`；4→failed `cli_error`（`tool_interrupted`）。
- containment：`output.root` 在 `results_root` 之外 → 409 `output_root_outside_results`。
- resume：批次内 run → 202 `kind=resume`，fake CLI 在同一批次 `runs/` 下新建
  `run-resume-*` 目录，任务 done；历史中出现 `relation=resume parent_run_id=run-init`，
  完成后来源 run `resumable` 变 false。standalone complete/incomplete resume 均
  409 `not_resumable`（standalone 恒灰）；`endpoint_url` 单给 400；越界 `from_run` 400；未知 404。
- 独立端到端（不经项目线束）：以上全部经由 curl 等价的 Python urllib + 原生 socket 驱动
  **真实二进制** `/tmp/t9-l1/studio` 的 REST/SSE 完成，CLI 子进程为自建 fake。

### 4. RunDetail 实时进度与资源曲线 — PASS（10/10）

- SSE：`run.state` starting→running（侦测到 run 目录后带 `run_id/run_path`）→done；
  `run.progress` `total=4`（来自冻结 selection）、`phase=formal`、done 递增、
  `current_case_id`/cases 状态、`last_seq` 递增；`run.resources` 增量点带 utc。
- REST RunDetail：`progress.done==total==4`、`last_seq>0`；资源点数 ≥4。
- 缺测不补 0：存在 `service_rss_bytes=null` 且 `mem_available_bytes` 非 null 的采样点
  （fake 的 `rss_sampler_missing`）、`temperature_celsius=null` 点（`temperature_sensor_unavailable`）、
  以及由 ticks 差分算出非 null `service_cpu_percent` 的点——三类并存，null 未被填 0。
- 浏览器侧渲染证据：`page-rundetail.png`（运行中徽标 + 逐题表格 + 资源曲线区）、
  `page-rundetail-done.png`（done + 退出摘要 + 4/4 评分表）、`page-resources.png`
  （uPlot canvas 存在、图例注明「缺测保留断点」）。

### 5. Device 三态 — PASS（11/11）

- 未配置：snapshot null / refreshing false / last_error null（见 item1）。
- 刷新中 409：fake `device-check` 延迟 1.5s 期间第二次 `POST /api/device/refresh` →
  409 `refresh_in_progress`；完成经 SSE `device.updated` 推送 `device_preflight.v1` 快照，
  `GET /api/device` 显示 captured_at、refreshing=false。
- CLI 缺失：`inferyard_path` 指向不存在路径后刷新 → SSE `device.updated` 携带
  `error.code=cli_unavailable` 且旧快照保留；`GET /api/device` `last_error` 同步可见；
  settings effective `inferyard_status=not_found`。
- 退出 2 不算错误：`device_exit=2` 刷新后快照更新、`recommendation.status="blocked"`、
  `error=null`。

### 6. 报告 — PASS（19/19）

- 列表：fixture `report-fix1` 列出，`created_at` 取目录 mtime（2023-11-14T22:13:20Z）；
  `run_paths[0]` 解析为可达绝对路径、`run_paths[1]` 为 null（来源不可达占位，服务端如实返回 null，
  UI 占位文案由浏览器套件与项目 harness 覆盖）；`html_url=/reports/{id}/report.html`。
- CLI 缺失生成：`POST /api/reports` → 503 `cli_unavailable`。
- 正常生成：201 + 报告目录落盘（`report-20261012T…Z`），第二个 SSE 连接收到
  `report.created` 广播；列表刷新为 2 条。
- 静态服务：`GET /reports/{id}/report.html?token=` 200 HTML 且带 `Referrer-Policy: no-referrer`；
  缺失文件 404 `not_found`；`/../` 与 `%2e%2e` 越界均 400 `invalid_request`；无 token 401。
- 预校验：相对路径 / 越界路径 / 重复 run / 空列表 / 101 个路径 → 全部 400 `invalid_request`。

### 7. 下载 — PASS（17/17）

- 请求校验：非 HF/MS host、非 blob URL、多段 `out_name`、纯点 `out_name` → 400。
- 进度：SSE `download.progress` 携带 `downloaded_bytes` 与 `bytes_per_second`；
  任务与事件 JSON 中不存在任何 total/percent 字段（无总量不显示百分比）。
- 取消：慢速下载中取消 → 202 → SSE `download.state cancelled`，目录保留（`.part` 存在）；
  复用同名 → 409 `output_exists`（message 提示更换目录名）；预置同名目录同样 409。
- 失败：CLI 退出 2 → `failed` + `cli_blocked` + limitations `model_acquire_incomplete` 原样透传，
  目录保留且继续 409 占用。
- 成功：`done` + `result.bytes=4096`、64 位 sha256、落盘 `weights.gguf` 与 `source.json`。

### 8. 桌面 GUI — PASS（独立冒烟 + Chrome 对照）

- 真实二进制起 MyGo 窗口：`/tmp/t9-l1/studio -spike-evidence /tmp/t9-l1/gui -session-fd N`
  （HOME 指向临时目录）→ 日志 `desktop page loaded from local HTTP`、两次 DOM 采样
  `title=InferYard Studio error=nil`、截图 `/tmp/t9-l1/gui/desktop.png`（181.9K，内容复核：
  Dashboard 渲染、左侧导航含 Dashboard/Runs/Reports/Models/Device/Settings 五入口、
  双 bundle 启动表单、无错误横幅）；SIGINT 后进程 exit 0；token 未出现在日志。
- 五页面导航 + SSE 连接指示（本机 Chrome 对照，headless）：`nav_test.cjs` 对真实二进制后端
  20/20 PASS——dashboard/runs/reports/models/device/settings 六路由（含 RunDetail 共 7 页截图）
  内容渲染、各页 `.connection` 指示「事件已连接」、无 console/页面错误；
  RunDetail 页以真实任务渲染实时进度表与退出摘要（`page-rundetail*.png`）。
- 资源曲线区独立抓取：`res_test.cjs` → uPlot canvas 存在（count=1）、图例说明
  「bytes 换算 GiB / CPU 为 ticks 差分百分比 / 缺测保留断点」（`page-resources.png`）。

### 9. 安全抽查复测 — PASS（12/12）

- 无 token 401、错误 token 401、裸 token（无 Bearer scheme）401、REST 路由上 query token
  拒绝（401）、SSE 错误 query token 401；`/assets/**` 未认证可取（契约 §2 分工）。
- 所有响应带 `Referrer-Policy: no-referrer`（含报告静态响应）。
- token 不落日志：整个验收期间两份服务器日志（`studio.log`、`gui.log`）中 grep 无 token；
  亦无请求 URL/GET 行。
- settings 文件权限：`settings.json` 0600、目录 0700（临时 HOME 下实测 stat）。
- 非回环绑定：`Listen` 仅接受字面 `127.0.0.1`（代码证据）+ 定向测试 PASS（见基线表）。

### 10. 断线韧性 — PASS（8/8）

- SSE 单流事件 id 单调递增且唯一；`device.updated`/`download.state`/`download.progress`
  同流共存。
- Last-Event-ID 回放：携带流中段 id 重连，精确补发 `(mid, latest]` 全部事件、有序、无
  `sync.reset`。
- 越界重连：`Last-Event-ID: 1`（早于缓冲最旧值）→ 单帧 `sync.reset` `{"reason":"buffer_gap"}`、
  帧携带当前最新 id；未来 id（latest+100000）→ 同样 `sync.reset`。
- 心跳：空闲连接 16.5s 内捕获 `: hb` 注释行（15s 间隔与契约一致）。
- `sync.reset` 后 REST 重建：重置帧之后 `/api/runs`、`/api/run-tasks`、detail 全部 200 可用
  （前端全量重建行为另由项目 wire 套件覆盖：T5/T6 sync.reset 重建断言 PASS）。
- 缓冲越界（>2048 条溢出）路径由项目 `TestEventReplayLoad`/Q5 实测覆盖，本轮未重复制造
  万级事件（见「范围说明」）。

## 三、过程中发现的问题与根因

### 3.1 「GUI 截图 internal_error 横幅」专项说明（非产品缺陷）

**现象**：三次 GUI 冒烟截图（23:25、23:31）Dashboard 顶部出现红色错误横幅
「无法读取服务响应，请重试。/ internal_error」，同时 Dashboard 其余内容（启动表单、
任务队列、五页面导航）渲染正常。

**排查**：
1. 首次怀疑并发干扰（当时主驱动正在另一服务器实例上写 `/var/tmp/inferyard-host.state.json`）。
   驱动结束后单独复跑 GUI，横幅**依旧**，排除并发因素。
2. 用同 HOME/同设置的 headless 实例直接 curl `/api/runs`、`/api/status`、`/api/reports`、
   `/api/device`、`/api/run-tasks`、`/api/downloads`——全部 200，排除服务端 500。
3. 服务器日志（`gui.log`）仅有正常的 `history scan: skipped 2 entries`（fixture 降级计数），
   无 internal_error 级日志。
4. 对照前端 `web/src/api/schema.ts`：`snapshot` 校验要求冻结 `device_preflight.v1` 的完整形状
   （`recommendation.{status∈recommended|blocked, selected, reason, candidates, limitations}`、
   `model_requests_sent`、`platform_capabilities.{device_inspection, live_single_run,
   live_experiments, metal_backend_verified}`、`ready_to_run`、`next_steps` 等）。

**根因**：自建 fake `device-check` 的快照载荷过于简陋（仅 `kind/hardware{cpu,memory_bytes}/
models/recommendation{status:"ok"}/platform_capabilities{metal}`），不满足冻结
`device_preflight.v1` 形状；前端 Zod 校验失败后按设计将错误上屏（`error(value) { error = value }`
→ ErrorNotice 显示 internal_error）。**这是验收 fixture 保真度不足，不是产品缺陷**；恰恰证明了
前端对契约形状的校验在真实 GUI 链路上生效。

**修复与复验**：fake `device-check` 改为输出完整冻结形状（`recommendation.status="recommended"`、
`model_requests_sent=0`、`ready_to_run`、`next_steps=[]`、`platform_capabilities` 四布尔等；
blocked 变体同步调整）。重建 fake 后 GUI 冒烟复跑：横幅消失，`/tmp/t9-l1/gui/desktop.png`
（181.9K）显示干净 Dashboard。修复后的 fake 亦用于后续全部 Chrome 导航证据。

### 3.2 线束自身 bug 修复记录（全部为验收工具问题，非产品缺陷）

| # | 组件 | 问题 | 修复 |
| --- | --- | --- | --- |
| 1 | fake CLI | `cpuPrev` 身份表携带 `value:0/read_finished_ns:0`，覆盖真实采样值，`dt>pt` 守卫永不通过 → cpu percent 恒 null | 身份表剔除 value/read_finished_ns，仅保留 clock/source/pid/start_ticks/boot_id/hz |
| 2 | fake CLI | memory 采样 seq 计算碰撞（cpu 行与下一周期 mem 行同 seq 同 utc） | executeTrial 内统一递增计数器闭包 |
| 3 | fake CLI | resume 的存活时延误读 `run_delay_ms` | 增加 `delayCtrl` 参数区分 run/resume |
| 4 | fake CLI | SIGINT 时写配置的 stopReason（如 plan_finished），取消语义失真 | 取消路径恒写 `user_cancelled` |
| 5 | fake CLI | score `category:"core"` 不在前端枚举（instruction/extraction/qa/…） | 改为 `"qa"`（这也是 RunDetail 浏览器渲染曾出横幅的第二个 fixture 保真度问题） |
| 6 | fake CLI | `model_slow` 模式不写 `.part` 数据 → downloaded_bytes 恒 0、无速率 | slow 模式改为持续写块（1GB 上限、50ms 周期） |
| 7 | fake CLI | device-check 载荷缺失冻结字段（见 3.1） | 补全完整 `device_preflight.v1` 形状 |
| 8 | harness SSE | recv 超时后才解析缓冲、且心跳每 15s 重置 idle 计时（recv 超时 40s > 心跳间隔）→ `frames(quiet=…)` 永不返回，驱动在 phase3/7 死等 | 重构为 `_parse`（先解析缓冲）+ 0.25s 有界 recv + 帧历史 |
| 9 | harness SSE | `wait_event` 消费帧后丢弃非匹配帧 → 早期 progress 事件丢失 | 引入 `history` + `cursor` 消费指针 |
| 10 | driver | 未配置 409 测试体缺 `allow_single_bundle:true`，先撞 400 形状校验 | 补齐请求体（产品侧为形状优先校验，行为合理） |
| 11 | driver | `/tmp` 与 `/private/tmp` symlink 规范化导致 out_dir/resume_from 断言失败 | 断言两侧均 realpath |
| 12 | driver | phase7 下载过快（~60ms）→ 无 download.progress 事件、事件数不足 | 放慢为 12288 B @300ms |
| 13 | driver | 部分 `req()` 未传 base → 相对 URL ValueError | harness 增加全局 CURRENT base |
| 14 | nav_test | token 放在 hash fragment 之后 → 入口请求无 token 401 | token 移到 hash 之前 |
| 15 | nav_test | `#/runs/<run_id>` 误用 run id（路由期望 task_id） | 先经 REST 创建任务再导航 `#/runs/<task_id>` |
| 16 | fixtures | report index.json 来源相对路径少一级（`../results` → `../../results`）→ run_paths 双 null | 修正相对路径 |

### 3.3 产品侧观察（如实列出；无阻塞缺陷）

本轮独立验收中**未发现需要修复的产品缺陷**。以下为记录备查的观察，均判定为契约内合理行为：

1. **校验顺序**：`POST /api/runs` 在 `results_root` 未配置时，若请求体同时缺
   `allow_single_bundle`（单 config），返回 400 `invalid_request` 而非 409
   `settings_incomplete`——请求形状校验先于状态校验。两个状态码均属契约 §3.2 合法映射，
   未构成矛盾；记录以防后续对顺序有异议。
2. **路径规范化**：`out_dir`/`resume_from`/`run_path` 回显为 `/private/tmp/...`（macOS `/var`、
   `/tmp` symlink 规范化后的规范路径），与 `internal/core/service.go` Resolve 的 symlink-aware
   canonical 设计一致；前端与 REST 消费无歧义。
3. **前端严格校验的利弊**：snapshot/progress 等 Zod 校验失败时统一上屏 internal_error 并保留
   页面骨架（如 3.1 所述）。对冻结契约的真实 CLI 输出无影响；提示未来若 CLI 新增可选字段，
   前端 schema 需同步（契约 §7 已要求向前兼容，当前实现符合冻结版本）。
4. 既存记录复核：T8 rework 报告的三项 suggestion（server README 测试计数 48→79 过期、
   core README S3 措辞未同步、mock cli_status `complete` vs 真实 `finished`）在本轮复核中
   维持原判，未新增证据；其中第 3 项的真实 CLI status `finished` 已在本轮 fake/实测侧再次印证
   （`exit.cli_status="finished"`）。

## 四、证据索引（全部为实际存在路径）

**仓库内（唯一写产物）**
- 本报告：`docs/reviews/t9-l1-acceptance.md`

**驱动与工具（/tmp/t9-l1/）**
- 验收驱动：`/tmp/t9-l1/t9_acceptance.py`（主驱动）、`/tmp/t9-l1/harness.py`（服务器线束 + REST/SSE 客户端）
- fake CLI：`/tmp/t9-l1/fakecli/main.go` → `/tmp/t9-l1/bin/inferyard`
- fixture 构造：`/tmp/t9-l1/fixtures.py`
- GUI 冒烟：`/tmp/t9-l1/gui_test.py`；Chrome 导航：`/tmp/t9-l1/nav_test.cjs`、`/tmp/t9-l1/res_test.cjs`、`/tmp/t9-l1/nav_server.py`
- 被测二进制（go build 产物复制）：`/tmp/t9-l1/studio`

**运行证据（/tmp/t9-l1/）**
- 主驱动逐断言输出：`/tmp/t9-l1/run.log`（154 行，最终轮 144/144，进程退出码 0）
- 主驱动结构化结果：`/tmp/t9-l1/results.json`（144 条逐断言记录）
- headless 服务器日志（两实例 + 最终轮）：`/tmp/t9-l1/studio.log`
- GUI 服务器日志：`/tmp/t9-l1/gui.log`；GUI 会话：`/tmp/t9-l1/home-gui/`（临时 HOME）
- 排查过程记录：`/tmp/t9-l1/debug_sse.py`、`debug_sse2.py`、`debug_sse3.py`、`debug_p7.py`、`dbg.log`、`dbgp7.log`、`pysample.txt`（SSE 客户端缺陷定位过程）

**fixture 数据（/tmp/t9-l1/）**
- 结果根：`/tmp/t9-l1/results/`（`batch-alpha/runs/{run-complete,run-partial,run-cancelled,run-dirty,run-corrupt,run-truncated}`、`batch-resume/runs/{run-init,run-resume-*}`、`run-standalone-complete`、`run-standalone-incomplete`、`run-noscore`、`out-zh/`；注：`out-pelican` 仅在 pelican 任务实际执行时创建，最终轮该任务为排队取消，故目录未产生）
- 报告根：`/tmp/t9-l1/reports/`（`report-fix1`、`report-20261012T154550Z`）
- 模型根：`/tmp/t9-l1/models/`（`slow-model`、`fail-model`、`good-model`、`replay-model`）
- fake CLI 控制目录：`/tmp/t9-l1/ctrl/`、`/tmp/t9-l1/ctrl-gui/`；config 产物：`/tmp/t9-l1/configs/{zh-core,zh-svg-pelican,outside}/`

**截图（/tmp/t9-l1/）**
- 桌面 GUI：`/tmp/t9-l1/gui/desktop.png`
- Chrome 页面：`page-dashboard.png`、`page-runs.png`、`page-reports.png`、`page-models.png`、`page-device.png`、`page-settings.png`、`page-rundetail.png`、`page-rundetail-done.png`、`page-resources.png`

**三平台构建产物**
- `/tmp/t9-l1-darwin`（15.3M）、`/tmp/t9-l1-linux`（16.0M）、`/tmp/t9-l1-win.exe`（16.1M）

**主机状态**
- `/var/tmp/inferyard-host.state.json`：验收前备份、验收后恢复为机器既存真实状态（0600）

## 五、总体结论

**L1 通过。**

- 项目自带检查（vet / race / typecheck / build / 三平台 / embed / Chrome 套件 / 定向安全测试）
  全部退出码 0。
- 独立线束（真实二进制 + fake CLI + 自建 REST/SSE wire 断言）144/144 通过，覆盖契约 §2–§7
  的 P0 用户路径、错误模型、SSE 语义与安全条款；另有桌面 GUI 冒烟与 Chrome 五页面导航 20/20、
  RunDetail 实时进度/逐题表格/资源曲线的视觉证据。
- 验收过程中出现的全部异常均已定位为验收线束自身问题（16 项修复记录见 3.2），其中两起
  用户可见现象（GUI internal_error 横幅、RunDetail 进度不渲染）经根因分析确认为 fake fixture
  违反冻结数据形状所致，修复 fixture 后复验干净；产品侧无缺陷，观察项见 3.3。
- 范围说明（L1 边界内未覆盖，属 L2/L3）：真实模型服务、真实模型下载、真实 inferyard CLI、
  >2048 条事件缓冲溢出的长时间实测、Windows/Linux 原生进程运行时行为（本轮仅交叉编译）。

## 附：证据路径存在性验证（报告落盘后自检）

自检命令：对上节列出的每个路径逐一执行 `[ -e <path> ]`（2026-10-12T15:58Z 执行）。
结果：**44/44 全部存在，0 缺失**。清单分类计数：

- 驱动与工具 9 项（t9_acceptance.py / harness.py / fixtures.py / gui_test.py /
  nav_test.cjs / res_test.cjs / nav_server.py / fakecli/main.go / bin/inferyard）：全部 OK
- 被测二进制与构建产物 4 项（studio、/tmp/t9-l1-darwin、/tmp/t9-l1-linux、/tmp/t9-l1-win.exe）：全部 OK
- 运行证据 8 项（run.log 154 行、results.json 144 条、studio.log、gui.log、
  debug_sse{,2,3}.py、debug_p7.py、dbg.log、dbgp7.log、pysample.txt）：全部 OK
- 截图 10 项（gui/desktop.png + 9 张 page-*.png）：全部 OK
- fixture 数据 20 项（results 下 11 个 run/批次/输出目录 [out-pelican 已按注修正为未创建]、
  reports 1、configs 3、models 4）：全部 OK
- 主机状态（/var/tmp/inferyard-host.state.json 恢复后仍为机器既存真实状态）：OK
- 本报告文件：OK

缺失项：无（原清单中的 `out-pelican` 经自检发现为条件产物，已在上文修正注明，
不作为缺失证据）。
# Studio 面板

InferYard Studio 是面向本地模型评测的桌面应用：MyGo v0.4.5 原生窗口 UI（Go）直接调用进程内
`core.Service`，CLI 仍是操作的执行者与证据契约的所有者。Studio 作为操作者代理可以启动并管理
模型服务进程（拥有自启动进程、Studio 关停时停止、从不自动重启），但运行评测、生成报告等一切
证据操作都派生 CLI 子进程完成；绑定既有服务仍走 CLI 的 PID 与启动时间身份核验。边界与通用层
职责见 [架构](../../cli/docs/architecture.md)；CLI 侧的运行时、证据与评分行为分别见
[运行时执行与引擎适配](runtime-execution.md)、[证据、封存与离线读取](evidence-storage.md)、
[评分、比较与报告](scoring-reporting.md)。

当前形态为纯原生桌面应用：无 Web 面板、无 headless 模式、无 REST/SSE 服务、无前端构建步骤，
页内刷新由进程内事件总线驱动。页面与运营规则的产品级描述见
[studio/README.md](../../studio/README.md)，本节只记录跨包职责边界、运行路径与约束。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 进程入口与装配 | 嵌入 `mygo.config.ts` 解析 version 写入 `core.Version`；signal context、`core.New`、`mygo.App.WhenReady` 装配窗口与桌面通知；退出时 30 秒超时的 `Service.Shutdown` | `main` 在 [`studio/main.go`](../../studio/main.go) |
| 服务核心 | 设置持久化与探测、设备快照、运行队列、下载、报告、引擎条目的单一持有者；路径包含校验（含符号链接与 macOS `/var` 规范化） | `Service`、`New`、`UpdateSettings`、`saveSettings`、`probe`、`Resolve`、`canonical` 在 [`studio/internal/core/service.go`](../../studio/internal/core/service.go) |
| 请求/响应类型 | 页面与服务间的 JSON 类型镜像（`UserSettings`、`RunTask`、`EngineState`、`ApiError` 等） | [`studio/internal/core/types.go`](../../studio/internal/core/types.go) |
| CLI 进程客户端 | 可执行定位、`--version` 解析、argv 直通执行、退出码映射、取消（SIGINT 后两秒 SIGKILL 兜底）、`progress.v1` 解析 | `Client.Exec`、`Locate`、`MapExitCode`、`ParseProgress`、`SplitCommand` 在 [`studio/internal/inferyard/client.go`](../../studio/internal/inferyard/client.go)、[`studio/internal/inferyard/progress.go`](../../studio/internal/inferyard/progress.go)、[`studio/internal/inferyard/command.go`](../../studio/internal/inferyard/command.go) |
| 向导命令执行 | 预置向导命令的白名单参数校验（仅允许 `init`/`model acquire`/`engine acquire`/`device-check`/`config create`/`config bind`/`run`/`resume`/`report`/`verify` 及列名旗标）、`--out` 不存在性预检、执行后状态回写 | `WizardExecute`、`wizardArgs` 在 [`studio/internal/core/onboarding.go`](../../studio/internal/core/onboarding.go) |
| CLI 安装与预置资产 | 复用已有 `inferyard`/uv（含 Homebrew 路径），或下载固定版本独立 uv（sha256 校验）后 `uv tool install inferyard==Version`；内置 SmolLM2-135M 模型 URL 与按平台固定的 Prism 引擎归档下载/解包 | `InstallCLI`、`FindCLI`、`FindUV` 在 [`studio/internal/core/install.go`](../../studio/internal/core/install.go)；`DefaultModelURL`、`DefaultEngineURL`、`FetchEngine` 在 [`studio/internal/core/defaults.go`](../../studio/internal/core/defaults.go) |
| 就绪度与目录供给 | CLI/工作区/结果/模型四就绪度；workspace 初始化标志为 `preparation.json` 的 `community_init.v1`；`Provision` 显式幂等建目录 | `Readiness`、`Provision`、`workspaceRoot` 在 [`studio/internal/core/readiness.go`](../../studio/internal/core/readiness.go) |
| 引擎服务生命周期 | 解析冻结配置 `candidate.toml` 的 `[engine]` 启动行、端口占用预检、子进程拉起、健康端点等待、停止与终止 | `StartEngine`、`StopEngine`、`monitorEngine`、`terminateEngine` 在 [`studio/internal/core/engine_service.go`](../../studio/internal/core/engine_service.go)；`Startup`、`Processes` 在 [`studio/internal/core/startup.go`](../../studio/internal/core/startup.go) |
| 绑定存活提示 | 只读比对绑定配置里的 `server_pid`/`process_start_ticks` 与进程实际启动 tick，仅作 UI 提示 | `BindingAlive` 在 [`studio/internal/core/binding.go`](../../studio/internal/core/binding.go) |
| 运行队列 | 绑定配置准入（含结果根包含、去重）、串行 worker、取消、resume 准入（历史索引 + CLI 终审） | `StartRuns`、`Resume`、`CancelRun`、`runQueue` 在 [`studio/internal/core/runs.go`](../../studio/internal/core/runs.go) |
| 下载任务 | 与运行队列并发的模型/引擎获取，共用同一任务列表；模型任务 `.part` 尺寸轮询与正式文件发布观测；引擎任务复用 `FetchEngine` 传输与解包、取消贯穿全程、字节与速率缺测为带 `progress_reason` 的 `null`、成功写 `engine_path` | `CreateDownload`、`CreateEngineDownload`、`CancelDownload` 在 [`studio/internal/core/downloads.go`](../../studio/internal/core/downloads.go)；`FetchEngine` 在 [`studio/internal/core/defaults.go`](../../studio/internal/core/defaults.go)；`PollDownload` 在 [`studio/internal/evidence/download.go`](../../studio/internal/evidence/download.go) |
| 报告 | 1–100 个来源 run 校验、唯一命名新目录生成、索引与静态路径包含解析；离线 HTML 在独立 WebView 窗口以 `file://` 打开 | `CreateReport`、`Reports`、`ReportFile` 在 [`studio/internal/core/reports.go`](../../studio/internal/core/reports.go)；`Reports.Open`、`reportURL` 在 [`studio/internal/pages/reports.go`](../../studio/internal/pages/reports.go) |
| 进程内事件 | 2048 条回放环 + 唤醒订阅；慢消费者按游标重建而非丢事件 | `EventBus`、`Publish`、`Subscribe`、`After`、`ReplayCapacity` 在 [`studio/internal/core/events.go`](../../studio/internal/core/events.go) |
| 证据索引与读取 | 结果根扫描（损坏条目跳过并计数）、JSONL 增量 tail、host dirty 标记判读、状态优先级 | `Index` 在 [`studio/internal/evidence/index.go`](../../studio/internal/evidence/index.go)；`Tail`、`Cursor` 在 [`studio/internal/evidence/tail.go`](../../studio/internal/evidence/tail.go)；`DirtyRun`、`RunStatus` 在 [`studio/internal/evidence/dirty.go`](../../studio/internal/evidence/dirty.go) |
| 历史聚合 | evidence 记录 → run 汇总/进度/资源投影；resume 资格标记；原子重建历史索引 | `summarize`、`scan`、`markResumable`、`History`、`rebuild` 在 [`studio/internal/core/aggregate.go`](../../studio/internal/core/aggregate.go) |
| 页面 | 单主窗口六个页内页面（向导/设备/模型与下载/运行/报告/设置）、事件驱动刷新、向导下一步命令推导；模型与下载页按模型/下载/引擎服务/绑定存活提示四区组织，done 引擎任务的路径在向导引擎路径为空时由页面幂等采纳 | `New`、`navigation`、`viewState` 在 [`studio/internal/pages/window.go`](../../studio/internal/pages/window.go)、[`studio/internal/pages/onboarding_actions.go`](../../studio/internal/pages/onboarding_actions.go)；四区在 [`studio/internal/pages/models.go`](../../studio/internal/pages/models.go)、[`studio/internal/pages/models_table.go`](../../studio/internal/pages/models_table.go)、[`studio/internal/pages/models_engine.go`](../../studio/internal/pages/models_engine.go) |
| 桌面通知 | 终态 run/download 与 `report.created` 的固定文案通知；投递失败不影响任务结果 | `Subscribe`、`messageFor` 在 [`studio/internal/desktop/notifications.go`](../../studio/internal/desktop/notifications.go) |

关键状态的事实源：设置持久化在 `os.UserConfigDir()/InferYard Studio/settings.json`（目录 0700、
文件 0600、原子替换），损坏 JSON 整体回退默认值并记诊断；引擎状态机由 `engine_service.go` 独占
（`stopped`/`starting`/`running`/`stopping`/`failed`）；运行队列与历史索引常驻 `Service` 内存，
证据目录才是完成性的唯一事实源，内存任务表不随重启存活。版本唯一来源是
[`studio/mygo.config.ts`](../../studio/mygo.config.ts)，由 `main` 经 `go:embed` 读取。
`InstallCLI` 固定的 Python（`installPython = "3.14.7"`）与独立 uv（`uvRelease`）版本必须与
[cli/docs/installation.md](../../cli/docs/installation.md) 保持同步（源码注释的维护义务）。

## 运行路径

1. **启动**：`main` 解析嵌入配置设置 `core.Version`，用 `signal.NotifyContext` 建应用上下文并
   `core.New` 创建服务（读设置、启动串行 runQueue、按需 `RefreshDevice`），随后 `mygo.App.Run`
   进入 WhenReady：注册桌面通知订阅、创建 `pages.Reports` 与主窗口 `pages.New`。SIGINT/SIGTERM
   触发 `mygo.App.Quit` 与 `Service.Shutdown`。
2. **页面刷新**：主窗口的六页面共用一个 `viewState`；后台 goroutine 订阅 `EventBus`，按游标
   取增量事件，`sync.reset`/`report.created`/`run.state` 触发全量刷新，其余做局部更新。
3. **向导执行**：向导页按就绪度推导下一条 CLI 命令（`nextCommand`/`baseCommand`），交给
   `WizardExecute`：`wizardArgs` 白名单校验 → `run`/`resume` 转入运行队列 → 其余命令探测 CLI
   后执行，`--out` 必须先不存在；`init` 成功后回写 workspace/results 设置，`report` 成功发布
   `report.created` 并可追加 `verify`。
4. **运行**：`StartRuns` 逐配置读取 bundle 与输出根并做包含/去重准入 → 入队由 `runQueue` 串行
   `executeRun` 派生 `inferyard run --config`（取消为 SIGINT + 两秒兜底）；执行循环每
   `ProgressInterval`（500ms）对比运行前后证据索引、tail `events.jsonl` 投影 case 进度与分数、
   聚合资源采样；终态经 `run.state` 事件发布。向导页另经 `Service.Progress` 派生
   `inferyard progress --workspace` 解析 `progress.v1` 恢复工作区进度。运行实现与取消语义见
   [运行时执行与引擎适配](runtime-execution.md)。
5. **引擎服务**：向导/运行页 `StartEngine` → `Startup` 从冻结配置目录取 `[engine]` 启动行 →
   监听端口预检 → 拉起引擎子进程（stdout/stderr 写入 workspace `logs/engine-*.log`）→
   `monitorEngine` 每秒探测 `{endpoint}/health`，2xx 转 `running`；启动默认超时 10 分钟。
   `StopEngine` 仅在无活动运行时允许，SIGTERM 后 10 秒未退出则 kill。
6. **绑定**：`Processes` 只暴露 PID 与可执行名（argv 可能带凭据）；对操作者自启动服务执行
   `config bind` 生成绑定配置，`BindingAlive` 仅提供存活提示，身份核验始终由 CLI 在
   bind/run 时完成。
7. **报告**：`CreateReport` 校验 1–100 个位于结果根内的不同 run → 派生 `inferyard report`
   写入结果根同级 `reports/` 下的新唯一目录 → 发布 `report.created`；页面 `Reports.Open` 以
   每报告一个原生 WebView 窗口打开 `report.html`（`file://`，同路径复用聚焦）。报告 ID 为 UTC
   `report-YYYYMMDDTHHMMSSZ[-N]`，命名冲突追加序号。报告与导出契约
   见 [评分、比较与报告](scoring-reporting.md) 与
   [report-contract](../../cli/docs/contracts/report-contract.md)。
8. **下载**：模型/引擎获取与运行队列并发、互不占位，共用 `downloading → done/failed/cancelled`
   任务列表与 `CancelDownload`。`CreateDownload` 只接受无用户信息、查询
   与片段的 `https` 来源；轮询观测 `.part` 尺寸并以 5 读数窗口估算速率，正式发布文件出现后完成
   任务并按实测字节收尾。引擎获取（预置或自定义来源）经 `CreateEngineDownload` 进入同一列表，
   复用 `FetchEngine` 传输与解包：归档解包保留同级共享库、仅重建归档内相对
   符号链接（`safeLinkTarget` 拒绝越界链接），输出目录已存在或被在途任务占用即拒绝
   （`output_exists`），取消贯穿传输与解包；该传输接口不提供字节读数，引擎任务的
   `downloaded_bytes`/`bytes_per_second` 恒为 `null` 并带 `progress_reason`，成功收尾写
   `engine_path`。
9. **关停**：`Shutdown` 立即关闭准入，未终态队列任务标 cancelled、活动任务标 cancelling 并
   取消上下文，待 runQueue 排空后停止自启动引擎再置 `shutdownDone`；调用方超时（main 为 30
   秒）不抛弃清理、不改变顺序。

## 关键状态转换

### 引擎条目（`EngineState.State`）

- `stopped`：`StartEngine` 前的空态；`starting`/`running`/`stopping` 期间重复启动不新拉进程，
  直接返回当前状态（幂等）。workspace 根变更丢弃整个引擎条目（`saveSettings`）。
- `starting → running`：`monitorEngine` 每秒探测 `{endpoint}/health`，2xx 转 `running`。
- `starting → failed`：健康等待超过 `engineStartTimeout`（默认 10 分钟，
  `Options.EngineStartTimeout` 可覆盖）→ `engine_start_timeout`；进程自行退出 → `engine_exited`。
- `starting`/`running → stopping → stopped`：来自 `StopEngine`（要求无活动运行）或 `Shutdown`
  排空后；先 SIGTERM，10 秒未退出则 kill。关停进行中即使启动超时也须等待运行清理结束。
- 任何转换都不触发自动重启；`failed`/`stopped` 之后允许再次 `StartEngine`。

### 运行任务（`RunTask`）

- 准入后入队（`Queued=true`）→ `runQueue` 取件并记 `StartedAt` → 执行循环按
  `ProgressInterval`（500ms）轮询证据索引，发现本次新 run 目录后置 `RunPath`/`RunId` 并转
  `running`。
- `CancelRun`：队列中直接终态 `cancelled`；执行中置 `cancelling` 并取消上下文，等待子进程退出
  后由 CLI 退出码映射终态；终态后取消返回 `invalid_state`。
- 终态 `done`/`failed`/`cancelled` 不可变；`Shutdown` 对队列任务直接 `cancelled`、对活动任务
  置 `cancelling`。

### 下载任务（`DownloadTask`）

- `CreateDownload`（模型，`Kind="model"`）与 `CreateEngineDownload`（引擎，`Kind="engine"`）
  共用同一任务列表与 `downloading → done/failed/cancelled` 转换；输出目录已存在或被在途任务
  占用返回 `output_exists`，来源或目录名非法返回 `invalid_request`，服务关停中返回
  `service_closing`。
- 模型任务轮询 `.part` 尺寸、以 5 读数窗口估算速率，正式文件发布后按实测字节收尾（结果
  `{bytes, sha256}`）；引擎任务复用 `FetchEngine` 传输与解包，取消贯穿全程，
  `downloaded_bytes`/`bytes_per_second` 恒为带 `progress_reason` 的 `null`，成功写
  `engine_path`。
- `CancelDownload` 取消 `downloading` 任务；终态后取消返回 `invalid_state`。

### 历史 run 状态（投影，非新状态机）

`RunStatus` 按冻结优先级派生：stop reason 为 `user_cancelled` → `cancelled`；否则 host 标记
匹配 → `dirty`；否则未封存或 completeness 非 `complete` → `partial`；否则 `complete`。优先级
`cancelled > dirty > partial > complete`；该索引是 UI 投影，不执行 inferyard 的证据/哈希验证，
完成性与 resume 终审归 CLI。

## 约束与失败行为

- **引擎状态机与所有权**：`stopped → starting → running → stopping → stopped`；进程在
  `starting`/`running` 期自行退出判 `failed`（`engine_exited`），启动超时判 `failed`
  （`engine_start_timeout`），均不自动重启——Studio 只拥有并停止自己拉起的进程，异常遗留的
  引擎需在运行页手动停止后重新绑定。端口占用返回 `engine_port_in_use`；有活动运行时
  `StopEngine` 返回 `run_active`；引擎 `stopping` 期间拒绝新运行（`engine_stopping`）；
  服务关停中一切准入返回 `service_closing`。
- **模拟不授予真机资格**：测试用注入式 fixture executor，不启动真实模型服务或网络下载；
  Go 单测与跨平台交叉编译不构成 Windows/Linux 真机验收，原生 UI 迁移需新的真机验收证据
  （见 [studio/README.md](../../studio/README.md) 的 Verification boundaries）。
- **运行队列**：串行执行；1–2 个配置且同 bundle 去重、输出根必须解析到结果根内
  （否则 `output_root_outside_results`）；非终态重复配置返回 `queue_duplicate`；单 bundle 运行
  需要显式声明（向导路径显式允许；UI 直建单 bundle 需用户确认后才置 `AllowSingleBundle`）。resume
  由历史索引给资格、CLI 终审，`--endpoint-url` 与 `--server-pid` 必须成对；缺 progress 命令的旧
  CLI 返回 `cli_outdated`。
- **历史与状态优先级**：历史索引从不读 `summary.json`；缺失或损坏证据不能证明完成，损坏
  run/batch 条目单独跳过并计数；host dirty 标记不可读或格式非法是 `internal_error` 而非 clean；
  状态优先级 `cancelled > dirty > partial > complete`，封存本身不是完成证明。证据字段语义与
  lineage 结论引用 [数据契约](../../cli/docs/data-contract.md#证据血缘与比较结论)，证据布局见
  [run-evidence-layout](../../cli/docs/contracts/run-evidence-layout.md)。
- **报告**：1–100 个不同来源 run；新目录唯一命名、原始 run 与历史报告永不覆写；报告路径必须
  留在报告目录内；缺失来源 run 显示为不可用但不阻止打开。
- **安全**：模型端点仅本地 loopback；token/凭据/完整 session URL 不进日志、设置或用户可见错误；
  向导执行不回传 stdout/stderr；设置文件 0700/0600 且仅在用户保存时替换。CLI 退出码语义
  （0/2/3/4/130）与呈现映射见 [CLI 与应用分发](cli-dispatch.md)。
- **缺测与单位**：未知测量为带原因的 `null`，不补零、不编百分比；内存以字节、速率以字节/秒
  存储，展示换算显式；资源曲线保留缺失采样。
- **采集节奏**：运行内进度事件以 500ms 间隔轮询生成并合并发布；资源历史保留 600 个采样点，
  CPU 差分要求进程/时钟身份一致、计数单调且采样间隔不超两秒，否则该点为 `null`。

## 验证

在仓库的 `studio/` 目录内执行（环境变量仅作用于对应命令，见
[studio/README.md](../../studio/README.md)）：

```sh
cd studio
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go vet ./...
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -o build/inferyard-studio .
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct sh scripts/build-all.sh
```

## 与既有文档的差异

- [`studio/internal/core/README.md`](../../studio/internal/core/README.md) 仍按旧 Web 后端描述
  "Slow clients rebuild from REST" 并引用 `../server/README.md`；`internal/server` 目录已不存在，
  当前 UI 直接订阅进程内 `EventBus`（`core/events.go`），无 REST/SSE。该 README 其余职责划分
  与当前源码一致。
- [`studio/internal/desktop/README.md`](../../studio/internal/desktop/README.md) 的历史验证表引用
  `bun run ... web/tests/verify.mjs` 与 `python3 scripts/verify-embed.py`；当前工作树无 `web/`
  目录，`studio/scripts/` 仅有 `build-all.sh` 与 `package.py`，两条命令均不存在。
- `studio/docs/evidence/` 下的 `browser.*`、`sse.*`、`frontend-build.log` 等是旧 Web UI 阶段的
  诊断产物；根 README 明确其为历史证据、非当前构建说明。当前为 MyGo v0.4.5 原生 UI，无前端
  构建步骤。

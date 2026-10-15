# Studio 面板

InferYard Studio 是面向本地模型评测的桌面应用：onevra v0.2026.9（构建在 MyGo v0.4.8 之上）
原生窗口 UI（Go）直接调用进程内
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
| 进程入口与装配 | 嵌入 `mygo.config.ts` 解析 version 写入 `core.Version`；signal context、`core.New`、`mygo.App.WhenReady` 装配 onevra `shells/sidebar` 主窗口与桌面通知；退出时 30 秒超时的 `Service.Shutdown` | `main` 在 [`studio/main.go`](../../studio/main.go) |
| 服务核心 | 设置持久化与探测、设备快照、运行队列、下载、报告、引擎条目的单一持有者；路径包含校验（含符号链接与 macOS `/var` 规范化） | `Service`、`New`、`UpdateSettings`、`saveSettings`、`probe`、`Resolve`、`canonical` 在 [`studio/internal/core/service.go`](../../studio/internal/core/service.go) |
| 请求/响应类型 | 页面与服务间的 JSON 类型镜像（`UserSettings`、`RunTask`、`EngineState`、`ApiError` 等） | [`studio/internal/core/types.go`](../../studio/internal/core/types.go) |
| CLI 进程客户端 | `--version` 解析、argv 直通执行、退出码映射、取消（SIGINT 后两秒 SIGKILL 兜底）、`progress.v1` 解析 | `Client.Exec`、`ParseVersion`、`MapExitCode`、`ParseProgress`、`SplitCommand` 在 [`studio/internal/inferyard/client.go`](../../studio/internal/inferyard/client.go)、[`studio/internal/inferyard/progress.go`](../../studio/internal/inferyard/progress.go)、[`studio/internal/inferyard/command.go`](../../studio/internal/inferyard/command.go) |
| Dashboard 命令执行 | 预置向导命令的白名单参数校验（仅允许 `init`/`model acquire`/`engine acquire`/`device-check`/`config create`/`config bind`/`run`/`resume`/`report`/`verify` 及列名旗标）、`--out` 不存在性预检、执行后状态回写 | `WizardExecute`、`wizardArgs` 在 [`studio/internal/core/onboarding.go`](../../studio/internal/core/onboarding.go) |
| CLI 安装与预置资产 | 复用已有 `inferyard`/uv（含 Homebrew 路径），或下载固定版本独立 uv（sha256 校验）后 `uv tool install inferyard==Version`；内置 SmolLM2-135M 模型 URL 与按平台固定的 Prism 引擎归档下载/解包 | `InstallCLI`、`FindCLI`、`FindUV` 在 [`studio/internal/core/install.go`](../../studio/internal/core/install.go)；`DefaultModelURL`、`DefaultEngineURL`、`FetchEngine` 在 [`studio/internal/core/defaults.go`](../../studio/internal/core/defaults.go) |
| 就绪度与目录供给 | CLI/工作区/结果/模型四就绪度；workspace 初始化标志为 `preparation.json` 的 `community_init.v1`；`Provision` 显式幂等建目录 | `Readiness`、`Provision`、`workspaceRoot` 在 [`studio/internal/core/readiness.go`](../../studio/internal/core/readiness.go) |
| 引擎服务生命周期 | 解析冻结配置 `candidate.toml` 的 `[engine]` 启动行、端口占用预检、子进程拉起、健康端点等待、停止与终止；拉起成功即向 workspace `logs/engine-owner.json` 持久化所有权记录（PID + start ticks + endpoint + 日志/候选基名，不含 argv），正常停止时按记录身份原子移除 | `StartEngine`、`StopEngine`、`monitorEngine`、`terminateEngine` 在 [`studio/internal/core/engine_service.go`](../../studio/internal/core/engine_service.go)；所有权读写与启动核验在 [`studio/internal/core/engine_owner.go`](../../studio/internal/core/engine_owner.go)；`Startup`、`Processes` 在 [`studio/internal/core/startup.go`](../../studio/internal/core/startup.go) |
| 遗留引擎核验与清理 | 启动时读 `engine-owner.json`：记录损坏/非法或身份无法核验时提供「仅删除记录（不终止进程）」入口，删除前重读记录，身份匹配则拒绝并要求停止；PID 已退出或 start ticks 不匹配（PID 复用）只移除记录；身份匹配时提供显式「停止遗留服务」按钮，停止前与停止中逐次重核身份，SIGTERM 超时升级 kill，任何一步不确定都不发信号 | `DiscardOrphanRecord` 在 [`studio/internal/core/engine_owner_recovery.go`](../../studio/internal/core/engine_owner_recovery.go)；`detectEngineOwner`、`StopOrphanEngine`、`orphanIdentity`、`stopOrphan` 在 [`studio/internal/core/engine_owner.go`](../../studio/internal/core/engine_owner.go)、[`studio/internal/core/engine_orphan.go`](../../studio/internal/core/engine_orphan.go) |
| 本机服务发现 | 调用 CLI `service discover` 并解析（枚举与探测全部在 CLI 侧，Studio 不新增端口/进程枚举代码）；CLI 缺失或过旧返回 `cli_outdated`；外部服务一键绑定显式传 discover 返回的 PID 与 endpoint，仍走 CLI 绑定核验 | `Discover` 在 [`studio/internal/core/discovery.go`](../../studio/internal/core/discovery.go)；解析在 [`studio/internal/inferyard/discovery.go`](../../studio/internal/inferyard/discovery.go)；契约见 [CLI 接口](../../cli/docs/cli-surface.md#本机服务发现契约) |
| 引擎核验 | 调用 CLI `engine check`（macOS 传 `--engine`，Windows 传 `--runtime-receipt`，回执按 CLI 产物布局在引擎二进制同目录或其上级目录查找）核验清单引擎；缓存键含路径 + mtime + 同目录 dylib 集合指纹（含符号链接条目与目标状态）+ receipt 大小与修改时间；核验任务绑定发起时的 CLI 路径与 generation，CLI 切换后过期结果不写回；有界并发与超时；瞬态失败 60s 退避；CLI 过旧降级为「未核验」且不禁选；触发时机为事件驱动刷新（页面可见时）与「模型」页导航点击进入 | `CheckEngine`、`VerifyEngines` 在 [`studio/internal/core/engine_check.go`](../../studio/internal/core/engine_check.go)；解析在 [`studio/internal/inferyard/engine_check.go`](../../studio/internal/inferyard/engine_check.go)，契约黄金样本（合法/非法）在 [`studio/internal/inferyard/testdata/engine_check_contract.json`](../../studio/internal/inferyard/testdata/engine_check_contract.json)，CLI 侧 schema 校验同一组样本；契约见 [CLI 接口](../../cli/docs/cli-surface.md#引擎核验契约) |
| 清单注册表 | 聚合模型清单（设备快照 + 模型下载任务）与引擎清单（`EngineRoots` 双根遍历，嵌套深度 ≤4、可执行位 + 引擎下载任务含 plain-file 记录）；只输出名称与展示信息，同名附加来源标记、再冲突加路径哈希前缀；路径留在 core 内部按名称映射回真实路径 | `Registry`、`resourcePath`、`scanEngineBinaries` 在 [`studio/internal/core/registry.go`](../../studio/internal/core/registry.go) |
| 绑定存活提示 | 只读比对绑定配置里的 `server_pid`/`process_start_ticks` 与进程实际启动 tick，仅作 UI 提示；比对失败显示「无法确认」，不据此判定绑定有效 | `BindingAlive` 在 [`studio/internal/core/binding.go`](../../studio/internal/core/binding.go) |
| 运行队列 | 绑定配置准入（含结果根包含、去重）、串行 worker、取消、resume 准入（历史索引 + CLI 终审） | `StartRuns`、`Resume`、`CancelRun`、`runQueue` 在 [`studio/internal/core/runs.go`](../../studio/internal/core/runs.go) |
| 下载任务 | 与运行队列并发的模型/引擎获取，共用同一任务列表；输出目录名取自设置的 `model_download_name`/`engine_download_name`（默认 `model`/`engine`，仅允许字母数字点下划线连字符且不能全为点），占用时自动追加 `-2`/`-3` 序号，任务详情展示实际目录名；模型任务 `.part` 尺寸轮询与正式文件发布观测；引擎任务复用 `FetchEngine` 传输与解包、取消贯穿全程、字节与速率缺测为带 `progress_reason` 的 `null`、成功写 `engine_path` | `CreateDownload`、`CreateEngineDownload`、`CancelDownload` 在 [`studio/internal/core/downloads.go`](../../studio/internal/core/downloads.go)；目录名解析在 [`studio/internal/core/download_names.go`](../../studio/internal/core/download_names.go)；`FetchEngine` 在 [`studio/internal/core/defaults.go`](../../studio/internal/core/defaults.go)；模型下载采样在 `Service.download` 中与任务生命周期合并 |
| 报告 | 1–100 个来源 run 校验、唯一命名新目录生成、索引与静态路径包含解析；离线 HTML 在独立 WebView 窗口以 `file://` 打开 | `CreateReport`、`Reports`、`ReportFile` 在 [`studio/internal/core/reports.go`](../../studio/internal/core/reports.go)；`Reports.Open`、`reportURL` 在 [`studio/internal/pages/reports.go`](../../studio/internal/pages/reports.go) |
| 进程内事件 | 2048 条回放环 + 唤醒订阅；慢消费者按游标重建而非丢事件 | `EventBus`、`Publish`、`Subscribe`、`After`、`ReplayCapacity` 在 [`studio/internal/core/events.go`](../../studio/internal/core/events.go) |
| 证据索引与读取 | 结果根扫描（损坏条目跳过并计数）、JSONL 增量 tail、host dirty 标记判读、状态优先级 | `Index` 在 [`studio/internal/evidence/index.go`](../../studio/internal/evidence/index.go)；`Tail`、`Cursor` 在 [`studio/internal/evidence/tail.go`](../../studio/internal/evidence/tail.go)；`DirtyRun`、`RunStatus` 在 [`studio/internal/evidence/dirty.go`](../../studio/internal/evidence/dirty.go) |
| 历史聚合 | evidence 记录 → run 汇总/进度/资源投影；resume 资格标记；原子重建历史索引 | `summarize`、`scan`、`markResumable`、`History`、`rebuild` 在 [`studio/internal/core/aggregate.go`](../../studio/internal/core/aggregate.go) |
| 页面 | 单主窗口六个页内页面（Dashboard/设备/模型/下载/报告/设置）、事件驱动刷新、Dashboard 下一步命令推导；「模型」页按选择区（模型 × 引擎双向联动选择框，标注引擎核验状态与模型格式匹配）、运行区（启动/停止与状态归因）、只读的本机运行服务卡三区组织，Dashboard 候选阶段的模型/引擎下拉与模型页选择框是同一选择的同步视图（候选阶段引擎选择为名称下拉，路径映射在 core 内完成，选择为空时默认采纳清单中首个已核验可用的引擎（清单排序：最新完成下载优先，其次最近修改））；「下载」页集中下载任务清单与新建下载表单；全页面名称化展示（模型名/引擎名/目录基名/bundle 名/日志文件名），完整路径仅设置页 | `New`、`navigation`、`viewState` 在 [`studio/internal/pages/window.go`](../../studio/internal/pages/window.go)、[`studio/internal/pages/dashboard_actions.go`](../../studio/internal/pages/dashboard_actions.go)；模型页三区在 [`studio/internal/pages/models.go`](../../studio/internal/pages/models.go)、[`studio/internal/pages/models_picker.go`](../../studio/internal/pages/models_picker.go)、[`studio/internal/pages/models_run.go`](../../studio/internal/pages/models_run.go)，自动链在 [`studio/internal/pages/models_chain.go`](../../studio/internal/pages/models_chain.go)；下载页在 [`studio/internal/pages/downloads.go`](../../studio/internal/pages/downloads.go)、[`studio/internal/pages/models_downloads.go`](../../studio/internal/pages/models_downloads.go)；名称化展示封装在 [`studio/internal/pages/path_display.go`](../../studio/internal/pages/path_display.go)；视觉控件取自 onevra `components`（窗口外壳为 `shells/sidebar`），共享封装在 [`studio/internal/pages/components.go`](../../studio/internal/pages/components.go) 与表格 [`studio/internal/pages/table.go`](../../studio/internal/pages/table.go)；保留 MyGo 标题/`ui.Field`/布局原语；统一规范与组件映射见 [Studio Design System](../../studio/docs/design-system.md)） |
| 桌面通知 | 终态 run/download 与 `report.created` 的固定文案通知；投递失败不影响任务结果 | `Subscribe`、`messageFor` 在 [`studio/internal/desktop/notifications.go`](../../studio/internal/desktop/notifications.go) |

关键状态的事实源：设置持久化在 `os.UserConfigDir()/InferYard Studio/settings.json`（目录 0700、
文件 0600、原子替换；含四个路径与 `model_download_name`/`engine_download_name` 下载目录名，
非法目录名读取时回退默认值），损坏 JSON 整体回退默认值并记诊断；引擎状态机由 `engine_service.go` 独占
（`stopped`/`starting`/`running`/`stopping`/`failed`，另有启动核验期与遗留场景的 `orphaned` 展示态）；运行队列与历史索引常驻 `Service` 内存，
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
   取增量事件，六类事件触发全量刷新（`sync.reset`/`report.created`/`run.state`/`engine.state`/
   `download.state`/`device.updated`，见 [`studio/internal/pages/registry_refresh.go`](../../studio/internal/pages/registry_refresh.go)），其余做局部更新。
3. **Dashboard 阶段执行**：Dashboard 按就绪度与 `progress.v1` 推导下一条 CLI 命令
   （`nextCommand`/`baseCommand`），交给
   `WizardExecute`：`wizardArgs` 白名单校验 → `run`/`resume` 转入运行队列 → 其余命令探测 CLI
   后执行，`--out` 必须先不存在；`init` 成功后回写 workspace/results 设置，`report` 成功发布
   `report.created` 并可追加 `verify`。
4. **运行**：`StartRuns` 逐配置读取 bundle 与输出根并做包含/去重准入 → 入队由 `runQueue` 串行
   `executeRun` 派生 `inferyard run --config`（取消为 SIGINT + 两秒兜底）；执行循环每
   `ProgressInterval`（500ms）对比运行前后证据索引、tail `events.jsonl` 投影 case 进度与分数、
   聚合资源采样；终态经 `run.state` 事件发布。Dashboard 另经 `Service.Progress` 派生
   `inferyard progress --workspace` 解析 `progress.v1` 恢复工作区进度。运行实现与取消语义见
   [运行时执行与引擎适配](runtime-execution.md)。
5. **引擎服务**：Dashboard（绑定或运行阶段）/模型页 `StartEngine` → `Startup` 从冻结配置目录取 `[engine]` 启动行 →
   监听端口预检 → 拉起引擎子进程（stdout/stderr 写入 workspace `logs/engine-*.log`）→
   持久化 `engine-owner.json` 所有权记录（写失败即中止启动）→
   `monitorEngine` 每秒探测 `{endpoint}/health`，2xx 转 `running`；启动默认超时 10 分钟。
   `StopEngine` 仅在无活动运行时允许，SIGTERM 后 10 秒未退出则 kill，停止成功按记录身份移除所有权记录。
6. **候选准备链（模型页与 Dashboard 共用）**：模型页运行区「启动」与 Dashboard 候选阶段的
   「创建候选」复用同一条准备编排（`prepareCombination`）：所选模型 ≠ 最新 preflight 推荐时先重跑
   `device-check --model`（blocked 即停并呈现固定原因）→ 逐题包读冻结 `candidate.toml` 的
   `[model] local_path`/`[engine] binary_path` 做 canonical 比对，仅为不匹配的题包各执行一次
   `config create`（题包处于 `prepare_candidate` 且有 next_command 时复用该命令，否则按
   `config create` 的参数面组装 `--workspace`/`--bundle`/`--out`；引擎参数按平台注入 registry
   条目的同一资源：非 Windows `--engine` 二进制路径，Windows `--runtime-receipt` 回执路径，
   不拼引擎 argv）→ 复验全部待测题包
   候选匹配（不匹配报 `candidate_mismatch`）→ `StartEngine` → 武装绑定链。已封存
   （run_complete）题包不重建候选、不要求匹配新组合；全部题包已封存时回退用封存候选启动，
   但该候选必须匹配所选组合，否则报 `candidate_mismatch` 拒绝启动；任一步失败即停，不自动重试。
7. **绑定**：一键「启动并绑定」健康通过后由自动绑定链单次触发（armed 单次 + 校验进度阶段防竞态），
   按 progress 顺序对全部 `bound_missing` 题包依次执行 `config bind`（同引擎同 endpoint、显式 PID），
   全部成功才进 run 阶段，任一失败即停、显示 CLI 固定原因、不自动重试；外部服务从
   `service discover` 清单一键绑定（显式传发现的 PID 与 endpoint），折叠的「高级：手动绑定」保留
   `Processes` 进程枚举 + endpoint 手输兜底（`Processes` 只暴露 PID 与可执行名，argv 可能带凭据）。
   一切绑定都生成绑定配置并经 CLI 的 PID 与启动时间身份核验；discover 不预判 bind 可行性，
   `startup_arguments_mismatch` 等拒绝原因如实呈现；`BindingAlive` 仅提供存活提示。
8. **报告**：`CreateReport` 校验 1–100 个位于结果根内的不同 run → 派生 `inferyard report`
   写入结果根同级 `reports/` 下的新唯一目录 → 发布 `report.created`；页面 `Reports.Open` 以
   每报告一个原生 WebView 窗口打开 `report.html`（`file://`，同路径复用聚焦）。报告 ID 为 UTC
   `report-YYYYMMDDTHHMMSSZ[-N]`，命名冲突追加序号。报告与导出契约
   见 [评分、比较与报告](scoring-reporting.md) 与
   [report-contract](../../cli/docs/contracts/report-contract.md)。
9. **下载**：模型/引擎获取与运行队列并发、互不占位，共用 `downloading → done/failed/cancelled`
   任务列表与 `CancelDownload`。输出目录名默认取设置的下载目录名，占用（文件系统或历史任务记录，
   含终态）时自动追加 `-2`/`-3` 序号；调用方显式指定名字且冲突时仍返回 `output_exists`。
   `CreateDownload` 只接受无用户信息、查询
   与片段的 `https` 来源；轮询观测 `.part` 尺寸并以 5 读数窗口估算速率，正式发布文件出现后完成
   任务并按实测字节收尾。引擎获取（预置或自定义来源）经 `CreateEngineDownload` 进入同一列表，
   复用 `FetchEngine` 传输与解包：归档解包保留同级共享库、仅重建归档内相对
   符号链接（`safeLinkTarget` 拒绝越界链接），输出目录已存在或已被历史任务记录占用即拒绝
   （`output_exists`），取消贯穿传输与解包；该传输接口不提供字节读数，引擎任务的   `downloaded_bytes`/`bytes_per_second` 恒为 `null` 并带 `progress_reason`，成功收尾写
   `engine_path`。所有 `fetchURL` 传输超过 64 MiB 拒绝（`download_too_large`），不静默截断。
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
  共用同一任务列表与 `downloading → done/failed/cancelled` 转换；目录名默认来自设置并自动避让
  占用（追加序号），显式指定的目录名已存在或被历史任务记录（含终态）占用返回 `output_exists`，
  来源或目录名非法返回 `invalid_request`，服务关停中返回
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

- **资源清单刷新**：全量刷新（六类事件与显式刷新，清单见「页面刷新」条）重建清单；`download.progress` / `run.progress` 沿用已有清单，不重复遍历目录。
  事件丢失或 `sync.reset` 触发完整刷新，下载完成立即更新默认引擎。
- **目录名避让**：自动下载目录名与新绑定目录最多检查 1000 个候选；全部占用返回固定错误码
  `output_name_exhausted`，不覆盖旧目录。绑定链失败时优先以首个 limitation 的解释作为主消息。

- **引擎状态机与所有权**：`stopped → starting → running → stopping → stopped`；进程在
  `starting`/`running` 期自行退出判 `failed`（`engine_exited`），启动超时判 `failed`
  （`engine_start_timeout`），均不自动重启——Studio 只拥有并停止自己拉起的进程。异常退出遗留的
  引擎经 `engine-owner.json` 在下次启动检测：记录与进程 start ticks 双重核验，身份不匹配（PID
  复用）只移除记录、绝不发信号；身份匹配时提供显式停止按钮，停止的每一步都重核
  身份。损坏记录、身份无法核验或清理失败也保持 `orphaned`；显式「仅删除记录（不终止进程）」
  重新读取 `logs/engine-owner.json`，有效且身份匹配时拒绝删除，其他可恢复情形只删记录，成功后解除启动阻塞。
  正常引擎退出后的记录删除失败同样进入此恢复入口；启动前再次检查残留记录。owner 记录的写入与
  两类删除都在跨进程文件锁（`engine-owner.json.lock`，Unix flock / Windows LockFileEx）内执行，
  锁文件 inode 永久保留，防多实例并发覆盖对方的记录。端口占用返回 `engine_port_in_use`；有活动运行时
  `StopEngine` 返回 `run_active`；引擎或遗留服务 `stopping` 期间拒绝新运行（`engine_stopping`）；
  服务关停中一切准入返回 `service_closing`。
- **服务发现**：发现完全由 CLI `service discover` 执行（loopback 监听枚举 + 只读探测），Studio
  只解析结果并保留 limitations；CLI 缺失/过旧映射 `cli_outdated`，输出非法映射
  `service_discovery_invalid_response`，退出 4 映射 `service_discovery_error`；命令匹配的退出 2
  映射 `cli_blocked` 并保留 limitations，退出 130 映射 `cancelled`。发现结果不构成绑定资格，
  绑定终审始终在 CLI；同端口多监听进程标记 `ambiguous_listener`，不判唯一归属。
- **模拟不授予真机资格**：单元/页面测试用注入式 fixture executor，不启动真实模型服务或网络下载；
  `studio/tests/e2e` 边界套件驱动真实 CLI 与假引擎（见下文「验证」），证明的是边界与状态机行为，
  同样不构成真机验收；Go 单测与跨平台交叉编译不构成 Windows/Linux 真机验收，原生 UI 迁移需新的
  真机验收证据（见 [studio/README.md](../../studio/README.md) 的 Verification boundaries）。
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
  留在报告目录内；缺失来源 run 显示为不可用但不阻止打开。报告窗口注册加载失败与渲染进程退出
  回调后再加载，失败向主窗口回传固定错误并在子窗口提供重试/关闭；重试重新执行 `ReportFile`
  路径校验，不展示原始 WebView 错误描述。实现见
  [reports.go](../../studio/internal/pages/reports.go) 与
  [report_window.go](../../studio/internal/pages/report_window.go)，系统 WebView 回调仍待真机验证。
- **安全**：模型端点仅本地 loopback；token/凭据/完整 session URL 不进日志、设置或用户可见错误；
  向导执行不回传 stdout/stderr；设置文件 0700/0600 且仅在用户保存时替换。CLI 退出码语义
  （0/2/3/4/130）与呈现映射见 [CLI 与应用分发](cli-dispatch.md)。
- **缺测与单位**：未知测量为带原因的 `null`，不补零、不编百分比；内存以字节、速率以字节/秒
  存储，展示换算显式；资源曲线保留缺失采样。
- **采集节奏**：运行内进度事件以 500ms 间隔轮询生成并合并发布；资源历史保留 600 个采样点，
  CPU 差分要求进程/时钟身份一致、计数单调且采样间隔不超两秒，否则该点为 `null`。

## 视觉规格

全局视觉与交互规则统一维护在 [Studio Design System](../../studio/docs/design-system.md)，
覆盖六个主页面、运行详情及报告窗口外壳。该规范吸收模型页的三区布局（选择/运行/只读服务）、主题状态色、
内联详情和测量展示约定，并补齐表单、无障碍、缩放和验收要求。

组件与窗口基线为 onevra v0.2026.9（构建在 MyGo v0.4.8 之上）：主窗口用 `shells/sidebar.NewShell`
（可折叠可调宽侧栏、浮动 header、macOS titlebar 材质与 Windows Mica），主题用 `theme.Onevra`；
组件映射与保留例外见
[组件用法](../../studio/docs/design-system.md#onevra-组件映射)。

Dashboard 按 [`studio/docs/prototypes/dashboard-page.html`](../../studio/docs/prototypes/dashboard-page.html)
原型实施：结论卡置顶（六阶段步骤条 + 当前阶段徽标 + 阻断原因），阶段动作区使用步骤轨道组件
（编号圆点 + 纵向连接线，内容铺在页面背景上不用卡片，只渲染当前阶段），其后依次为快速运行、
任务队列与运行历史卡；运行详情为页内子视图。

共享标题、活动进度、换行按钮和一次性公告由 `pages/design_system.go` 承载；
字段提示由 `pages/form_validation.go` 承载。页面首次加载、刷新与失败分别呈现，刷新失败保留已有数据。
已实施项、回归入口与本轮验证边界见
[页面审查](../../studio/docs/design-system-audit.md)。当前整改进度见
[backlog](../../cli/docs/backlog.md#inferyard-studio-后续工作)。数据层 `null` 及原因保持原契约，
UI 文案按全局规范区分缺测、未知与未配置。

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

`go test ./...` 包含 `tests/e2e` 边界套件：真实 CLI 执行全部操作（init/device-check/bind/run/
report/verify），引擎为假引擎（测试二进制自身以 `--params-file` 再入），CLI 调用逐条记录为
transcript。判责规则：CLI 非零退出或 stdout 非法归 CLI；应答合法而 Studio 行为错误归 Studio；
工作区证据逐文档对照 `cli/schemas/` 校验，违规恒归 CLI（Studio 不写证据文件）。候选生产
（`config create`）是刻意的夹具边界；候选声明 `environment-admission.v2` 空 required_fields 以
跨平台可移植，观测差异仍全部落证据。运行旋钮：`INFERYARD_E2E_CLI` 指定 CLI、
`INFERYARD_E2E_ROOT` 保留现场。套件清单与布局见
[studio/tests/e2e/README.md](../../studio/tests/e2e/README.md)。CI 由
`.github/workflows/studio-check.yml` 在 macos-15 与 windows-2025 执行 vet 与该套件；
内部包单测暂仅在 macos-15 执行，Windows 单测的路径、权限位与时序假设仍待清理。

## 历史验证材料

`studio/docs/evidence/` 下的 `browser.*`、`sse.*`、`frontend-build.log` 等保留为旧 Web UI
阶段的原始诊断证据，不是当前构建说明，也不能证明当前原生 UI 已验收。
当前构建与验证以 [Studio README](../../studio/README.md) 为准。

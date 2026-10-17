# Studio 面板

InferYard Studio 是面向本地模型评测的桌面应用：onevra v0.2026.10（构建在 MyGo v0.5.4 之上）
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
| CLI 进程客户端 | `--version` 解析（版本与 `source=checkout\|release`）、argv 直通执行、退出码映射、取消（SIGINT 后两秒 SIGKILL 兜底）、`progress.v1` 解析 | `Client.Exec`、`ParseVersion`、`MapExitCode`、`ParseProgress`、`SplitCommand` 在 [`studio/internal/inferyard/client.go`](../../studio/internal/inferyard/client.go)、[`studio/internal/inferyard/progress.go`](../../studio/internal/inferyard/progress.go)、[`studio/internal/inferyard/command.go`](../../studio/internal/inferyard/command.go) |
| Dashboard 命令执行 | 预置向导命令的白名单参数校验（仅允许 `init`/`model acquire`/`engine acquire`/`device-check`/`config create`/`config bind`/`run`/`resume`/`report`/`verify` 及列名旗标）、`--out` 不存在性预检、执行后状态回写 | `WizardExecute`、`wizardArgs` 在 [`studio/internal/core/onboarding.go`](../../studio/internal/core/onboarding.go) |
| CLI 安装与预置资产 | 复用已有 `inferyard`/uv（含 Homebrew 路径），或下载固定版本独立 uv（sha256 校验）后 `uv tool install inferyard==Version`；内置 SmolLM2-135M 模型 URL 与按平台固定的 Prism 引擎归档下载/解包 | `InstallCLI`、`FindCLI`、`FindUV` 在 [`studio/internal/core/install.go`](../../studio/internal/core/install.go)；`DefaultModelURL`、`DefaultEngineURL`、`FetchEngine` 在 [`studio/internal/core/defaults.go`](../../studio/internal/core/defaults.go) |
| 就绪度与目录供给 | CLI/工作区/结果/模型四就绪度；workspace 初始化标志为 `preparation.json` 的 `community_init.v1`；`Provision` 显式幂等建目录 | `Readiness`、`Provision`、`workspaceRoot` 在 [`studio/internal/core/readiness.go`](../../studio/internal/core/readiness.go) |
| 引擎服务生命周期 | 解析冻结配置 `candidate.toml` 的 `[engine]` 启动行、端口占用预检、子进程拉起、健康端点等待、停止与终止；拉起成功即向 workspace `logs/engine-owner.json` 持久化所有权记录（PID + start ticks + endpoint + 日志/候选基名，不含 argv），正常停止时按记录身份原子移除 | `StartEngine`、`StopEngine`、`monitorEngine`、`terminateEngine` 在 [`studio/internal/core/engine_service.go`](../../studio/internal/core/engine_service.go)；所有权读写与启动核验在 [`studio/internal/core/engine_owner.go`](../../studio/internal/core/engine_owner.go)；`Startup`、`Processes` 在 [`studio/internal/core/startup.go`](../../studio/internal/core/startup.go) |
| 遗留引擎核验与清理 | 启动时读 `engine-owner.json`：记录损坏/非法或身份无法核验时在任务页提供「仅删除记录（不终止进程）」入口，删除前重读记录，身份匹配则拒绝并要求停止；PID 已退出或 start ticks 不匹配（PID 复用）只移除记录；身份匹配时提供显式「停止遗留服务」按钮，停止前与停止中逐次重核身份，SIGTERM 超时升级 kill，任何一步不确定都不发信号 | `DiscardOrphanRecord` 在 [`studio/internal/core/engine_owner_recovery.go`](../../studio/internal/core/engine_owner_recovery.go)；`detectEngineOwner`、`StopOrphanEngine`、`orphanIdentity`、`stopOrphan` 在 [`studio/internal/core/engine_owner.go`](../../studio/internal/core/engine_owner.go)、[`studio/internal/core/engine_orphan.go`](../../studio/internal/core/engine_orphan.go) |
| 本机服务发现 | 调用 CLI `service discover` 并解析（枚举与探测全部在 CLI 侧，Studio 不新增端口/进程枚举代码）；CLI 缺失或过旧返回 `cli_outdated`；外部服务一键绑定显式传 discover 返回的 PID 与 endpoint，仍走 CLI 绑定核验 | `Discover` 在 [`studio/internal/core/discovery.go`](../../studio/internal/core/discovery.go)；解析在 [`studio/internal/inferyard/discovery.go`](../../studio/internal/inferyard/discovery.go)；契约见 [CLI 接口](../../cli/docs/cli-surface.md#本机服务发现契约) |
| 引擎核验 | 调用 CLI `engine check`（macOS 传 `--engine`，Windows 传 `--runtime-receipt`，回执按 CLI 产物布局在引擎二进制同目录或其上级目录查找）核验清单引擎；缓存键含路径 + mtime + 同目录 dylib 集合指纹（含符号链接条目与目标状态）+ receipt 大小与修改时间；核验任务绑定发起时的 CLI 路径与 generation，CLI 切换后过期结果不写回；有界并发与超时；瞬态失败 60s 退避；CLI 过旧降级为「未核验」且不禁选；触发时机为事件驱动刷新（页面可见时）与「模型」页导航点击进入 | `CheckEngine`、`VerifyEngines` 在 [`studio/internal/core/engine_check.go`](../../studio/internal/core/engine_check.go)；解析在 [`studio/internal/inferyard/engine_check.go`](../../studio/internal/inferyard/engine_check.go)，契约黄金样本（合法/非法）在 [`studio/internal/inferyard/testdata/engine_check_contract.json`](../../studio/internal/inferyard/testdata/engine_check_contract.json)，CLI 侧 schema 校验同一组样本；契约见 [CLI 接口](../../cli/docs/cli-surface.md#引擎核验契约) |
| 清单注册表 | 聚合模型清单（设备快照 + 模型下载任务）与引擎清单（`EngineRoots` 双根遍历，嵌套深度 ≤4、可执行位 + 引擎下载任务含 plain-file 记录）；只输出名称与展示信息，同名附加来源标记、再冲突加路径哈希前缀；路径留在 core 内部按名称映射回真实路径 | `Registry`、`resourcePath`、`scanEngineBinaries` 在 [`studio/internal/core/registry.go`](../../studio/internal/core/registry.go) |
| 绑定存活提示 | 只读比对绑定配置里的 `server_pid`/`process_start_ticks` 与进程实际启动 tick，仅作 UI 提示；比对失败显示「无法确认」，不据此判定绑定有效 | `BindingAlive` 在 [`studio/internal/core/binding.go`](../../studio/internal/core/binding.go) |
| 运行队列 | 绑定配置准入（含结果根包含、去重）、串行 worker、取消、resume 准入（历史索引 + CLI 终审） | `StartRuns`、`Resume`、`CancelRun`、`runQueue` 在 [`studio/internal/core/runs.go`](../../studio/internal/core/runs.go) |
| 下载任务 | 与运行队列并发的模型/引擎获取，共用同一任务列表；输出目录名取自设置的 `model_download_name`/`engine_download_name`（默认 `model`/`engine`，仅允许字母数字点下划线连字符且不能全为点），占用时自动追加 `-2`/`-3` 序号，任务详情展示实际目录名；模型任务 `.part` 尺寸轮询与正式文件发布观测；引擎任务复用 `FetchEngine` 传输与解包、取消贯穿全程、字节与速率缺测为带 `progress_reason` 的 `null`、成功写 `engine_path` | `CreateDownload`、`CreateEngineDownload`、`CancelDownload` 在 [`studio/internal/core/downloads.go`](../../studio/internal/core/downloads.go)；目录名解析在 [`studio/internal/core/download_names.go`](../../studio/internal/core/download_names.go)；`FetchEngine` 在 [`studio/internal/core/defaults.go`](../../studio/internal/core/defaults.go)；模型下载采样在 `Service.download` 中与任务生命周期合并 |
| 报告 | 1–100 个来源 run 校验、唯一命名新目录生成、索引与静态路径包含解析；主窗口内呈现索引中的 dashboard 与请求行 | `CreateReport`、`Reports`、`ReportFile` 在 [`studio/internal/core/reports.go`](../../studio/internal/core/reports.go)；`reportView` 在 [`studio/internal/pages/report_view.go`](../../studio/internal/pages/report_view.go) |
| 进程内事件 | 2048 条回放环 + 唤醒订阅；慢消费者按游标重建而非丢事件 | `EventBus`、`Publish`、`Subscribe`、`After`、`ReplayCapacity` 在 [`studio/internal/core/events.go`](../../studio/internal/core/events.go) |
| 证据索引与读取 | 结果根扫描（损坏条目跳过并计数）、JSONL 增量 tail、host dirty 标记判读、状态优先级 | `Index` 在 [`studio/internal/evidence/index.go`](../../studio/internal/evidence/index.go)；`Tail`、`Cursor` 在 [`studio/internal/evidence/tail.go`](../../studio/internal/evidence/tail.go)；`DirtyRun`、`RunStatus` 在 [`studio/internal/evidence/dirty.go`](../../studio/internal/evidence/dirty.go) |
| 历史聚合 | evidence 记录 → run 汇总/进度/资源投影；resume 资格标记；原子重建历史索引 | `summarize`、`scan`、`markResumable`、`History`、`rebuild` 在 [`studio/internal/core/aggregate.go`](../../studio/internal/core/aggregate.go) |
| 页面 | 单主窗口在工作区、结果目录和模型目录三个路径均未写入设置时，空启动打开任务页的环境准备；任一路径已保存后，空启动打开报告。环境未齐时 Dashboard 只显示环境准备卡，不画六段进度。侧栏顺序为报告/模型/下载/设备/设置，上方「新任务」打开 Dashboard（无导航项选中），保留六个页内页面；右上角固定状态行显示引擎与队列，空闲为「引擎 已停止 · 队列 空」，busy/refreshing 为「执行中」，引擎 starting/running 且无非终态运行、无 busy 时显示「停止」；页面采用事件驱动刷新，Dashboard 推导下一步命令；「设备」页仅展示硬件快照与行内缺测原因，不执行工作区 next_command，不提供数据来源明细；「刷新设备」调用 RefreshDevice，以 device-check --json --models-root 采集快照，不传 --out、不写工作区预检证据；「模型」页按选择区（先引擎后模型，标注核验状态）、组合状态区（状态归因与「用这个组合新开任务」，复用「新任务」入口并预填组合；服务或测评运行期间组合显示为文字；空清单仅有「到下载页」按钮；遗留引擎仅警告，不提供恢复或组合新任务按钮）、只读的本机运行服务卡三区组织，Dashboard 候选阶段的模型/引擎下拉与模型页选择框是同一选择的同步视图（候选阶段引擎选择为名称下拉，路径映射在 core 内完成，选择为空时默认采纳清单中首个已核验可用的引擎（清单排序：最新完成下载优先，其次最近修改））；「下载」页仅在有任务时显示清单，引擎任务在模型任务之前；新建下载在同一卡内先展示推荐 llama-server 引擎，再展示 SmolLM2-135M-Instruct-Q4_K_M 模型，URL 预填且可编辑，非 HTTPS 在本页拒绝；凭证折叠在「需要凭证时」，仅接受环境变量名；全页面名称化展示（模型名/引擎名/目录基名/bundle 名/日志文件名），完整路径仅设置页 | `New`、`navigation`、`viewState` 在 [`studio/internal/pages/window.go`](../../studio/internal/pages/window.go)、[`studio/internal/pages/dashboard_actions.go`](../../studio/internal/pages/dashboard_actions.go)；模型页三区在 [`studio/internal/pages/models.go`](../../studio/internal/pages/models.go)、[`studio/internal/pages/models_picker.go`](../../studio/internal/pages/models_picker.go)、[`studio/internal/pages/models_run.go`](../../studio/internal/pages/models_run.go)，自动链在 [`studio/internal/pages/models_chain.go`](../../studio/internal/pages/models_chain.go)；下载页在 [`studio/internal/pages/downloads.go`](../../studio/internal/pages/downloads.go)、[`studio/internal/pages/models_downloads.go`](../../studio/internal/pages/models_downloads.go)；名称化展示封装在 [`studio/internal/pages/path_display.go`](../../studio/internal/pages/path_display.go)；视觉控件取自 onevra `components`（窗口外壳为 `shells/sidebar`），共享封装在 [`studio/internal/pages/components.go`](../../studio/internal/pages/components.go) 与表格 [`studio/internal/pages/table.go`](../../studio/internal/pages/table.go)；保留 MyGo 标题/`ui.Field`/布局原语；统一规范与组件映射见 [Studio Design System](../../studio/docs/design-system.md)） |
| 桌面通知 | 终态 run/download 与 `report.created` 的固定文案通知；投递失败不影响任务结果 | `Subscribe`、`messageFor` 在 [`studio/internal/desktop/notifications.go`](../../studio/internal/desktop/notifications.go) |

关键状态的事实源：设置持久化在 `os.UserConfigDir()/InferYard Studio/settings.json`（目录 0700、
文件 0600、原子替换；含四个路径与 `model_download_name`/`engine_download_name` 下载目录名，
非法目录名读取时回退默认值），损坏 JSON 整体回退默认值并记诊断；引擎状态机由 `engine_service.go` 独占
（`stopped`/`starting`/`running`/`stopping`/`failed`，另有启动核验期与遗留场景的 `orphaned` 展示态）；运行队列与历史索引常驻 `Service` 内存，
证据目录才是完成性的唯一事实源，内存任务表不随重启存活。版本号唯一来源是
[`studio/mygo.config.ts`](../../studio/mygo.config.ts)，由 `main` 经 `go:embed` 读取。侧栏不再显示进程来源；`studioOrigin()` 仍区分 `go-build` 路径的「Studio 源码运行」和构建产物的「Studio 发行包」，并附 `os.Executable()` 路径，供 `TestStudioOriginNamesPackagedBuild` 测试使用。环境卡的 CLI 行显示 `checkout`（源码树）、`release`（发行安装）或未标明来源的旧 CLI、包版本和探测到的 CLI 路径；`source` 只说明安装方式，不包含 git 提交。自动安装保留已配置且存在的 CLI，缺失时才查找或安装。
`InstallCLI` 固定的 Python（`installPython = "3.14.7"`）与独立 uv（`uvRelease`）版本必须与
[cli/docs/installation.md](../../cli/docs/installation.md) 保持同步（源码注释的维护义务）。

## 运行路径

1. **启动**：`main` 先解析启动参数；`--help` 输出说明并退出，非法参数退出 2。只有开发标签
   `ui_debug` 且无 `release` 时注册 `--debug-ui` 并允许进入下述独立预览路径。
   普通启动解析嵌入配置设置 `core.Version`，用 `signal.NotifyContext` 建应用上下文并
   `core.New` 创建服务（读设置、启动串行 runQueue、按需 `RefreshDevice`），随后 `mygo.App.Run`
   进入 WhenReady：注册桌面通知订阅、创建 `pages.Reports` 与主窗口 `pages.New`。SIGINT/SIGTERM
   触发 `mygo.App.Quit` 与 `Service.Shutdown`。
2. **页面刷新**：主窗口的六页面共用一个 `viewState`；后台 goroutine 订阅 `EventBus`，按游标
   取增量事件，六类事件触发全量刷新（`sync.reset`/`report.created`/`run.state`/`engine.state`/
   `download.state`/`device.updated`，见 [`studio/internal/pages/registry_refresh.go`](../../studio/internal/pages/registry_refresh.go)），其余做局部更新。
3. **Dashboard 任务执行**：准备环境按最新 readiness 依次补 CLI、结果/模型目录、工作区；已初始化就跳过 `init`，新工作区不传 `--bundle`。首次 workspace device-check 自动执行一次，保留 `--models-root`、`ModelName` 为空。新任务复用模型/引擎选择，默认勾选存在的 zh-core，按勾选题包创建新候选并自动启动、绑定和运行；活动阶段与本次返回的 run 路径归任务内存所有，未选题包不阻挡。绑定使用独立取消上下文；新任务取消绑定和所有非终态运行并等待结束，下载不受影响。所选新 run 封存后执行 `report --runs`，`ReportVerify=false`，完成区域仅显示「查看报告」，直接打开本次输出目录对应的报告；不展示路径、生成成功文字或已核验状态，核验留在报告页。没有活动任务时从 `progress.v1` 恢复。所有命令仍经 `WizardExecute` 原白名单和身份检查。
4. **运行**：`StartRuns` 逐配置读取 bundle 与输出根并做包含/去重准入 → 入队由 `runQueue` 串行
   `executeRun` 派生 `inferyard run --config`（取消为 SIGINT + 两秒兜底）；执行循环每
   `ProgressInterval`（500ms）对比运行前后证据索引、tail `events.jsonl` 投影 case 进度与分数、
   聚合资源采样；终态经 `run.state` 事件发布。Dashboard 另经 `Service.Progress` 派生
   `inferyard progress --workspace` 解析 `progress.v1` 恢复工作区进度。运行实现与取消语义见
   [运行时执行与引擎适配](runtime-execution.md)。
   `stageView` 在有非终态运行或活动任务处于未失败的 `run` 阶段时收起正文模型服务卡，
   题包间隙不重新展开；`cornerStatus` 保留引擎与队列状态，不受后台刷新或 busy 覆盖。
   无非终态运行且任务失败或回到工作区进度时，run/resume 阶段恢复服务操作；绑定阶段仍显示启动与绑定操作。
5. **引擎服务**：Dashboard（绑定或运行阶段）`StartEngine` → `Startup` 从冻结配置目录取 `[engine]` 启动行 →
   监听端口预检 → 拉起引擎子进程（stdout/stderr 写入 workspace `logs/engine-*.log`）→
   持久化 `engine-owner.json` 所有权记录（写失败即中止启动）→
   `monitorEngine` 每秒探测 `{endpoint}/health`，2xx 转 `running`；启动默认超时 10 分钟。
   `StopEngine` 仅在无活动运行时允许，SIGTERM 后 10 秒未退出则 kill，停止成功按记录身份移除所有权记录。
6. **候选准备链**：Dashboard 候选阶段的「运行 benchmark」调用准备编排（`prepareCombination`）：所选模型 ≠ 最新 preflight 推荐时先重跑
   `device-check --model`（blocked 即停并呈现固定原因）→ 逐题包读冻结 `candidate.toml` 的
   `[model] local_path`/`[engine] binary_path` 做 canonical 比对，仅为不匹配的题包各执行一次
   `config create`（题包处于 `prepare_candidate` 且有 next_command 时复用该命令，否则按
   `config create` 的参数面组装 `--workspace`/`--bundle`/`--out`；引擎参数按平台注入 registry
   条目的同一资源：非 Windows `--engine` 二进制路径，Windows `--runtime-receipt` 回执路径，
   不拼引擎 argv）→ 复验全部待测题包
   候选匹配（不匹配报 `candidate_mismatch`）→ `StartEngine` → 武装绑定链。新任务强制为所选题包重建候选；任一步失败即停，不自动重试。
7. **绑定**：任务在健康通过后自动绑定所选题包；既有启动链由 armed 单次触发，
   按 progress 顺序对目标 `bound_missing` 题包依次执行 `config bind`（同引擎同 endpoint、显式 PID），
   全部成功才进 run 阶段，任一失败即停、显示 CLI 固定原因、不自动重试；外部服务从
   `service discover` 清单一键绑定（显式传发现的 PID 与 endpoint），绑定失败后折叠的「高级：手动绑定」保留
   `Processes` 进程枚举 + endpoint 手输兜底（`Processes` 只暴露 PID 与可执行名，argv 可能带凭据）。
   `run_missing` 与 single 的 `run --config` 在新 PID 下重新绑定；`resume --from-run` 直接传 PID 与 endpoint，不要求预绑定。duration 与未完成 batch 的 `run --plan` 仅显示阻断原因，不放宽执行白名单。
   一切绑定都生成绑定配置并经 CLI 的 PID 与启动时间身份核验；discover 不预判 bind 可行性，
   `startup_arguments_mismatch` 等拒绝原因如实呈现；`BindingAlive` 仅提供存活提示。
8. **报告**：页面先展示「历史任务」，再展示「报告列表」。进入任务清除详情与续测来源并聚焦所选运行；续测清除详情并设置来源；实时详情打开对应任务，三者都进入 Dashboard。打开报告为主操作；1–100 个来源的生成入口折叠在「从历史运行另做报告」。仅工作区 `next_action=verify` 且报告未核验时显示「核验」，沿用向导并设置 `ReportVerify=true`；`verified` 显示「已核验」。
   `CreateReport` 校验 1–100 个位于结果根内的不同 run → 派生 `inferyard report`
   写入结果根同级 `reports/` 下的新唯一目录 → 发布 `report.created`；页面在主窗口内容区读取 `index.json`，呈现当前所选运行的指标卡、分类表、模型与机器、逐题答案及数据来源。
   指标取自 `runs[].dashboard` 与 `summary.completion_rate`；模型、引擎和机器取自 `profile`、`threads`，题包取自 `facets.bundle_label`，运行 ID 取自 `source.run_id`，逐题行取自 `requests`。
   多运行索引用选择器分别显示，不合并指标。质量和完成率保留分母与排除数；缺测显示 — 并保留已有采样原因。延迟从 ms 转为 s，内存从 bytes 转为 GiB；诊断运行标注「诊断运行」。
   `ReportIndex` 接受列表 ID 或本次报告绝对目录；绝对目录先规范化并验证位于配置的 reports 根目录内，再经 `ReportFile` 校验索引，越界路径不能按同名目录打开。报告 ID 为 UTC
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

## UI 调试预览

预览源文件、嵌入 fixture 与对应测试使用 `ui_debug && !release` 构建约束。普通构建
不注册 `--debug-ui`，帮助不列出它，显式传入时退出 2。`build-all.sh` 固定 `-tags release`，
覆盖环境传入的标签；`package.py` 在写入分发目录前读取全部二进制的 Go build info，
拒绝缺少 `release` 标签的产物。`release` 与 `ui_debug` 同时存在时仍排除预览。

`--debug-ui` 在 `core.New` 之前分流至 `pages.NewPreview`，只创建原生窗口及内存
`previewSession`，不创建真实服务、运行队列、事件订阅或桌面通知，不读取用户设置与工作区证据。
页面通过未导出的 `previewMode` 接口访问预览；实现仅存在于开发构建，正式模式保持空值。
普通模式与预览共用 `pageShellOptions` 和真实页面构建器；预览窗口的 `StateKey` 为空，
不保存窗口和侧栏布局。标题与侧栏持续标注模拟数据。

[`preview.go`](../../studio/internal/pages/preview.go) 管理场景、重置和操作提示；
[`preview_fixtures.go`](../../studio/internal/pages/preview_fixtures.go) 提供固定 DTO 与嵌入报告索引。
切换或重置场景创建新的 `viewState`，并更换页面子树 key，使字段、表格选择与 `ui.Local`
折叠状态一起复位。新任务只取消当前任务并返回准备阶段，保留设置、下载与历史；
显示的路径、PID、采样与进度不代表真实资源。

页面在纯表单校验之后、真实 IO 或异步上下文创建之前派发 `previewIntent`，参数携带
本次模型、题包、下载来源、任务 ID 或设置 patch，不根据按钮文案猜测操作。
[`preview_operations.go`](../../studio/internal/pages/preview_operations.go) 分派内存操作；
任务、下载与报告分别由 `preview_benchmark.go`、`preview_downloads.go`、`preview_reports.go` 推进。
任务冻结所选模型、引擎和题包，绑定后一次加入全部选中题包，串行运行；等待项与真实队列一样
使用 `State=starting, Queued=true`。取消同时结束运行项与等待项，未启动项没有运行路径或历史证据。
全部题包完成后生成独立报告快照；已打开的详情随进度刷新。
下载完成更新模型/引擎清单，重试分配新任务和目录名；设置只更新本次会话。

[`preview_schedule.go`](../../studio/internal/pages/preview_schedule.go) 按到期时间、同时间插入顺序
推进事件。原生窗口的 ticker 只经 `win.Update` 修改状态；关闭窗口取消上下文，场景重置清空
事件并递增 generation，旧场景事件不能回写。任务取消保留短暂收尾状态，下载按任务 ID 独立取消。
测试直接调用 `advance` 推进模拟时间，不等待真实时间。一次性失败在操作被接受时绑定到该操作，
表单校验失败不消耗它，取消不受故障注入影响。

`work` 保留禁止执行真实闭包的兜底；未接入动作显示明确错误，不能伪装成功。
新增页面操作须覆盖这些边界，不能以注入假 executor 代替服务隔离。启动方法与场景清单见
[Studio README](../../studio/README.md#ui-debug-preview)。

`preview_test.go` 覆盖全部场景、导航、隔离、重置，以及 800/1120 宽、文字缩放 1/2、浅深主题布局；
`preview_interaction_test.go` 用页面按钮覆盖准备、双题包运行、动态详情、报告、取消收尾、
失败重试、并行下载、设置生效和过期事件。macOS 原生窗口已用页面按钮完成准备环境、
模拟下载并选择模型、双题包运行、动态详情与打开本次报告。模拟检查不证明真实模型流程或 Windows/Linux 原生行为。

[`tests/ui`](../../studio/tests/ui/README.md) 使用 XCUITest 构建实际 Go 入口并启动独立
`--debug-ui` macOS 窗口。测试只按可访问控件操作和断言，不读取 `viewState`、推进测试时钟或用
场景下拉完成流程；由真实窗口 ticker 推进模拟任务。每个用例重新启动模拟应用，结果写入新的
`build/ui-e2e/run-*`，失败保留截图与辅助功能树。根级 `mise run test:studio-ui` 和 macOS CI
调用同一入口；此层与 `ui.NewTester` 内存回归、真实 CLI／假引擎边界测试分别报告。

[`preview_parity_test.go`](../../studio/internal/pages/preview_parity_test.go) 对正常编排与模拟器运行同一组
场景，并将双方各自与明确预期比较：core 单题包、pelican 单题包、双题包、启动失败后手动重试、
绑定中取消、运行中同时取消活动项和等待项。观察包括阶段标题、键盘可达按钮、错误码、串行队列
状态和报告包含的题包，报告不得混入已有的无关运行。检查不比较随机路径、PID、耗时或模拟分数。

正常侧调用实际 `runBenchmarkFlow` / `cancelBenchmarkRuns`，在 `benchmarkBackend` 边界注入
受控服务响应；模拟侧调用页面动作与内存调度器。起点是候选已准备好，取消比较终态与无报告，
不覆盖正常窗口的异步重置过程。该层不启动 `core.Service`、CLI 或模型，不证明环境安装、下载、
真实事件刷新或全部故障分支一致。已有 macOS CI 的 `ui_debug` 页面测试自动包含它。单独运行：

```sh
cd studio
mise exec -- go test -tags ui_debug ./internal/pages -run TestPreviewBenchmarkParity -count=1
```

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
  Dashboard 与模型页从 `EngineState` 独立展示 Studio 启动的 `starting`/`running`/`stopping`
  服务，扫描失败、未完成或 `models` 缺测均不影响该条目；候选名称取状态中的基名，不使用当前选择
  冒充运行模型。`orphaned` 显示为「遗留服务记录（状态未确认）」，`failed`/`stopped` 不列为活动服务。
  `engine.state` 事件沿用页面刷新路径；显式刷新服务列表也重读 `Service.Engine()`。
  其他 `models != null` 端点放入默认折叠的「待确认的兼容接口」，提示可能是代理、目录不证明本机
  已加载模型，模型目录另行折叠。只有 Dashboard 提供显式绑定，模型页只读。
  展示去重要求活动生命周期、相同正 PID、完全相同 endpoint 且无监听歧义；仅隐藏重复条目，
  不为发现端点授予受管身份。未知端点、终态及遗留记录不参与去重。
  展示实现在 [`service_discovery.go`](../../studio/internal/pages/service_discovery.go)。
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
  留在报告目录内；缺失来源 run 显示为不可用但不阻止打开。单项索引不可读不影响其他报告，
  列表标记无法读取，打开后可重新读取或返回列表。质量与类别分母直接取自索引，不重算分数；
  空延迟显示 —。CLI 仍生成 `report.html`，Studio 没有加载 HTML 的控件。实现见
  [report_view.go](../../studio/internal/pages/report_view.go)、[report_metrics.go](../../studio/internal/pages/report_metrics.go) 与 [report_tables.go](../../studio/internal/pages/report_tables.go)。macOS 模拟数据的直达、布局与滚动已检查；其他平台和真实测评数据未在本轮验证。
- **安全**：模型端点仅本地 loopback；token/凭据/完整 session URL 不进日志、设置或用户可见错误；
  向导执行不回传 stdout/stderr；设置文件 0700/0600 且仅在用户保存时替换。CLI 退出码语义
  （0/2/3/4/130）与呈现映射见 [CLI 与应用分发](cli-dispatch.md)。
- **缺测与单位**：未知测量为带原因的 `null`，不补零、不编百分比；内存以字节、速率以字节/秒
  存储，展示换算显式；资源曲线保留缺失采样。
- **采集节奏**：运行内进度事件以 500ms 间隔轮询生成并合并发布；资源历史保留 600 个采样点，
  CPU 差分要求进程/时钟身份一致、计数单调且采样间隔不超两秒，否则该点为 `null`。

## 视觉规格

全局视觉与交互规则统一维护在 [Studio Design System](../../studio/docs/design-system.md)，
覆盖六个主页面、运行详情及报告视图。该规范吸收模型页的三区布局（选择/组合状态/只读服务）、主题状态色、
内联详情和测量展示约定，并补齐表单、无障碍、缩放和验收要求。

组件与窗口基线为 onevra v0.2026.10（构建在 MyGo v0.5.4 之上）：主窗口用 `shells/sidebar.NewShell`
（可折叠可调宽侧栏、浮动 header、macOS titlebar 材质与 Windows Mica），主题用 `theme.Onevra`；
组件映射与保留例外见
[组件用法](../../studio/docs/design-system.md#onevra-组件映射)。

Dashboard 按 [`studio/docs/prototypes/dashboard-page.html`](../../studio/docs/prototypes/dashboard-page.html)
原型实施：结论卡置顶（六阶段步骤条 + 当前阶段徽标 + 阻断原因），阶段动作区使用步骤轨道组件
（编号圆点 + 纵向连接线，内容铺在页面背景上不用卡片，只渲染当前阶段），移除运行历史、快速运行与任务队列卡；运行详情为页内子视图，通过「返回运行」回到任务。

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

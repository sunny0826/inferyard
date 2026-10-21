# Studio 面板

InferYard Studio 是面向本地模型评测的桌面应用：onevra v0.2026.14（构建在 MyGo v0.6.2 之上）
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

配套源码构建、隔离开发配置与重建行为见[Studio 源码开发环境](studio-development.md)。

## 职责与事实源

正文见[职责与事实源](studio-panel/responsibilities.md).

## 运行路径

正文见[运行路径](studio-panel/runtime.md).

## 本地五引擎

登记、模型格式、逐题包服务替换、LM 外部绑定与重试报告边界见[本地引擎参考](studio-panel/local-engines.md)。

## UI 调试预览

正文见[UI 调试预览](studio-panel/preview.md).

## 关键状态转换

正文见[关键状态转换](studio-panel/preview.md).

### 引擎条目（`EngineState.State`）

正文见[关键状态转换](studio-panel/preview.md).

### 运行任务（`RunTask`）

正文见[关键状态转换](studio-panel/preview.md).

### 下载任务（`DownloadTask`）

正文见[关键状态转换](studio-panel/preview.md).

### 历史 run 状态（投影，非新状态机）

正文见[关键状态转换](studio-panel/preview.md).

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
  本地进程组记录身份无法核验时，即使显式仅删记录也拒绝，以保留待核验 worker 的归属；
  具体边界见[本地引擎参考](studio-panel/local-engines.md#串行执行与所有权)。
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
  任务页与模型页从 `EngineState` 独立展示 Studio 启动的 `starting`/`running`/`stopping`
  服务，扫描失败、未完成或 `models` 缺测均不影响该条目；候选名称取状态中的基名，不使用当前选择
  冒充运行模型。`orphaned` 显示为「遗留服务记录（状态未确认）」，`failed`/`stopped` 不列为活动服务。
  `engine.state` 事件沿用页面刷新路径；显式刷新服务列表也重读 `Service.Engine()`。
  其他 `models != null` 端点放入默认折叠的「待确认的兼容接口」，提示可能是代理、目录不证明本机
  已加载模型，模型目录另行折叠。只有任务页提供显式绑定，模型页只读。
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
- **采集节奏**：运行内进度事件以 500ms 间隔轮询生成并合并发布；资源历史按来源各保留最近
  600 个观测。CPU、温度与来源切换规则见[资源投影](studio-panel/runtime.md#资源投影)。

## 视觉规格

全局视觉与交互规则统一维护在 [Studio Design System](../../studio/docs/design-system.md)，
覆盖六个主页面、运行详情及报告视图。该规范吸收模型页的三区布局（选择/组合状态/只读服务）、主题状态色、
内联详情和测量展示约定，并补齐表单、无障碍、缩放和验收要求。

组件与窗口基线为 onevra v0.2026.14（构建在 MyGo v0.6.2 之上）：主窗口用 `shells/sidebar.NewShell`
（可折叠可调宽侧栏、浮动 header、macOS titlebar 材质与 Windows Mica），主题用 `theme.Onevra`；
组件映射与保留例外见
[组件用法](../../studio/docs/design-system.md#onevra-组件映射)。

任务页按 [`studio/docs/prototypes/dashboard-page.html`](../../studio/docs/prototypes/dashboard-page.html)
原型实施：结论卡置顶（六阶段步骤条 + 当前阶段徽标 + 阻断原因），阶段动作区使用步骤轨道组件
（编号圆点 + 纵向连接线，内容铺在页面背景上不用卡片，只渲染当前阶段），移除运行历史、快速运行与任务队列卡；运行详情为页内子视图，通过「返回运行」回到任务。

共享标题、活动进度、换行按钮和一次性公告由 `pages/design_system.go` 承载；
字段提示由 `pages/form_validation.go` 承载。成功 Toast 与一次性公告在
`pages/window.go` 的 `contentView` 中消费，主窗口、预览窗口与内存测试视图共用这一条路径，
因此三者的反馈行为一致。Toast 位置由 `pages/window_shell.go` 的 `pageShellOptions` 显式
固定为 `components.ToastTop`（顶部居中），不依赖 onevra 的默认常量。
页面首次加载、刷新与失败分别呈现，刷新失败保留已有数据。
已实施项、回归入口与本轮验证边界见
[页面审查](../../studio/docs/design-system-audit.md)。当前整改进度见
[backlog](../../cli/docs/backlog.md#inferyard-studio-后续工作)。数据层 `null` 及原因保持原契约，
UI 文案按全局规范区分缺测、未知与未配置。

## 验证

在仓库的 `studio/` 目录内执行（环境变量仅作用于对应命令，见
[studio/README.md](../../studio/README.md)）：

```sh
cd studio
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go vet ./...
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -o build/inferyard-studio .
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct sh scripts/build-all.sh
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

## 长上下文专项

正式调用链、持久记录、只读结果及本轮软件检查边界见[专项参考](studio-panel/context-speed.md)。

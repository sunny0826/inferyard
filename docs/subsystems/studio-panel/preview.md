# preview

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

[`preview.go`](../../../studio/internal/pages/preview.go) 管理场景、重置和操作提示；
[`preview_fixtures.go`](../../../studio/internal/pages/preview_fixtures.go) 提供固定 DTO 与嵌入报告索引。
切换或重置场景创建新的 `viewState`，并更换页面子树 key，使字段、表格选择与 `ui.Local`
折叠状态一起复位。新任务只取消当前任务并返回准备阶段，保留设置、下载与历史；
显示的路径、PID、采样与进度不代表真实资源。
场景下拉使用限高滚动列表，长清单不延伸到窗口外；选择末尾场景须先滚动到对应选项。

页面在纯表单校验之后、真实 IO 或异步上下文创建之前派发 `previewIntent`，参数携带
本次模型、题包、下载来源、任务 ID 或设置 patch，不根据按钮文案猜测操作。
[`preview_operations.go`](../../../studio/internal/pages/preview_operations.go) 分派内存操作；
任务、下载与报告分别由 `preview_benchmark.go`、`preview_downloads.go`、`preview_reports.go` 推进。
任务冻结所选模型、引擎和题包，绑定后一次加入全部选中题包，串行运行；等待项与真实队列一样
使用 `State=starting, Queued=true`。取消同时结束运行项与等待项，未启动项没有运行路径或历史证据。
全部题包完成后生成独立报告快照；已打开的详情随进度刷新。
下载完成更新模型/引擎清单，重试分配新任务和目录名；设置只更新本次会话。
「首次使用 · 无资源」从未配置环境和空资源清单开始，共用正式资源引导与设备检查准入；
`preview_first_use_test.go` 通过页面按钮完成准备、双资源下载、返回任务、双题包运行和报告。

[`preview_schedule.go`](../../../studio/internal/pages/preview_schedule.go) 按到期时间、同时间插入顺序
推进事件。原生窗口的 ticker 只经 `win.Update` 修改状态；关闭窗口取消上下文，场景重置清空
事件并递增 generation，旧场景事件不能回写。任务取消保留短暂收尾状态，下载按任务 ID 独立取消。
测试直接调用 `advance` 推进模拟时间，不等待真实时间。一次性失败在操作被接受时绑定到该操作，
表单校验失败不消耗它，取消不受故障注入影响。

`work` 保留禁止执行真实闭包的兜底；未接入动作显示明确错误，不能伪装成功。
新增页面操作须覆盖这些边界，不能以注入假 executor 代替服务隔离。启动方法与场景清单见
[Studio README](../../../studio/README.md#ui-debug-preview)。

本地五引擎场景共用正式选择、注册与外部绑定控件；`preview_local_engines.go` 仅在窗口内存
推进逐题包服务替换与等待，未接入真实进程或 CLI。共享场景与验证层次见[本地引擎参考](local-engines.md#验证边界)。

长上下文速度正式与模拟共用页面和 `ContextSpeedResult` 投影；
`preview_context_speed.go` 与 `preview_speed_result.go` 仅进入调试构建，
在窗口内存推进准备、绑定、计划、等待、五次测量、取消和结果读取。
正常按钮仍经 `previewIntent` 分流，不建立服务或执行磁盘 IO。
生产侧 `startSpeedFlow` / `readSpeedFlow` 在 Service 边界接受受控响应，
`preview_speed_parity_test.go` 分别对照明确预期再比较阶段、数值、缺测、身份、
准备/绑定失败重试、等待/活动取消、只读重试及普通报告隔离。
模拟结果另通过正式 parser 的结构/语义校验；原生窗口和真实模型另验。
完整职责与历史边界见[长上下文专项](context-speed.md)。

`preview_test.go` 覆盖全部场景、导航、隔离、重置，以及 800/1120 宽、文字缩放 1/2、浅深主题布局；
`preview_interaction_test.go` 用页面按钮覆盖准备、双题包运行、动态详情、报告、取消收尾、
失败重试、并行下载、设置生效和过期事件。macOS 原生窗口已用页面按钮完成准备环境、
模拟下载并选择模型、双题包运行、动态详情与打开本次报告。模拟检查不证明真实模型流程或 Windows/Linux 原生行为。

[`tests/ui`](../../../studio/tests/ui/README.md) 使用 XCUITest 构建实际 Go 入口并启动独立
`--debug-ui` macOS 窗口。测试只按可访问控件操作和断言，不读取 `viewState`、推进测试时钟或用
场景下拉完成流程；由真实窗口 ticker 推进模拟任务。整套测试只启动一次模拟应用，按具名 activity 连续执行，结束时核对进程未替换。结果写入新的
`build/ui-e2e/run-*`，失败保留截图与辅助功能树。根级 `mise run test:studio-ui` 和 macOS CI
调用同一入口；此层与 `ui.NewTester` 内存回归、真实 CLI／假引擎边界测试分别报告。
原生检查通过场景切换或「重置场景」隔离独立流程；「任务 · 待运行」提供已准备但未启动的起点，
避免为失败、取消和专项重复准备环境。完整准备仍由普通测评和首次使用流程覆盖。覆盖分配、等待和滚动策略
统一记录在上述原生测试说明中，不改变模拟时钟或正式流程。

[`preview_parity_test.go`](../../../studio/internal/pages/preview_parity_test.go) 对正常编排与模拟器运行同一组
场景，并将双方各自与明确预期比较：core 单题包、pelican 单题包、双题包、启动失败后手动重试、
绑定中取消、运行中同时取消活动项和等待项。观察包括阶段标题、键盘可达按钮、错误码、串行队列
状态、活动题包显示和报告包含的题包，报告不得混入已有的无关运行。
双方的活动 run fixture 均包含 CLI 会产生的 `single_run_resume_not_supported` 离线原因，
显示仍须满足独立预期的「运行中／排队中」，不能泄漏恢复告警。检查不比较随机路径、PID、耗时或模拟分数。

正常侧调用实际 `runBenchmarkFlow` / `cancelBenchmarkRuns`，在 `benchmarkBackend` 边界注入
受控服务响应；模拟侧调用页面动作与内存调度器。起点是候选已准备好，取消比较终态与无报告，
异步重置另由 `preview_parity_reset_test.go` 对照空闲及活动/等待队列两种起点：正式侧调用
实际重置循环并延迟 UI 回调，模拟侧调用新任务动作；各自断言旧任务结束、新任务不被取消，
再比较结果。该层不启动 `core.Service`、CLI 或模型，不证明环境安装、下载、
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

- `CreateModelDownload`（模型，`Kind="model"`）与 `CreateEngineDownload`（引擎，`Kind="engine"`）
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

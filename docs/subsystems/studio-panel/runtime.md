# runtime

## 运行路径

1. **启动**：`main` 先解析启动参数；`--help` 输出说明并退出，非法参数退出 2。只有开发标签
   `ui_debug` 且无 `release` 时注册 `--debug-ui` 并允许进入下述独立预览路径。
   普通启动解析嵌入配置设置 `core.Version`，用 `signal.NotifyContext` 建应用上下文并
   `core.New` 创建服务（读设置、启动串行 runQueue、按需 `RefreshDevice`），随后 `mygo.App.Run`
   进入 WhenReady：注册桌面通知订阅、创建 `pages.Reports` 与主窗口 `pages.New`。SIGINT/SIGTERM
   触发 `mygo.App.Quit` 与 `Service.Shutdown`。
2. **页面刷新**：主窗口的六页面共用一个 `viewState`；后台 goroutine 订阅 `EventBus`，按游标
   取增量事件，六类事件触发全量刷新（`sync.reset`/`report.created`/`run.state`/`engine.state`/
   `download.state`/`device.updated`，见 [`studio/internal/pages/registry_refresh.go`](../../../studio/internal/pages/registry_refresh.go)），其余做局部更新。
3. **任务执行**：准备环境按最新 readiness 依次补 CLI、结果/模型目录、工作区；已初始化就跳过 `init`，新工作区不传 `--bundle`。首次使用先根据资源清单引导准备模型与引擎，资源齐备后 workspace device-check 自动执行一次，保留 `--models-root`、`ModelName` 为空。只有退出 2 且唯一原因为 `no_local_model` 的检查结果视为资源准备提示，其他阻断保留错误。新任务复用模型/引擎选择，默认勾选存在的 zh-core 与 zh-svg-pelican，按勾选题包创建新候选并自动启动、绑定和运行；活动阶段与本次返回的 run 路径归任务内存所有，未选题包不阻挡。绑定使用独立取消上下文；新任务取消绑定和所有非终态运行并等待结束，下载不受影响。所选新 run 封存后执行 `report --runs`，`ReportVerify=false`，完成区域仅显示「查看报告」，直接打开本次输出目录对应的报告；不展示路径、生成成功文字或已核验状态，核验留在报告页。没有活动任务时从 `progress.v1` 恢复。所有命令仍经 `WizardExecute` 原白名单和身份检查。
4. **运行**：`StartRuns` 逐配置读取 bundle 与输出根并做包含/去重准入 → 入队由 `runQueue` 串行执行。
   新任务重置等待 UI 线程确认完成后退出取消循环；`Window.Update` 只负责排队，不能视为
   回调已经执行。等待期间不能启动专项，重置结束后不再取消后续新任务。
   `executeRun` 派生 `inferyard run --config`（取消为 SIGINT + 两秒兜底）；执行循环每
   `ProgressInterval`（500ms）对比运行前后证据索引、tail `events.jsonl` 投影 case 进度与分数、
   聚合资源采样；终态经 `run.state` 事件发布。任务页另经 `Service.Progress` 派生
   `inferyard progress --workspace` 解析 `progress.v1` 恢复工作区进度。运行实现与取消语义见
   [运行时执行与引擎适配](../runtime-execution.md)。
   `stageView` 在有非终态运行或活动任务处于未失败的 `run` 阶段时收起正文模型服务卡，
   题包间隙不重新展开；`cornerStatus` 保留引擎与队列状态，不受后台刷新或 busy 覆盖。
   无非终态运行且任务失败或回到工作区进度时，run/resume 阶段恢复服务操作；绑定阶段仍显示启动与绑定操作。
5. **引擎服务**：任务页（绑定或运行阶段）`StartEngine` → `Startup` 从冻结配置目录取 `[engine]` 启动行 →
   监听端口预检 → 拉起引擎子进程（stdout/stderr 写入 workspace `logs/engine-*.log`）→
   持久化 `engine-owner.json` 所有权记录（写失败即中止启动）→
   `monitorEngine` 每秒探测 `{endpoint}/health`，2xx 转 `running`；启动默认超时 10 分钟。
   `StopEngine` 仅在无活动运行时允许，SIGTERM 后 10 秒未退出则 kill，停止成功按记录身份移除所有权记录。
6. **候选准备链**：任务页候选阶段的「运行 benchmark」调用准备编排（`prepareCombination`）：所选模型 ≠ 最新 preflight 推荐时先重跑
   `device-check --model`（blocked 即停并呈现固定原因）→ 逐题包读冻结 `candidate.toml` 的
   `[model] local_path`/`[engine] binary_path` 做 canonical 比对，仅为不匹配的题包各执行一次
   `config create`（题包处于 `prepare_candidate` 且有 next_command 时复用该命令，否则按
   `config create` 的参数面组装 `--workspace`/`--bundle`/`--out`；引擎参数按平台注入 registry
   条目的同一资源：非 Windows `--engine` 二进制路径，Windows `--runtime-receipt` 回执路径，
   不拼引擎 argv）→ 复验全部待测题包
   候选匹配（不匹配报 `candidate_mismatch`）→ `runBenchmarkFlow` 启动引擎并等待健康检查。新任务强制为所选题包重建候选；任一步失败即停，不自动重试。
7. **绑定**：`runBenchmarkFlow` 在健康通过后按所选题包顺序依次执行 `config bind`（同引擎同 endpoint、显式 PID），
   全部成功才进 run 阶段，任一失败即停、显示 CLI 固定原因、不自动重试；外部服务从
   `service discover` 清单一键绑定（显式传发现的 PID 与 endpoint），绑定失败后折叠的「高级：手动绑定」保留
   `Processes` 进程枚举 + endpoint 手输兜底（`Processes` 只暴露 PID 与可执行名，argv 可能带凭据）。
   `run_missing` 与 single 的 `run --config` 在新 PID 下重新绑定；`resume --from-run` 直接传 PID 与 endpoint，不要求预绑定。duration 与未完成 batch 的 `run --plan` 仅显示阻断原因，不放宽执行白名单。
   一切绑定都生成绑定配置并经 CLI 的 PID 与启动时间身份核验；discover 不预判 bind 可行性，
   `startup_arguments_mismatch` 等拒绝原因如实呈现；`BindingAlive` 仅提供存活提示。
8. **报告**：页面先展示「历史任务」，再展示「报告列表」。进入任务清除详情与续测来源并聚焦所选运行；续测清除详情并设置来源；实时详情打开对应任务，三者都进入任务页。打开报告为主操作；1–100 个来源的生成入口折叠在「从历史运行另做报告」。仅工作区 `next_action=verify` 且报告未核验时显示「核验」，沿用向导并设置 `ReportVerify=true`；`verified` 显示「已核验」。
   `CreateReport` 校验 1–100 个位于结果根内的不同 run → 派生 `inferyard report`
   写入结果根同级 `reports/` 下的新唯一目录 → 发布 `report.created`；页面在主窗口内容区读取 `index.json`，呈现当前所选运行的指标卡、分类表、模型与机器、逐题答案及数据来源。
   指标取自 `runs[].dashboard` 与 `summary.completion_rate`；模型、引擎和机器取自 `profile`、`threads`，题包取自 `facets.bundle_label`，运行 ID 取自 `source.run_id`，逐题行取自 `requests`。
   多运行索引用选择器分别显示，不合并指标。质量和完成率保留分母与排除数；缺测显示 — 并保留已有采样原因。延迟从 ms 转为 s，内存从 bytes 转为 GiB；诊断运行标注「诊断运行」。
   `ReportIndex` 接受列表 ID 或本次报告绝对目录；绝对目录先规范化并验证位于配置的 reports 根目录内，再经 `ReportFile` 校验索引，越界路径不能按同名目录打开。报告 ID 为 UTC
   `report-YYYYMMDDTHHMMSSZ[-N]`，命名冲突追加序号。报告与导出契约
   见 [评分、比较与报告](../scoring-reporting.md) 与
   [report-contract](../../../cli/docs/contracts/report-contract.md)。
9. **下载**：模型/引擎获取与运行队列并发、互不占位，共用 `downloading → done/failed/cancelled`
   任务列表与 `CancelDownload`。输出目录名默认取设置的下载目录名，占用（文件系统或历史任务记录，
   含终态）时自动追加 `-2`/`-3` 序号；调用方显式指定名字且冲突时仍返回 `output_exists`。
   `CreateModelDownload` 只接受无用户信息、查询
   与片段的 `https` 来源；轮询观测 `.part` 尺寸并以 5 读数窗口估算速率，正式发布文件出现后完成
   任务并按实测字节收尾。引擎获取（预置或自定义来源）经 `CreateEngineDownload` 进入同一列表，
   复用 `FetchEngine` 传输与解包：归档解包保留同级共享库、仅重建归档内相对
   符号链接（`safeLinkTarget` 拒绝越界链接），输出目录已存在或已被历史任务记录占用即拒绝
   （`output_exists`），取消贯穿传输与解包；该传输接口不提供字节读数，引擎任务的   `downloaded_bytes`/`bytes_per_second` 恒为 `null` 并带 `progress_reason`，成功收尾写
   `engine_path`。所有 `fetchURL` 传输超过 64 MiB 拒绝（`download_too_large`），不静默截断。
9. **关停**：`Shutdown` 立即关闭准入，未终态队列任务标 cancelled、活动任务标 cancelling 并
   取消上下文，待专项准备与 runQueue 排空后停止自启动引擎再置 `shutdownDone`；调用方超时（main 为 30
   秒）不抛弃清理、不改变顺序。

任务页的资源引导与活动题包投影由 [`first_use.go`](../../../studio/internal/pages/first_use.go) 从现有清单、下载任务和运行队列派生，不持久化另一套向导状态。活动题包按 bundle 关联非终态 RunTask，展示排队／运行／取消状态；离线 `progress.v1` 的恢复原因仅用于没有活动任务的题包。历史 `no_local_model` 在可用模型出现后不再重复展示，实际候选准备仍执行模型专属设备检查。右上角显示下载任务生命周期，后台 refresh 不产生全局忙碌提示。

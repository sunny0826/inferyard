# Studio 页面 Design System 审查

更新于 2026-10-14。初始审查基线为 `b92c41f`，MyGo v0.4.5；组件层随后迁移到
onevra v0.2026.9（构建在 MyGo v0.4.8 之上），映射见
[组件用法](design-system.md#onevra-组件映射)。本页记录其后按
[Studio Design System](design-system.md)实施的页面改动和实际验证范围。
范围包含六个主页面、运行详情及主窗口内报告视图。未完成事项集中在
[backlog](../../cli/docs/backlog.md#inferyard-studio-后续工作)。

## 当前结果

DS-01 至 DS-09 已有对应实现和软件回归；原生窗口与辅助技术尚未验收。
不得由测试通过推导三平台桌面体验全部合格。

| 编号 | 原问题与本轮落实 | 回归入口 |
| --- | --- | --- |
| DS-01 | 原不定进度传入 NaN，现用 -1 与名称；有效比例保留冻结分母；减少动态效果时静态显示 | `TestDesignProgressRanges`、`TestDesignReducedMotionIsStatic` |
| DS-02 | 标题增加角色及层级；表格、来源列表、进度均有容器名称 | `TestDesignNamedControlsAndHeadingLevels` |
| DS-03 | 主指标按语义显示缺测、总量未知、无活动进程、未配置；数据层 null 和单位不变 | 下载卡与测量既有回归 |
| DS-04 | 共享 Rem/Space，窄窗或文字放大后模型页单列，按钮与图例换行，设备键值纵排 | `TestDesignLayoutMatrix`、`TestDesignModelsStackAndLongPath` |
| DS-05 | 详情读取失败不再同时显示加载；增加重试、返回及空态下一步；刷新失败保留已有内容 | `TestDesignDetailFailureIsNotLoading`、`TestDesignDetailRetryAndLoading`、`TestDesignPageLoadingErrorEmpty` |
| DS-06 | 冻结配置准备直接指向 Dashboard；服务步骤中文化，颜色区分活动、就绪、失败和停止 | `TestDesignEngineSemanticColors` 与既有步骤、绑定回归 |
| DS-07 | 页面变化与当前页新错误一次性公告，主动重复提交重新公告对应错误，重渲染去重，刷新保留输入焦点 | `TestDesignAnnouncementsOnce`、`TestDesignHiddenDetailErrorIsNotAnnounced`、`TestFormFailureAnnouncedOnEverySubmission` |
| DS-08 | 输入关联 Description/Error；提交保留草稿，后端仍校验 | `TestDesignInvalidFieldsPreserveEdits` 与既有设置、续测回归 |
| DS-09 | 设置仅展示路径与下载目录名表单；历史封存中文化并区分核验；服务诊断位于操作之后 | 历史展开、设置切换与页面布局回归 |

初始 NaN 和详情错误/加载重叠已用真实视图回归复现，再以同一回归验证修复。
NaN 会被 MyGo 写入无障碍 range；此前未观察到读屏故障或崩溃，不将它们描述为已复现后果。
其余规范迁移包含项目新增的信息层级与尺寸约定，不倒推为旧页面违反 MyGo 官方设计。

## 组件基线迁移

组件层已从 MyGo v0.4.5 + 自制组件迁移到 onevra v0.2026.9（构建在 MyGo v0.4.8 之上），
实现于工作树：

- 窗口外壳改用 `shells/sidebar.NewShell`：可折叠、可调宽侧栏，浮动 header，macOS titlebar
  材质与 Windows Mica；标题 1120 × 800、最小 800 × 600，侧栏状态键 `inferyard-studio-main`。
- 主题改用 `theme.Onevra`（暖中性色 + terracotta），经 `ShellOptions.Theme` 安装。
- 导航改用 `components.Sidebar` / `components.SidebarItem` 配 lucide 图标；后随模型服务流
  改造增至六页（新增「下载」页）。
- 按钮、徽章、Spinner、进度、复选框、选择、文本框、表格与资源曲线映射到 onevra 组件；
  标题、`ui.Field` 与布局原语保留 MyGo 原生实现。
- 测试视图与真实窗口共享 `leftNav` / `contentView` 构建函数。

完整映射、保留例外与依赖许可见 [组件用法](design-system.md#onevra-组件映射)；
软件回归入口见下方“软件验证”，原生窗口验收仍按“待验收”清单执行。
下方“软件验证”记录的测试数、渲染图片与目检均来自迁移前的审查轮次。

## 页面与实现归属

| 页面 | 本轮落实 | 源码 |
| --- | --- | --- |
| 公共外壳 | onevra Shell 侧栏与主题、命名控件、首次加载、刷新状态、一次性公告 | [window.go](../internal/pages/window.go)、[design_system.go](../internal/pages/design_system.go) |
| Dashboard | 阶段进度名称、中文阻断原因、当前下一步；准备与绑定字段说明、有限进度、历史层级、续测字段错误 | [dashboard.go](../internal/pages/dashboard.go)、[dashboard_run.go](../internal/pages/dashboard_run.go)、[dashboard_tasks.go](../internal/pages/dashboard_tasks.go) |
| 设备 | 标题、紧凑键值排版、解析失败恢复入口 | [device.go](../internal/pages/device.go) |
| 模型 | 选择区双向联动标注、运行区状态与归因、只读服务卡、缺测语义 | [models.go](../internal/pages/models.go)、[models_picker.go](../internal/pages/models_picker.go)、[models_run.go](../internal/pages/models_run.go) |
| 下载 | 任务清单、取消/重试、实际目录名展示与新建表单 | [downloads.go](../internal/pages/downloads.go)、[models_downloads.go](../internal/pages/models_downloads.go) |
| 运行详情 | 失败与加载分离、重试和返回、表格名称、换行图例 | [detail.go](../internal/pages/detail.go)、[chart.go](../internal/pages/chart.go) |
| 报告 | 来源列表名称化展示、空态下一步、键盘打开 | [reports.go](../internal/pages/reports.go)、[reports_table.go](../internal/pages/reports_table.go) |
| 设置 | 字段关联、路径保存与下载目录名 | [preparation.go](../internal/pages/preparation.go)、[form_validation.go](../internal/pages/form_validation.go) |
| 报告视图 | 完成按钮直达对应报告；四指标卡、分类表、模型与机器、逐题表和来源；多运行独立切换、缺测、重读与返回列表 | [report_view.go](../internal/pages/report_view.go) |

设备页展示硬件快照；模型列表与推荐在模型页。未将历史 HTML 原型当成已经实现的原生页面。
报告视图读取本地 `index.json`，字段映射见[子系统参考](../../docs/subsystems/studio-panel.md#运行路径)；重读重新执行路径校验。
索引损坏只在该列表项及打开后的视图显示无法读取，不影响其他报告，不修改历史 HTML。

## 软件验证

环境为 macOS arm64、mise Go 1.27.2。命令从 `studio/` 执行，使用
`GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0`：

- 实现阶段 `go test ./internal/pages -count=1` 通过：82 个顶层测试。
- 整合、地址文案及重复提交公告修正后 `go test ./...` 与 `go vet ./...` 均通过。
- `CGO_ENABLED=0 go build` 对 darwin/arm64、windows/amd64、linux/amd64 均通过；
  产物写入临时目录，未打包或安装。macOS 后续按用户要求启动供手动验收；
  交叉编译不代表 Windows/Linux 运行验收。
- [布局矩阵](../internal/pages/design_layout_test.go)覆盖 7 个视图 × 3 个尺寸 × 3 个文字比例 ×
  深浅色 × 高对比开关，共 252 个场景。尺寸为 1120×800、800×600、1440×900；文字比例为
  1、1.5、2。断言检查实际节点水平边界、一级标题和固定工具栏，表格仅豁免其内部可横向滚动内容。
- [交互回归](../internal/pages/design_interaction_test.go)与
  [语义回归](../internal/pages/design_system_test.go)检查字段纠错、公告、焦点、恢复动作及进度。
  部分语义断言读取固定 MyGo 版本的内部视图节点，升级依赖时需要复核这些测试适配；
  当前依赖为 MyGo v0.5.4 与 onevra v0.2026.10；2026-10-16 升级后在 macOS arm64
  重跑 `go vet ./...` 与 `go test ./... -count=1 -timeout 900s` 均通过，包含页面布局、交互与真实 CLI / 假引擎 e2e。
  本次未重做原生窗口目检，历史图片不作为新依赖的视觉验收证据。
- [报告视图回归](../internal/pages/report_view_test.go)使用临时索引和禁止 CLI 执行的 fixture，
  检查主窗口打开、质量与类别分母、空延迟、单项读取失败、重新读取及原 HTML 字节不变；不替代原生验收。
- 既有双题包、单题包显式确认、1–100 来源限制、续测拒绝、冻结分母、缺测断点和取消回归继续通过。

实现者导出了 34 张 MyGo 内存渲染 PNG，并根据目检调整设备键值与报告长路径。
独立验证者重新生成图片，目检七个视图的 1120×800／文字比例 1、800×600／文字比例 2
高对比浅色图，以及部分深色图；未目检高对比关闭、1440 宽窗、文字比例 1.5 或滚动后的画面。

图片复核确认报告来源列表按底边裁剪，列表与页脚相隔 10 DIP；
[像素回归](../internal/pages/design_visual_regression_test.go)在深浅色下改写来源 ID，确认页脚区域不变。
详情的 `last_seq: 0` 是事件游标初值，不属于缺测补零；这两项初步判断均已撤回。
模型双栏中地址值的重复 Endpoint 前缀使“未启动”断行，现已去掉该前缀。
独立验证者目检主工作区重新生成的 1120×800／文字比例 1 高对比深浅色图，
确认“未启动”完整显示在一行，邻近配置、进程及缺测文案也完整。
独立代码审查逐一检查初始交付的 27 个文件及 MyGo/core/CLI 对应契约，并重跑全模块测试与 vet，
无阻塞发现。审查发现相同无效表单重复提交时公告被去重；
[六条提交路径的回归](../internal/pages/form_announcement_test.go)先复现第二次提交无公告，再验证修正通过。
设置、续测、模型下载、引擎下载、进程绑定和工作区初始化现在均在主动提交时重新公告对应错误，
静止帧仍去重。增量独立复核通过：审查者在旧快照副本加入新测试，确认六条路径均在第二次
提交失败；在主工作区重跑页面测试与 vet 通过，并确认只清除对应消息的公告记录，不重播无关错误。
报告打开失败的重复公告尚未覆盖，作为非阻塞后续项记录在 backlog。

临时图片可由 `STUDIO_UI_PREVIEW_DIR` 配合页面测试重新生成（先创建输出目录），不作为持久验收附件。
未下载模型、启动模型服务或发送推理请求。

## 待原生验收

按[规范验收矩阵](design-system.md#验收)逐页补充：

1. 真窗口压力尺寸、滚动、文字缩放、高对比度、焦点环及减少动态效果。
2. 键盘导航、列表选择、详情返回、字段纠错、取消后的焦点和真实读屏公告。
3. 主窗口报告视图的打开、返回列表、空延迟、损坏索引与重新读取，以及离线滚动和焦点。
4. macOS VoiceOver、Windows Narrator、Linux Orca 各自的证据。

用户要求手动验收后，macOS 当前分支应用已用既有设置启动，两次均正常退出。
根据用户的设置页截图反馈，已移除全部页面顶部的通用“刷新”按钮，空闲状态不再占用工具栏；
自动更新、错误重试与 Cmd+R 保留。该调整后全模块测试、vet 与 macOS 构建通过。
这次窗口启动与单页反馈不构成完整原生验收；报告视图的后续复核见下文，其他交互与读屏仍未完成。
MyGo 内存渲染、PNG 目检和软件回归不替代以上验收，也不外推到其他平台。

报告布局复核：[report_presentation_test.go](../internal/pages/report_presentation_test.go) 覆盖完成与恢复入口直达、多运行切换、数值与缺测、长表滚动及越界路径拒绝；布局矩阵包含报告详情。macOS 原生模拟窗口已检查完成按钮、直达详情、四指标卡、表格铺满和页面滚动；未运行模型，未改写历史报告。此证据不覆盖 Windows/Linux 原生交互、读屏或真实测评数据。

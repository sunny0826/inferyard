# Studio 页面 Design System 审查

更新于 2026-10-14。初始审查基线为 `b92c41f`，MyGo v0.4.5；本页记录其后按
[Studio Design System](design-system.md)实施的页面改动和实际验证范围。
范围包含五个主页面、运行详情及离线报告窗口外壳。未完成事项集中在
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
| DS-09 | 设置先展示生效工作区，低频原始字段折叠；历史封存中文化并区分核验；服务诊断位于操作之后 | 历史展开、设置切换与页面布局回归 |

初始 NaN 和详情错误/加载重叠已用真实视图回归复现，再以同一回归验证修复。
NaN 会被 MyGo 写入无障碍 range；此前未观察到读屏故障或崩溃，不将它们描述为已复现后果。
其余规范迁移包含项目新增的信息层级与尺寸约定，不倒推为旧页面违反 MyGo 官方设计。

## 页面与实现归属

| 页面 | 本轮落实 | 源码 |
| --- | --- | --- |
| 公共外壳 | 主题标题、命名控件、首次加载、刷新状态、一次性公告 | [window.go](../internal/pages/window.go)、[design_system.go](../internal/pages/design_system.go) |
| Dashboard | 阶段进度名称、中文阻断原因、当前下一步；准备与绑定字段说明、有限进度、历史层级、续测字段错误 | [dashboard.go](../internal/pages/dashboard.go)、[dashboard_run.go](../internal/pages/dashboard_run.go)、[dashboard_tasks.go](../internal/pages/dashboard_tasks.go) |
| 设备 | 标题、紧凑键值排版、解析失败恢复入口 | [device.go](../internal/pages/device.go) |
| 模型与下载 | 四区响应布局、缺测语义、状态色和 Dashboard 入口 | [models.go](../internal/pages/models.go)、[models_engine.go](../internal/pages/models_engine.go) |
| 运行详情 | 失败与加载分离、重试和返回、表格名称、换行图例 | [detail.go](../internal/pages/detail.go)、[chart.go](../internal/pages/chart.go) |
| 报告 | 来源列表名称及完整路径描述、空态下一步、键盘打开 | [reports.go](../internal/pages/reports.go)、[reports_table.go](../internal/pages/reports_table.go) |
| 设置 | 字段关联、生效工作区与折叠诊断 | [preparation.go](../internal/pages/preparation.go)、[form_validation.go](../internal/pages/form_validation.go) |
| 报告窗口 | 固定身份标题、加载失败回传、窗口内重试与关闭 | [report_window.go](../internal/pages/report_window.go) |

设备页展示硬件快照；模型列表与推荐在模型页。未将历史 HTML 原型当成已经实现的原生页面。
报告窗口仍以本地文件加载，按路径复用窗口；重试重新执行 `ReportFile` 路径校验。
错误使用固定文案，不展示系统 WebView 的原始 URL 或错误描述，不修改历史 HTML。

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
  部分语义断言读取固定 MyGo 版本的内部视图节点，升级依赖时需要复核这些测试适配。
- [报告窗口回归](../internal/pages/report_window_test.go)使用临时 HTML 和禁止 CLI 执行的 fixture，
  检查加载失败恢复视图、重新校验路径、原 HTML 字节不变；它没有触发系统 WebView 的真实事件。
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
3. 报告 WebView 的本地加载、加载后失败、离线交互、重复打开聚焦、关闭后主窗口操作。
4. macOS VoiceOver、Windows Narrator、Linux Orca 各自的证据。

用户要求手动验收后，macOS 当前分支应用已用既有设置启动，两次均正常退出。
根据用户的设置页截图反馈，已移除全部页面顶部的通用“刷新”按钮，空闲状态不再占用工具栏；
自动更新、错误重试与 Cmd+R 保留。该调整后全模块测试、vet 与 macOS 构建通过。
这次窗口启动与单页反馈不构成完整原生验收；上列交互、读屏和系统 WebView 检查仍未完成。
MyGo 内存渲染、PNG 目检和软件回归不替代以上验收，也不外推到其他平台。

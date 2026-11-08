# design-system-components

## 布局与尺寸

MyGo 布局使用 DIP。以下数值来自现有主窗口与模型页，作为默认密度下的基线；
不把 DIP 当成屏幕物理像素，也不要求复刻 HTML 的字体度量或字距。

| 项目 | 基线与用途 |
| --- | --- |
| 初始主窗口 | 1120 × 800；这是初始尺寸，不是已验证的最小窗口 |
| 侧栏 | onevra Shell 侧栏默认 205 宽，可折叠、可拖拽调宽；缩放时保证标签和键盘焦点可见 |
| 内容外边距 | 18，`t.Space(6)`（Studio 主题间距单位 3） |
| 主要区块间距 | 12，`t.Space(4)`；模型页列间与卡片间可用 15 |
| 卡片 | 内边距 10.5，圆角 6，1 DIP 描边；普通内容间距 7.5 |
| 紧密内容 | 3、6；表单或操作组 7.5、9；不按单个页面随意新增刻度 |
| 边框 | 1，使用 `t.Border` |
| 控件圆角 | 4（Studio 主题的 `Radius`） |
| 模型页宽布局 | 左右约 1.2:1，右栏最小宽 310 |

新组件用 `t.Space(...)` 表达间距与尺寸，在共享组件中维护语义名称。
Studio 主题（`studio_theme.go`）把间距单位设为 3、控件圆角设为 4：18 即 `t.Space(6)`，
12 即 `t.Space(4)`，10.5 即 `t.Space(3.5)`。现有常量迁移不得改变控件焦点和可点击区域。
保留 MyGo 控件默认内边距、行高、禁用态与焦点环，避免为对齐截图压缩操作目标。

宽布局只有在两栏都能容纳内容时启用。空间不足或文字放大时改为单列，模型页顺序为选择区、
运行区、本机运行的服务；操作组允许换行。不要仅缩小字体或裁掉按钮来适应窗口。
表格允许自己的横向滚动，页面其余内容保持纵向阅读。

路径、URL、哈希和运行 ID 可选择复制；需要截断时提供完整值的可访问展示或复制入口。
阻断原因和恢复动作不截断。图表图例与长按钮行随可用宽度换行。

## 颜色与字体

Studio 在 onevra 组件之上安装自己的方案 A 调色板（`studio_theme.go`：暖中性色配
terracotta 强调色），由 `shells/sidebar` 在 `ShellOptions.Theme` 安装，浅色/深色跟随
`mygo.Theme.SetSource` 并随系统高对比度与文字缩放变化；onevra 其余字段保持默认。
背景使用 `Background`，卡片使用 `Surface`，主文字使用 `Text`，次要说明使用 `TextMuted`，
分隔线使用 `Border`。正文和交互区域不硬编码浅色背景或黑色文字。

HTML 原型与样式对照使用 Studio 调色板实际值（来源：`studio_theme.go`，调整方案时同步）：

| 语义 | 浅色 | 深色 |
| --- | --- | --- |
| `Background` | `#f8f7f3` | `#1b1a17` |
| `Surface` | `#ffffff` | `#24221e` |
| `SurfaceHover` / `SurfacePressed` | `#f0eee8` / `#e3e0d8` | `#2e2b26` / `#38342e` |
| `Border` | `#d6d3c9` | `#47433c` |
| `Text` | `#26261f` | `#efeae2` |
| `TextMuted` | `#5c5c53` | `#c6bfb3` |
| `Accent`（terracotta） | `#b3401a` | `#e88d64` |
| `AccentText` | `#ffffff` | `#291b14` |
| `Success` | `#047857` | `#6fbfa2` |
| `Warning` | `#a34a06` | `#dfa95f` |
| `Danger` | `#a63a42` | `#dd878d` |
| `Selection` / `Focus` | `Accent` 22% / 55% 透明度 | `Accent` 24% / 80% 透明度 |
| 圆角 `Radius`（控件） | 4 | 4 |
| 卡片圆角 | 6 | 6 |
| A/B 侧色 | `#2563eb` / `#b45309` | `#7cb2ff` / `#e6b567` |

| 语义 | 主题颜色 | 使用条件 |
| --- | --- | --- |
| 当前操作、活动状态 | `Accent` | 当前阶段、下载中、启动中、取消中 |
| 已确认成功或就绪 | `Success` | 获取完成、健康检查通过；不据此暗示绑定或证据已核验 |
| 需要处理的限制 | `Warning` | 阻断、部分结果、存活提示无法确认 |
| 失败 | `Danger` | 操作失败、工具或证据错误 |
| 中性 | `TextMuted` | 尚未配置、等待（含排队中）、已停止、已取消 |

状态必须有文字，不能只靠颜色。相同业务语义复用同一呈现映射；不同领域分别映射，
例如引擎 `running` 表示服务已就绪，测评 `running` 表示工作尚在进行。

沿用模型页的低饱和背景：状态面板可用 `color.Mix(t.Surface, 0.93)`。
徽章使用不透明的 tint 背景（`Surface` 混入状态色 12%）并把文字混向 `Text` 30%，
不再使用半透明背景；浅色与深色下正文对比度不低于 4.5:1，状态仍必须有文字表达。
高对比度下使用主题默认表面并保留 1 DIP 状态色边界；关键文字不得通过降低透明度弱化。
模型推荐区可以保留现有轻微主题渐变，不把它扩展到每个卡片；普通内容无需阴影或装饰底纹。

正文继承主题字体与 `FontSize`，代码、路径和安全命令按需使用等宽字体。
项目字号层级建议以 `t.Rem(...)` 表达：页标题约 1.6、卡片标题约 1.2、
状态结论约 1.4、指标约 1.25、辅助文字约 0.82。该比例是本次项目约定，
不是 MyGo 官方要求；Studio 主题基础字号再乘 0.96。默认密度下辅助文字目标不小于 12 DIP；
错误和操作条件使用正文尺寸。
文字缩放时容器也须扩展或换行，不能只调整字号。

## 组件用法

| 组件 | 约定 |
| --- | --- |
| 页眉 | 普通任务页不显示页顶标题；其他页面有一个一级标题，可有短说明；eyebrow 可选，不用它替代标题或导航 |
| 普通卡片 | 二级标题、相关内容与就近操作；避免多层同款卡片嵌套 |
| 状态面板 | 中文状态、结论、原因或下一步；代码放在次要诊断位置 |
| 键值组 | 标签和值有语义关联；值可复制；缺测原因靠近值 |
| 任务卡 | 类型、名称、状态、指标、操作、输出与错误；以任务 ID 保持稳定身份 |
| 步骤条 | 显示阶段名称和当前阶段；单个当前节点；重复名称仍按位置区分 |
| 步骤轨道 | 任务页当前阶段的行动区：编号圆点加纵向连接线，内容铺在页面背景上不用卡片；只渲染当前阶段；轨道标题与卡片标题同为二级 |
| 表格和列表 | 容器有 `Label`；稳定行 key；选择后操作可通过键盘到达 |
| 通知 | 成功可用 Toast；错误保留在操作附近或页内，不能只显示瞬时消息 |

优先使用 onevra `components` 包的控件，并保留 MyGo 布局原语与 `ui.Field`。
不以可点击普通文本替代标准按钮。共享组件的现状见
[components.go](../internal/pages/components.go)与[通用辅助函数](../internal/pages/shared.go)；
标题、活动进度、按钮换行与公告集中在
[design_system.go](../internal/pages/design_system.go)，字段校验提示集中在
[form_validation.go](../internal/pages/form_validation.go)。软件回归与原生验收范围分别记录在页面审查中。

### onevra 组件映射

已实现的控件映射（onevra v0.2026.17）：

| Studio 用法 | onevra 组件 |
| --- | --- |
| `button` | `components.Button(ButtonSecondary)` |
| `primaryButton` | `components.Button(ButtonPrimary)` |
| `statusBadge` | `components.Badge` |
| `activitySpinner` | `components.Spinner(SpinnerRing)` |
| `progressBar` | `components.Progress` |
| 复选框 | `components.Checkbox` |
| 下拉选择 | `components.NativeSelect` |
| 文本输入 | `components.TextBox` |
| 表格 | `components.Table`，封装见 [table.go](../internal/pages/table.go) |
| 资源曲线 | `components.chart.Line`（见“测量、进度与图表”） |
| 侧栏导航 | `components.Sidebar` / `components.SidebarItem` 配 lucide 图标 |
| 列表 | `components.List`（模型/引擎/服务清单与报告来源列表） |
| 折叠分区 | `components.Collapsible`（高级选项与低频诊断折叠） |
| 通知 | `components.Toast`（成功与状态提示；错误仍保留在操作附近） |
| 滚动容器 | `components.ScrollView` |

全面切换约定：onevra 有等价组件的一律使用 onevra，不再新增 MyGo 自带控件实现。
源码残留（`ui.List`、`ui.Collapsible`、`c.Toast`）随本批页面改造迁移到上表组件。

下列保留 MyGo 原生实现，理由是 onevra 没有等价能力：

| 保留项 | 原因 |
| --- | --- |
| 标题：`ui.Text` 配 `Role(ui.RoleHeading).Level(...)` | onevra `Text` 不提供标题角色与层级无障碍语义 |
| 表单标签与错误关联：`ui.Field` | onevra 不提供等价的标签/错误关联外壳 |
| 布局原语：`ui.Row`、`ui.Column`、`ui.Scroll`、`ui.Toolbar`、`ui.Grid` 等 | 页面外层布局骨架仍由 MyGo 布局原语承载 |

依赖许可：onevra 为 MIT；其适配自 MyGo 的部分保留上游 MIT 声明，lucide 图标为 ISC/Feather MIT。

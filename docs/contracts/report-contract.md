# 离线报告展示契约

依据 [ADR 011](../decisions/011-report-dashboard.md)、[ADR 033](../decisions/033-svg-generative-category.md)
及 [ADR037](../decisions/037-offline-evidence-reading.md)。新建报告 `schema_version = 3`、
`report_format_version = 7`；仅接受 v7；历史 v1–v6 返回 unsupported_format/2，旧 HTML 交原项目处理。
各版本 `template_sha256` 绑定所用模板目录内排序后的文件名与字节，包含样式、脚本和子模板。
v7 将模板摘要保留为生成溯源，另封存 HTML 字节并核验来源和数据语义；重渲染是显式选项，
见[离线读取契约](offline-reading-contract.md)。`script-src` 的 SHA-256 按所渲染模板版本的
`report_script.html` 现算，快照报告不受后续脚本编辑影响。

模型与机器 profile 来源只限通过原 manifest / `read_trial` 核验的配置、identity 与 environment。文件大小仅从与模型 path / SHA 匹配的 identity 记录取得；无匹配为 null。结构参数量不从文件名的 27B 等文本猜测；文件名可作为显示标签，原配置显示名保留。硬件参数属于测评时快照，缺失附来源 / 原因，不用当前机器或 Linux 默认值填充。统一内存不加到 RSS / GPU 内存中。

生成参数分别保留 requested、effective、verification、source；没有实际核验时 effective=null、verification=unknown。冻结执行、采集、线程、上下文、并发、缓存与推理条件完整展示。

六类 Q01 质量率分别展示，不合成总体质量分数。首页仅累计同 run 的已记录通过数 / 有效执行数；任一分类率缺失或不适用时标为未完整观测，并保留原因。完成率使用核心原分母，五种终态分别呈现，不因 UI 筛选改变。耗时 p50/p95 使用最近秩，仅统计 completed 且时间完整的请求，保留样本数与排除数，不把失败耗时混入完成耗时；p95 小样本标为探索性。

资源按原来源与窗口独立展示；同指标存在多个来源时首页峰值为 null 并提示查看分图，不取跨来源最大值。采样峰值不能称连续峰值；缺段不连线。逐题展示题目、参考答案、原始输出、评分规则结果、耗时、token 和停止原因，未知仍为 null / —。用户内容自动转义，唯一例外是 v3 起通过下述检查的 SVG 提取结果。显示筛选只隐藏，不重评分、不缩分母。

## v4 SVG 画廊

v4 在运行概览 KPI 之后新增 `svg_gallery` 与「SVG 生成展示 · 不评分」区段：每个 svg 题以
整栏图形原始渲染，附题面、解析状态、字节数、耗时、输出 token 与题目详情锚点；提取失败
（`not_found`/`malformed`/`too_large`）降级为状态说明，不渲染。无 svg 题的运行为空列表，
不渲染该区段。画廊与逐题 `svg_view` 同源自 request 的 `content` 现算，渲染规则与安全
约束同下节；当前非 SVG 行保留既有语义。

## v3 SVG 展示与 CSP

只对 `category = svg` 行新增 `svg_view`，从 request 的 `content` 现算，非 SVG 行零字段变化。
优先首个完整的 svg 代码围栏，否则首个闭合的 `<svg ...</svg>` 子串，无匹配为 `not_found`。
stdlib XML 解析禁止 DTD，根元素须为 SVG；非法 XML 为 `malformed`。
字符数上限为 512 × 1024（含边界），超限为 `too_large`。`bytes` 单独记录候选文本的 UTF-8
字节数；未提取时为 0。只有 `ok` 返回原始 `svg` 字符串，其余为 null。
这些是 informational 观察量，不进入 score 或指标目录。截断、未闭合、超限均降级为源码展示，
不改变 run 状态。原始输出始终以转义的 `<pre>` 折叠保留。

通过检查的 SVG 局部关闭转义并原始内嵌，不做标签或属性清洗，不重新序列化。
安全依赖 CSP：`default-src 'none'; style-src 'unsafe-inline'` 允许 SVG 样式和
`@keyframes` 动画；`script-src` 只允许报告固定脚本的 SHA-256，禁止模型脚本及事件处理器。
报告不得在无 CSP 环境分发，后续模板修改不得放松 `script-src`。
原始 CSS 可能影响页面布局；XML 检查不评价图形质量或布局隔离。
证据资格遵循[总规则](../data-contract.md#证据血缘与比较结论)。

报告完整性、诊断和比较资格继续分别展示；单次资源读数不授予严格比较资格。报告使用已封存数据，来源源码与报告生成源码分别记录。原始 trial 仍由 manifest / `read_trial` 核验，派生报告通过 `inferyard verify --path REPORT`。

比较产物的 `schema_version`、`format_version` 必须是整数；离线重建结果按规范 JSON
逐项核对，嵌套布尔、整数与浮点数不能利用 Python 宽松相等互相冒充。
`compare-check` 与通用 `verify --path` 共用此核验；通用入口还核验比较报告。
合法旧产物保持可读，证据资格遵循[血缘总规则](../data-contract.md#证据血缘与比较结论)。


## v5 描述性比较

[ADR035](../decisions/035-purpose-specific-admission.md) 新增 comparison format v2 /
phase2.v2；现由 ADR038 收敛为 comparison4 / phase2.v3 与 report7。旧格式不再重算。
执行 plan/run 标记保持 phase2.v1。
observed_differences 展示同口径完成率/质量观测、逐题记录、条件差异与样本范围。
质量总体差值要求相同题目内容、答案政策、规则、协议、评分器、分母及完整评分。
性能先展示匹配定义/来源/单位的双侧值；无开销/环境/逐指标资格时差值仍为 null。
eligibility 继续表达受控结论资格，不用描述性字段授予因果或性能结论。

## 当前 v7 边界

当前隐式比较采用 comparison4 / phase2.v3，校准适用域见
[职责身份契约](scoped-measurement-contract.md)。v7 沿用此计算语义，新增呈现封存和来源相对定位；
comparison v4 同样只改变封存/定位结构。关联比较仅接受 format4。

# ADR 033：SVG 生成题与报告 v3

状态：Accepted

## 决策

活动数据契约仍为 schema_version = 3。新增 `svg` category，归属 `quality` 协议，
`rules` 只能为 `{}`，`reference_answer` 必填并说明无标准答案。
评分返回 `quality_state = not_applicable`；成功、失败或未执行的 SVG 题均不进入
Q01–Q08 的分子、分母或排除计数。执行终态和现有生成期计时、token、资源采集继续保留。
SVG 内容不调用质量评分规则，不新增指标目录项。

题包 `bundles/zh-svg-pelican.json` 使用英文原题
`Generate an animated SVG of a pelican riding a bicycle`，按 `zh-CN` 登记，
`license_note` 明示英文原文；审核记录为空，后续另行人工审核。

## 展示与安全

report_format_version = 3 从保存的 request content 确定性提取 SVG：优先首个完整
svg 代码围栏，否则首个闭合的 `<svg ...</svg>` 子串。XML 解析禁 DTD，
字符上限为 512 × 1024；UTF-8 字节数单独展示。只展示 `ok`、`not_found`、
`malformed`、`too_large` 观察量，不写 score。超限或截断只展示转义的原始源码，
`finish_reason = length` 不因此改变 run 状态。

通过检查的 SVG 原始渲染，不做任何标签、属性白名单或黑名单清洗，也不序列化改写。
安全依赖报告 CSP：`default-src 'none'`、`style-src 'unsafe-inline'` 支持内联样式
及 CSS 动画。检查发现旧模板有 `script-src 'unsafe-inline'`，因此 v3 改为只允许
报告自身固定脚本的 SHA-256，阻止模型脚本和事件处理器，保留离线筛选功能。
报告不得在无 CSP 环境分发，后续模板修改不得放松 `script-src`。
XML 可解析不等于视觉正确，也不证明模型样式无法影响报告布局。

v2 的全部报告模板按原字节冻结在 `templates/v2/`；v1/v2 各用原模板和旧投影核验，
不回写历史 HTML。v3 使用当前模板，模板哈希覆盖所有当前报告子模板。
非 SVG 行的投影字段保持一致。

## 补充（2026-10-07，report_format_version = 4）

SVG 是模型能力的可视化表现，不应只折叠在逐题详情中。v4 在运行概览 KPI 之后新增
「SVG 生成展示 · 不评分」画廊区段（`svg_gallery`），整栏原始渲染并附题面、解析状态、
字节数、耗时与 token；提取失败降级为状态说明。无 svg 题的运行不渲染该区段。
v3 模板按原字节冻结在 `templates/v3/`，v1–v3 核验与投影不变；`script-src` 的 SHA-256
改为按所渲染模板版本的 `report_script.html` 现算，快照报告不受后续脚本编辑影响。
安全模型与免评分口径不变。

## 身份与验收

本次变更后 `scorer_sha256` 变化，历史 run 不继承 rerun/重评分资格；
`scorer_identity_mismatch` 为设计内行为，不绕过核验。
证据读取、manifest 核验、报告重建不受影响。
来源规则见[证据血缘总规则](../data-contract.md#证据血缘与比较结论)。

软件验证覆盖提取、XML/大小拒绝路径、免评分汇总、模拟服务 diagnostic 运行、
v2/v3 离线逐字节核验及浏览器 CSP/渲染；本次不进行真实模型测试或人工题包审核。

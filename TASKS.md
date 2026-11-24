# TASKS - 修复 Studio 鹈鹕测试动图展示

## 需求与根因
- 用户需求：目前的鹈鹕测试（`zh-svg-pelican`）在 Studio 无法展示动图，请修复。
- 根因 1：CLI `studio_report.py` 导出 `studio.json` 时未投影 `category == "svg"` 题目的模型生成输出。
- 根因 2：Studio 报告详情页未渲染 SVG 动图；`mygo/ui.SVG` 仅支持静态矢量，需使用 `mygo.Window.NewWebView` + `ui.WebView` 以沙箱方式播放 SMIL / CSS 动画。

## 约束与采纳决策（节点 a 审阅定稿）
1. 根目录无 `PRD.md`，严格围绕用户需求范围克制实现，不加额外抽象层或扩展点。
2. `studio.json` 保持 `schema_version = 3` 与 `kind = "studio-report.v1"`，在 `run` 上新增可选字段 `svg_outputs`（仅存在 `svg` 请求时写入），元素结构：`{ case_id, prompt, status, svg, bytes }`，其中 `status` 枚举为 `ok | not_found | malformed | too_large`，仅 `status == "ok"` 时 `svg` 为非空字符串、其余为 `null`。
3. 保持“报告只读 `studio.json`”边界：不对旧报告回退读取源运行目录；对不含 `svg_outputs` 的旧 SVG 报告在 UI 给出重新生成报告提示。
4. Studio 动图展示采用 `mygo.Window.NewWebView`（`PageOptions{Untrusted: true}`, `ScrollToContainer: true`, `BackgroundColor: "#ffffff"`）加载严格 CSP HTML（`default-src 'none'; img-src data:; style-src 'unsafe-inline';`），Go 侧校验大小上限并做 base64 编码后以 `<img src="data:image/svg+xml;base64,...">` 渲染；单 WebView 复用且在切换/离页/关闭报告/关窗时立即 `Destroy()`；源码折叠区用原生 `ui.Text` 展示。
5. 遵循工作流指令在当前分支单线串行执行，不建新分支与 worktree。

## 任务清单
- [!] **T01：CLI SVG 提取与 `studio.json` 跨端契约扩展（Python + Go + Schema）**
  - 新增 `cli/src/inferyard/reporting/svg_extract.py`：剥离 `<think>...</think>` 后确定性提取 ` ```svg ` 围栏或裸 `<svg>...</svg>`，显式拒绝 `<!DOCTYPE` / `<!ENTITY` 与非 SVG 根标签，限制 512 KiB 字符上限，不改写合法 SVG 内容。
  - 扩展 `cli/src/inferyard/reporting/studio_report.py` 与 `cli/src/inferyard/contracts/schemas_studio_report.py`，为含 `svg` 请求的 run 导出可选 `svg_outputs` 并校验跨字段不变式；运行 `scripts/export_schemas.py` 同步 `cli/schemas/studio_report.schema.json`。
  - 同步扩展 `studio/internal/inferyard/studio_report.go`、`studio_report_validate.go`、`studio_report_optional.go`，解析校验 `StudioRun.SVGOutputs` 并与导出 JSON Schema 保持一致，继续兼容无 `svg_outputs` 的旧 `studio.json`。
  - 补充 CLI 与 Go 契约单测，运行 CLI unit/ruff/schema-check 与 `go test ./internal/inferyard/...`。
  - 未解决问题（第 5 轮 Reviewer 打回）：
    1. 首行说明文字后紧跟外层 `<svg>` 且首行内含 `<title>Pelican</title>` 文本节点时，`chain_start` 被文本清空导致丢弃外层 `viewBox` 与 `<title>`、只导出第二行内层 `<svg>`（`cli/src/inferyard/reporting/svg_extract.py:189`）。
    2. 说明文字中单独提及未闭合 `<g>`（如 `I use a <g> for each wheel.\n<svg><g><circle/></g></svg>`）时，`_rewind_doc_start` 误将正文中的 `</g>` 当成说明里 `<g>` 的闭合标签而回退到错误位置导致判为 `malformed`（`cli/src/inferyard/reporting/svg_extract.py:198`）。
- [ ] **T02：Studio 报告详情页 SVG 动图渲染、生命周期管理、i18n 与 Preview**
  - 新增 `studio/internal/pages/report_svg.go`：基于 `mygo.WebView` 沙箱播放 SVG 动图（Go 端再次校验字节上限、仅拼接 base64 至 `<img>`、单 WebView 承载、多条 SVG 时支持切换）；展示题面 prompt、解析状态、字节/耗时元信息及原生 `ui.Text` 源码折叠区；无窗口或 WebView 创建失败时降级展示说明与源码；对不含 `svg_outputs` 的旧 SVG 报告展示重新生成提示。
  - 接入 `report_view.go`、`window.go`（关闭窗口时销毁 WebView）、`window_navigation.go`（离开报告页时销毁 WebView）。
  - 同步更新 `studio/internal/l10n/locales/reports/{zh-Hans,en}.json`（禁止硬编码 UI 字符串）。
  - 更新 Preview（`preview_reports.go` / `preview_fixtures.go` / `preview_i18n_test.go`）提供含真实 SMIL/CSS 动画鹈鹕 SVG 的报告预览场景。
  - 补充 Studio UI/preview 单测并运行 `mise run test:studio` 与 `mise run test:studio:preview`。
- [ ] **T03：全链路验证、文档同步检查与节点 (d) 全局终审**
  - 运行 CLI 全量测试 / ruff / schema-check，以及 `mise run test:studio`、`mise run test:studio:preview`、`mise run test:studio:e2e`。
  - 检查并按需同步受影响文档（`README.md` / `README.zh-CN.md` / `studio/README.md`）。
  - 向常驻 `advisor` 发起节点 (d) 全局终审，完成最终三份交付。

# 约束与失败行为

本页是[评分、比较与报告](../scoring-reporting.md) 的子文档，收录「约束与失败行为」「分析身份与谱系」「限制、失败模式与兼容规则」的正文；内容不变，入口保留原节标题与锚点。

## 分析身份与谱系

分析文档（`analysis.json`）的身份字段在导出与重评分中共同遵守：原始分析 `parent_analysis_id = null`、`reason = "offline_original_scoring_reduction"`；重评分保留 `source_runs`（`run_id` + `manifest_sha256`）、更新 `definition_versions.scoring` 与 `scorer_sha256`，并带 `limitations`（如 `quality_only_revision_execution_unchanged`）。重评分可以链式叠加，每代保存父代 `analysis.json` 与 `rescore.json`，核验时从来源 run 递归重放整条链；谱系深度超过 16 即拒绝（`rescore_lineage_depth_exceeded`）。任何一代的重算结果与保存字节不一致即报 `rescore_recomputation_mismatch`，不跳过后继续发布成绩。

## 限制、失败模式与兼容规则

- **纯计算边界**：评分不执行模型输出中的代码、不扩充同义答案、不先修复再解析；客户端计时用单调时钟事件，指标口径、单位与缺测原因遵循 [指标定义](../../../cli/docs/experiments/metrics.md)，本文件不复述。
- **分母与 unscorable**：`failed` 请求保留在质量分母中且按未通过计；`completed` 但缺评分或评分结果为 `unscorable` 时分组记 `unscorable` 并使整包质量值为 `null`（reason `incomplete_run`）；分母为零返回 `null` 并标 `zero_denominator`，从不以 0 冒充。取消、invalid、未执行的请求单列 `excluded`。
- **重评分不可变性**：重评分只改分析产物，不改执行终态、不改原始 run；`redacted_scoring_input` 或内容含 `[REDACTED]` 的请求直接判 `unscorable`，脱敏后无法复核的输入不得给出可复现成绩。评分器身份、父证据哈希或来源绑定不匹配即拒绝（`rescore_scorer_identity_mismatch`、`rescore_parent_recomputation_mismatch` 等）。不同评分规则版本默认禁止直接质量差值，见[证据血缘与比较结论](../../../cli/docs/data-contract.md#证据血缘与比较结论)。
- **比较资格**：`unknown` 不等于 `unknown`——任一侧未知即记 `unknown` 条件且不计入 `same`；质量、完成率、性能资格分别判定，性能资格在无开销证据时恒为 `False`；`side-by-side` 模式永不产出受控差值或因果结论；量化比较要求可核验的同源 revision；config 模式只放行 `FACTORS` 声明的冻结单因素，声明因素未实际变化记 `declared_factor_did_not_change` 阻断。比较只接受 `format_version = 4` / `phase2.v3`，旧格式不重算。
- **HTML 安全**：模板经 Jinja `autoescape` 转义题目、答案、路径与异常；唯一例外是通过检查的 SVG 以原始字节局部内嵌。SVG 提取优先完整 svg 代码围栏、否则首个闭合 `<svg>` 子串，禁止 DTD、根元素须为 SVG、上限 512×1024 字符，失败降级为转义源码展示。CSP 为 `default-src 'none'`、`style-src 'unsafe-inline'`、`script-src` 仅限同版本 `report_script.html` 的 SHA-256，禁止模型脚本与事件处理器；模板修改不得放松 `script-src`。动画元素统计（`animation_stats`）在渲染时现算，只进 HTML，不写入 `index.json` 或封存证据，不构成评分或资格结论。
- **格式与覆盖**：新报告仅接受 `report_format_version = 8`，旧 v1–v7 明确拒绝；比较只接受 format4；重评分记录只接受 format1。报告、重评分、导出、比较全部写入新目录，原件不覆盖。缺失测量显示 `—` 而非 0。
- **导出文本安全**：CSV 单元格对以 `=`、`+`、`-`、`@` 或制表/换行开头的字符串加 `'` 前缀防公式注入，数值不受影响；Markdown 导出转义 HTML 特殊字符与表格元字符。缺失值导出为空，`status` 与 `missing_reason` 保留原语义（`csv_cell`、`markdown_cell` 在 [`reporting/export.py`](../../../cli/src/inferyard/reporting/export.py)）。
- **资格证据边界**：本子的计算全部为离线软件逻辑；性能与环境资格依赖保存证据中的来源、开销与校准记录，模拟或软件验证不授予真机资格，结论血缘遵循 [数据契约](../../../cli/docs/data-contract.md#证据血缘与比较结论) 与[职责身份契约](../../../cli/docs/contracts/scoped-measurement-contract.md)。

常见拒绝路径（均为 `EvidenceError`，由 CLI 层统一映射到错误退出码，见 [CLI 与应用分发](../cli-dispatch.md)）：未封存来源要求导出（`analysis_requires_sealed_source`）、产物目录位于原始证据内（`report_output_inside_original_evidence`）、未知评分版本（`unsupported_rescore_version`）、未知比较模式或不支持的格式版本（`unsupported_comparison_mode` 等）、性能证据只给单侧或核验期间目标已变（`both_performance_overhead_sources_required`、`performance_target_changed_during_read`）、重评分 reason 缺失或超长（`rescore_reason_required`）。

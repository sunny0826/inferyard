# 运行路径

本页是[评分、比较与报告](../scoring-reporting.md) 的子文档，收录「运行路径」的正文；内容不变，入口保留原节标题与锚点。

1. **执行期评分**（入口在运行时）：`runner` / `request_execution` 构造 `ScoringContext`，`prepare` 深拷贝并校验冻结题目（structured 类预建 `Draft202012Validator`），协议完成后对答案调用 `score` 得到 score 文档并随日志落盘；终态保存与评分保存分离。异常或不完整输入产生 `unscorable` 结果，不撤销执行终态。该环节跨子系统，见[运行时执行与引擎适配](../runtime-execution.md)。
2. **离线归约**：证据账本 `read_trial` 重建 trial 视图后，`build_observations` 以质量（`summarize_quality`）与计时（`request_timing`、最近秩分位）归约生成 `summary.metric_observations`，每条观测绑定证据文件哈希；`repetition_metrics` 与 `summarize_windows` 处理多轮与分窗场景。账本与封存属于[证据、封存与离线读取](../evidence-storage.md)。
3. **报告生成**：`report` 命令经 `application/dispatch.py` 分派到 [`reporting/report.py`](../../../cli/src/inferyard/reporting/report.py) 的 `execute`，`write_report` 逐 run 归约（恰好两个 run 时自动做并列比较并拒绝重复 run）、内容按哈希去重（`text_ref`）、来源路径转为相对定位，再渲染 `report.html` 并原子写入 `index.json` / `report.html`，最后 `seal_report` 写入 `artifact-manifest.json` 封存。产物目录以 `0o700` 新建，且不得位于原始证据内部（`_new_output` 检查）。
4. **报告核验**：`verify --path REPORT` 与 `report-check` 走 `verify_report` → `sealed_report.verify`，核对封存字节与来源语义，只保留重算所需字节；`--rerender` 额外用已核验的 HTML 字节重渲染并与保存结果比对。来源单独移动时用 `--source-root OLD=NEW` 显式映射，保存文件不变（见 [reports.md](../../../cli/docs/reports.md)）。
5. **比较产物**：`compare` 命令加载两份 `comparison_input`（核验指标引用与 manifest 身份），`build_comparison` 要求比较格式为 4、性能证据两侧同时提供或同时缺失且目标 manifest 未变，`compare_trials` 逐字段产出 `conditions`、`blockers` 与分维度 `eligibility`；`compare-check` 离线重算并核验。
6. **重评分**：`rescore` 命令以 `--scorer` 选择评分版本、`--reason` 为必填（非空且 ≤500 字符），`build_rescore` 只对 `completed` 请求重打分并生成新的 `analysis_id`；父分析以 `parent_analysis_id` 绑定，谱系通过 `parent-lineage.json` 递归重放（深度上限 16）。`rescore-check` 从来源证据完整重建并逐字节比对。
7. **导出与公开发布**：`export` 从已封存 run 生成 `analysis.json`、`metrics.csv`、`metrics.md`（源 manifest 未封存时拒绝），或从既有分析目录导出；`export-check` 核验。`public-package` 经 `project_metrics` 只发布白名单内的文本，其余字符串以 SHA-256 指纹代替并标 `redacted`。
8. **重复实验报告**：`repeat-summary` 命令对同一计划的多个 run 写出重复摘要（`write_repetition_summary`），逐轮值、有效样本与范围只描述已观测轮次，不拼装未完成的轮次；`repeat-check` 离线核验。

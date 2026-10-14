# 评分、比较与报告

确定性评分、指标归约、比较资格判定、重评分、离线 HTML 报告与导出的实现子系统，覆盖 `analysis/` 与 `reporting/` 两个包。评分输入是冻结题目、参考答案与 answer policy，输出是带身份哈希的 score 文档与指标观测；本子系统只读取已保存证据，不发送模型请求、不调用模型裁判。运行期评分的调用方在执行子系统（见[运行时执行与引擎适配](runtime-execution.md)），日志、封存与账本重建在证据子系统（见[证据、封存与离线读取](evidence-storage.md)），命令入口在[CLI 与应用分发](cli-dispatch.md)）。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 确定性评分 | 按题型纯计算规则打分，无执行、无修复、无网络 | `score_case`、`ScoringContext`、`scorer_hash`、`SCORER_VERSION` 在 [`analysis/scoring.py`](../../cli/src/inferyard/analysis/scoring.py)；规则求值在 `score_rules`（[`analysis/scoring_rules.py`](../../cli/src/inferyard/analysis/scoring_rules.py)） |
| 评分修订版本 | 追加评分器版本（当前 `phase2.v2`），复用原始规则并改写诊断 | `VERSION`、`resolve`、`revise` 在 [`analysis/scoring_revision.py`](../../cli/src/inferyard/analysis/scoring_revision.py) |
| 质量归约 | Q01–Q08 汇总、分母与 unscorable 处理、分类混淆矩阵 | `summarize_quality` 在 [`analysis/quality.py`](../../cli/src/inferyard/analysis/quality.py)；`rate`、`distribution` 在 [`analysis/aggregate.py`](../../cli/src/inferyard/analysis/aggregate.py) |
| 延迟归约 | E2E/首事件计时、`budget_exhausted` 语义、完成分布 | `request_timing`、`summarize_performance` 在 [`analysis/performance.py`](../../cli/src/inferyard/analysis/performance.py)；引擎 prefill/decode 速率在 `engine_rates`（[`analysis/engine_timing.py`](../../cli/src/inferyard/analysis/engine_timing.py)） |
| 资源归约 | 样本窗口选择、计数差与回绕、基线 RSS、正式窗口峰值 | `reduce_resources`、`counter_delta`、`baseline_rss` 在 [`analysis/resource_metrics.py`](../../cli/src/inferyard/analysis/resource_metrics.py)；早晚分窗在 `summarize_windows`（[`analysis/duration_windows.py`](../../cli/src/inferyard/analysis/duration_windows.py)） |
| 指标观测 | 版本化 `metric_observations`、分组、缺测原因、最近秩分位 | `Observations`、`build_observations` 在 [`analysis/observations.py`](../../cli/src/inferyard/analysis/observations.py)；指标定义经 `catalogue("metrics")`（[`registry.py`](../../cli/src/inferyard/registry.py)） |
| 重复与扫描 | 多轮一致性、逐轮值与范围、长度/位置扫描汇总 | `repetition_metrics` 在 [`analysis/repetition_metrics.py`](../../cli/src/inferyard/analysis/repetition_metrics.py)；重复实验报告在 [`reporting/repetition_report.py`](../../cli/src/inferyard/reporting/repetition_report.py)；长度与输入目标核对在 `input_length_summary`、`check_input_target`（[`analysis/input_lengths.py`](../../cli/src/inferyard/analysis/input_lengths.py)）；材料位置在 `position_summary`（[`analysis/position.py`](../../cli/src/inferyard/analysis/position.py)）；缓存序列在 `cache_series`（[`analysis/cache_observations.py`](../../cli/src/inferyard/analysis/cache_observations.py)） |
| 环境与稳定性观测 | 起止环境评估与资格、采集调度、分窗漂移与传感器观测 | `assess_environment`、`environment_qualification` 在 [`analysis/environment.py`](../../cli/src/inferyard/analysis/environment.py)；边界环境在 [`analysis/boundary_environment.py`](../../cli/src/inferyard/analysis/boundary_environment.py)；开销环境对照在 [`analysis/overhead_environment.py`](../../cli/src/inferyard/analysis/overhead_environment.py)；漂移与传感器在 [`analysis/stability_observations.py`](../../cli/src/inferyard/analysis/stability_observations.py)、[`analysis/sensor_observations.py`](../../cli/src/inferyard/analysis/sensor_observations.py) |
| 公开指标投影 | 白名单词表外的文本以哈希代替并标记 redacted | `project_metrics`、`VOCABULARY` 在 [`analysis/public_metrics.py`](../../cli/src/inferyard/analysis/public_metrics.py) |
| 比较计算 | 双侧条件核对、资格判定、质量/性能差值 | `compare_trials`、`quality_differences`、`FACTORS` 在 [`analysis/comparison.py`](../../cli/src/inferyard/analysis/comparison.py)；未知值判定在 [`analysis/comparison_helpers.py`](../../cli/src/inferyard/analysis/comparison_helpers.py)；专项门槛在 `qualification`（[`analysis/quantization_comparison.py`](../../cli/src/inferyard/analysis/quantization_comparison.py)、[`analysis/cache_comparison.py`](../../cli/src/inferyard/analysis/cache_comparison.py)）与 `performance_comparison`（[`analysis/performance_comparison.py`](../../cli/src/inferyard/analysis/performance_comparison.py)）、`observed_differences`（[`analysis/observed_comparison.py`](../../cli/src/inferyard/analysis/observed_comparison.py)） |
| 报告构建 | 多 run 索引、自包含 HTML、封存与核验 | `write_report`、`verify_report`、`build_index` 在 [`reporting/report.py`](../../cli/src/inferyard/reporting/report.py)；渲染与输出保护在 `render_report_html`、`_environment`、`_new_output`（[`reporting/report_common.py`](../../cli/src/inferyard/reporting/report_common.py)）；内容引用去重在 [`reporting/report_content.py`](../../cli/src/inferyard/reporting/report_content.py)；模板摘要在 `template_hash`（[`reporting/report_assets.py`](../../cli/src/inferyard/reporting/report_assets.py)）；封存在 `seal_report`、`verify`（[`reporting/sealed_report.py`](../../cli/src/inferyard/reporting/sealed_report.py)） |
| 报告视图 | 概览 KPI、逐题详情、profile、资源曲线与扫描区段 | `dashboard_view` 在 [`reporting/report_dashboard.py`](../../cli/src/inferyard/reporting/report_dashboard.py)；`profile_view` 在 [`reporting/report_profile.py`](../../cli/src/inferyard/reporting/report_profile.py)；其余分区块在 `report_duration.py`、`report_facets.py`、`report_resources.py`、`report_scans.py`（同目录） |
| SVG 展示 | 从模型输出提取 SVG、XML/大小检查、渲染时动画统计 | `extract_svg`、`check_svg`、`animation_stats`、`MAX_SVG_CHARACTERS` 在 [`reporting/svg_render.py`](../../cli/src/inferyard/reporting/svg_render.py) |
| 重评分 | 独立分析身份、谱系重放、逐请求变化记录 | `build_rescore`、`write_rescore`、`verify_rescore`、`replay_lineage`、`rescore_rows` 在 [`reporting/rescore.py`](../../cli/src/inferyard/reporting/rescore.py) |
| 导出与比较产物 | `analysis.json` + CSV/Markdown 导出；comparison4 构建与核验 | `trial_analysis`、`formatted_files`、`write_export` 在 [`reporting/export.py`](../../cli/src/inferyard/reporting/export.py)；`build_comparison`、`verify_comparison`、`comparison_input` 在 [`reporting/comparison_report.py`](../../cli/src/inferyard/reporting/comparison_report.py) |
| 公开发布包 | 脱敏公开副本的投影、写出与核验 | `projection`、`write_public`、`verify_public` 在 [`reporting/public_package.py`](../../cli/src/inferyard/reporting/public_package.py) |
| 命令接入 | `report`/`compare`/`rescore`/`export`/`public-package` 及 `*-check` 延迟分派 | [`application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py) |

关键身份与版本的事实在此处定义一次：评分器身份是 `scorer_hash()` 对固定源码文件列表、`jsonschema` 版本与 `unicodedata` 版本的 SHA-256，修订版身份在其上叠加 `scoring_revision.py` 字节；`analysis_id` 由来源绑定与生成者哈希派生（原始分析为 `trial-` 前缀，重评分为 `rescore-` 前缀）。当前格式版本：报告 `report_format_version = 8`、比较产物 `format_version = 4`（定义版本 `phase2.v3`）、重评分记录 `format_version = 1`，均要求 `schema_version = 3`。字段语义与格式规则见 [数据契约](../../cli/docs/data-contract.md) 与 [报告契约](../../cli/docs/contracts/report-contract.md)，不在这里复制。

`reporting/engine_fit*.py` 是 engine-fit 诊断产物与绑定核验的报告侧，职责归 [engine-fit 引擎诊断](engine-fit.md)，不在本参考展开。

## 运行路径

1. **执行期评分**（入口在运行时）：`runner` / `request_execution` 构造 `ScoringContext`，`prepare` 深拷贝并校验冻结题目（structured 类预建 `Draft202012Validator`），协议完成后对答案调用 `score` 得到 score 文档并随日志落盘；终态保存与评分保存分离。异常或不完整输入产生 `unscorable` 结果，不撤销执行终态。该环节跨子系统，见[运行时执行与引擎适配](runtime-execution.md)。
2. **离线归约**：证据账本 `read_trial` 重建 trial 视图后，`build_observations` 以质量（`summarize_quality`）与计时（`request_timing`、最近秩分位）归约生成 `summary.metric_observations`，每条观测绑定证据文件哈希；`repetition_metrics` 与 `summarize_windows` 处理多轮与分窗场景。账本与封存属于[证据、封存与离线读取](evidence-storage.md)。
3. **报告生成**：`report` 命令经 `application/dispatch.py` 分派到 [`reporting/report.py`](../../cli/src/inferyard/reporting/report.py) 的 `execute`，`write_report` 逐 run 归约（恰好两个 run 时自动做并列比较并拒绝重复 run）、内容按哈希去重（`text_ref`）、来源路径转为相对定位，再渲染 `report.html` 并原子写入 `index.json` / `report.html`，最后 `seal_report` 写入 `artifact-manifest.json` 封存。产物目录以 `0o700` 新建，且不得位于原始证据内部（`_new_output` 检查）。
4. **报告核验**：`verify --path REPORT` 与 `report-check` 走 `verify_report` → `sealed_report.verify`，核对封存字节与来源语义，只保留重算所需字节；`--rerender` 额外用已核验的 HTML 字节重渲染并与保存结果比对。来源单独移动时用 `--source-root OLD=NEW` 显式映射，保存文件不变（见 [reports.md](../../cli/docs/reports.md)）。
5. **比较产物**：`compare` 命令加载两份 `comparison_input`（核验指标引用与 manifest 身份），`build_comparison` 要求比较格式为 4、性能证据两侧同时提供或同时缺失且目标 manifest 未变，`compare_trials` 逐字段产出 `conditions`、`blockers` 与分维度 `eligibility`；`compare-check` 离线重算并核验。
6. **重评分**：`rescore` 命令以 `--scorer` 选择评分版本、`--reason` 为必填（非空且 ≤500 字符），`build_rescore` 只对 `completed` 请求重打分并生成新的 `analysis_id`；父分析以 `parent_analysis_id` 绑定，谱系通过 `parent-lineage.json` 递归重放（深度上限 16）。`rescore-check` 从来源证据完整重建并逐字节比对。
7. **导出与公开发布**：`export` 从已封存 run 生成 `analysis.json`、`metrics.csv`、`metrics.md`（源 manifest 未封存时拒绝），或从既有分析目录导出；`export-check` 核验。`public-package` 经 `project_metrics` 只发布白名单内的文本，其余字符串以 SHA-256 指纹代替并标 `redacted`。
8. **重复实验报告**：`repeat-summary` 命令对同一计划的多个 run 写出重复摘要（`write_repetition_summary`），逐轮值、有效样本与范围只描述已观测轮次，不拼装未完成的轮次；`repeat-check` 离线核验。

## 约束与失败行为

### 分析身份与谱系

分析文档（`analysis.json`）的身份字段在导出与重评分中共同遵守：原始分析 `parent_analysis_id = null`、`reason = "offline_original_scoring_reduction"`；重评分保留 `source_runs`（`run_id` + `manifest_sha256`）、更新 `definition_versions.scoring` 与 `scorer_sha256`，并带 `limitations`（如 `quality_only_revision_execution_unchanged`）。重评分可以链式叠加，每代保存父代 `analysis.json` 与 `rescore.json`，核验时从来源 run 递归重放整条链；谱系深度超过 16 即拒绝（`rescore_lineage_depth_exceeded`）。任何一代的重算结果与保存字节不一致即报 `rescore_recomputation_mismatch`，不跳过后继续发布成绩。

### 限制、失败模式与兼容规则

- **纯计算边界**：评分不执行模型输出中的代码、不扩充同义答案、不先修复再解析；客户端计时用单调时钟事件，指标口径、单位与缺测原因遵循 [指标定义](../../cli/docs/experiments/metrics.md)，本文件不复述。
- **分母与 unscorable**：`failed` 请求保留在质量分母中且按未通过计；`completed` 但缺评分或评分结果为 `unscorable` 时分组记 `unscorable` 并使整包质量值为 `null`（reason `incomplete_run`）；分母为零返回 `null` 并标 `zero_denominator`，从不以 0 冒充。取消、invalid、未执行的请求单列 `excluded`。
- **重评分不可变性**：重评分只改分析产物，不改执行终态、不改原始 run；`redacted_scoring_input` 或内容含 `[REDACTED]` 的请求直接判 `unscorable`，脱敏后无法复核的输入不得给出可复现成绩。评分器身份、父证据哈希或来源绑定不匹配即拒绝（`rescore_scorer_identity_mismatch`、`rescore_parent_recomputation_mismatch` 等）。不同评分规则版本默认禁止直接质量差值，见[证据血缘与比较结论](../../cli/docs/data-contract.md#证据血缘与比较结论)。
- **比较资格**：`unknown` 不等于 `unknown`——任一侧未知即记 `unknown` 条件且不计入 `same`；质量、完成率、性能资格分别判定，性能资格在无开销证据时恒为 `False`；`side-by-side` 模式永不产出受控差值或因果结论；量化比较要求可核验的同源 revision；config 模式只放行 `FACTORS` 声明的冻结单因素，声明因素未实际变化记 `declared_factor_did_not_change` 阻断。比较只接受 `format_version = 4` / `phase2.v3`，旧格式不重算。
- **HTML 安全**：模板经 Jinja `autoescape` 转义题目、答案、路径与异常；唯一例外是通过检查的 SVG 以原始字节局部内嵌。SVG 提取优先完整 svg 代码围栏、否则首个闭合 `<svg>` 子串，禁止 DTD、根元素须为 SVG、上限 512×1024 字符，失败降级为转义源码展示。CSP 为 `default-src 'none'`、`style-src 'unsafe-inline'`、`script-src` 仅限同版本 `report_script.html` 的 SHA-256，禁止模型脚本与事件处理器；模板修改不得放松 `script-src`。动画元素统计（`animation_stats`）在渲染时现算，只进 HTML，不写入 `index.json` 或封存证据，不构成评分或资格结论。
- **格式与覆盖**：新报告仅接受 `report_format_version = 8`，旧 v1–v7 明确拒绝；比较只接受 format4；重评分记录只接受 format1。报告、重评分、导出、比较全部写入新目录，原件不覆盖。缺失测量显示 `—` 而非 0。
- **导出文本安全**：CSV 单元格对以 `=`、`+`、`-`、`@` 或制表/换行开头的字符串加 `'` 前缀防公式注入，数值不受影响；Markdown 导出转义 HTML 特殊字符与表格元字符。缺失值导出为空，`status` 与 `missing_reason` 保留原语义（`csv_cell`、`markdown_cell` 在 [`reporting/export.py`](../../cli/src/inferyard/reporting/export.py)）。
- **资格证据边界**：本子的计算全部为离线软件逻辑；性能与环境资格依赖保存证据中的来源、开销与校准记录，模拟或软件验证不授予真机资格，结论血缘遵循 [数据契约](../../cli/docs/data-contract.md#证据血缘与比较结论) 与[职责身份契约](../../cli/docs/contracts/scoped-measurement-contract.md)。

常见拒绝路径（均为 `EvidenceError`，由 CLI 层统一映射到错误退出码，见 [CLI 与应用分发](cli-dispatch.md)）：未封存来源要求导出（`analysis_requires_sealed_source`）、产物目录位于原始证据内（`report_output_inside_original_evidence`）、未知评分版本（`unsupported_rescore_version`）、未知比较模式或不支持的格式版本（`unsupported_comparison_mode` 等）、性能证据只给单侧或核验期间目标已变（`both_performance_overhead_sources_required`、`performance_target_changed_during_read`）、重评分 reason 缺失或超长（`rescore_reason_required`）。

## 验证

以下命令在仓库根执行；根级 mise tasks 已内置 `cd cli`（见 [mise.toml](../../mise.toml) 与 [cli/pyproject.toml](../../cli/pyproject.toml) 的 pytest/ruff 配置）：

```sh
# 全量单元与集成回归（pytest，testpaths = tests）
mise run test

# Ruff 检查源码、测试与脚本
mise run lint

# JSON Schema 与 catalogue 生成物漂移核对
mise run schemas
mise exec -- uv run --frozen python scripts/export_catalogue.py --check

# 评分、比较与重评分回归
mise exec -- uv run --frozen pytest \
  tests/unit/test_scoring.py \
  tests/unit/test_scoring_contract.py \
  tests/unit/test_scoring_revision.py \
  tests/unit/test_rescore.py \
  tests/unit/test_comparison.py \
  tests/unit/test_performance_comparison.py \
  tests/unit/test_quantization_comparison.py \
  tests/unit/test_cache_comparison.py \
  tests/unit/test_observations.py \
  tests/unit/test_repetition_metrics.py

# 报告、SVG 与导出回归
mise exec -- uv run --frozen pytest \
  tests/unit/test_report_content.py \
  tests/unit/test_report_dashboard.py \
  tests/unit/test_report_facets.py \
  tests/unit/test_report_resources.py \
  tests/unit/test_report_scans.py \
  tests/unit/test_svg_render.py \
  tests/unit/test_export.py \
  tests/unit/test_export_utf8.py \
  tests/unit/test_public_metrics.py
```

`cli/scripts/` 下 `verify_*` 专项脚本会启动服务或发送模型请求，不属于本子系统的离线回归，未列入；catalogue 生成源（指标/方法定义）变更后才需要重跑 `export_catalogue.py` 并更新生成物。

## 与既有文档的差异

- [实验设计](../../cli/docs/experiments/design.md) 的比较模式表列出 `device` / `engine` 两种模式，但 `compare_trials` 只接受 `model` / `config` / `side-by-side`，其余模式抛 `unsupported_comparison_mode`；设备与引擎作为被测因素的条件扩展由 engine-fit 比较路径处理，不经本子系统的 `compare` 命令。
- [报告契约](../../cli/docs/contracts/report-contract.md) 的 CSP 一节描述 `default-src 'none'; style-src 'unsafe-inline'` 与 `script-src` 哈希，实际模板 meta 还含 `img-src data:`、`base-uri 'none'`、`form-action 'none'`；契约只列安全关键指令，语义不冲突。

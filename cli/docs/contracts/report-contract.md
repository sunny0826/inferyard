# 报告契约（studio.json）

历史依据：[ADR 011](../decisions/011-report-dashboard.md)、[ADR 033](../decisions/033-svg-generative-category.md)、
[ADR 037](../decisions/037-offline-evidence-reading.md) 与 [ADR 045](../decisions/045-report-content-refs.md)；
离线 HTML 展示、呈现封存与报告核验由 [ADR 059](../decisions/059-studio-json-only-report.md) 退役。

报告目录的唯一必需文件是 `studio.json`：`schema_version = 3`、`kind = "studio-report.v1"`，registry key 为
`studio_report`。结构源在 `contracts/schemas_studio_report.py`，导出产物为 `cli/schemas/studio_report.schema.json`
（由 `scripts/export_schemas.py` 生成，不手改）。不再使用 `report_format_version`，不生成 `index.json`、
`report.html` 或 `artifact-manifest.json`。

## 字段与语义

`runs` 为 1–100 项，按 `source.run_id` 唯一；`requests` 可为空数组。所有对象 required 键齐全、
`additionalProperties = false`；nullable 字段显式写 `null`，缺测不填 0。计数为整数；耗时单位为毫秒，
内存为字节（由 MiB 换算，可为浮点）。

- `source.run_id` / `source.path`：来源 run 标识与定位。`source.path` 只是回链定位，不是内嵌证据或完整性承诺。
- `model`、`threads`、`diagnostic`、`facets.bundle_label`：run 身份与题包标签。
- `profile`：`title`、`engine`（binary_path/adapter/backend 三键）、`machine`（cpu/logical_cpus/memory_total_bytes 三键）。
  硬件参数属于测评时快照，缺失为 null，不用当前机器或默认值填充；统一内存不加到 RSS / GPU 内存中。
- `summary`：完成率 `numerator/denominator/excluded` 与 `stop_reason`；使用核心原分母，不因显示筛选改变。
- `dashboard.quality`：六类质量计数之和与 `complete`/`reason`；`complete` 只表示展示质量输入是否完整，
  不是证据 verified；`reason` 恰在不完整时出现。
- `dashboard.categories[]`：仅六类质量分类（instruction/extraction/qa/math/classification/structured），
  id 数组内唯一，不合成总体质量分数。
- `dashboard.latency`：`min/p50/p95/max`（最近秩）与 `sample_count/excluded`；只对完成且计时完整的请求采样，
  `sample_count + excluded == len(requests)`；不足 20 个样本时 `p95` 为 null；空样本四个分位值均为 null。
  每题耗时取 `request_timing()` 的 L03 value。
- `dashboard.sampled_peak_rss_bytes` / `sampled_minimum_available_bytes` 及成对 reason：采样峰值不能称连续峰值；
  多来源或缺测给出原因，不取跨来源极值。
- `requests[]`：`case_id`、`category_label`（未知类别回退原标签）、`state`（五种终态）、
  `quality`（含 `not_applicable`，无 score 时为 null）、`latency_ms`。

JSON Schema 只证明结构；run/category 唯一性、计数一致性、null/reason 成对与分布约束由 parser 执行。

## 边界

- 不进入报告的字段：题面/prompt、原始答案、reference answer、reasoning、正文 contents、SVG gallery、
  资源曲线/坐标、证据 URL、完整运行配置。相应内容保留在原始证据中，Studio 不借旧字段隐式访问。
- `studio.json` 是可重建的展示投影，不是可核验产物：`verify --path REPORT` 固定返回
  `studio_report_not_verifiable` / 退出 4，不写文件。证据权威是原始 run/batch/plan 及其 manifest。
- 旧版报告（report_format_version v1–v8）明确拒绝、不迁移、不转换：可识别版本返回
  `unsupported_format` / 退出 2 且 supported 为空；损坏 JSON、坏 hash/seal、身份冲突为退出 4。
  带已知旧封存的输入先经有限字节检查再拒绝；无封存的可识别版本只作有限识别，不声称完整。
  规则细节见[离线读取契约](offline-reading-contract.md)。

## 描述性比较

比较产物为 `comparison.json`：`schema_version = 3`、`format_version = 5`，计算 definition 保持
`phase2.v3`，校准适用域见[职责身份契约](scoped-measurement-contract.md)。
observed_differences 展示同口径完成率/质量观测、逐题记录、条件差异与样本范围。
质量总体差值要求相同题目内容、答案政策、规则、协议、评分器、分母及完整评分。
性能先展示匹配定义/来源/单位的双侧值；无开销/环境/逐指标资格时差值仍为 null。
eligibility 继续表达受控结论资格，不用描述性字段授予因果或性能结论。
`verify` 对 comparison 从来源重算 source ref、hash、performance evidence 与规范 JSON；
关联比较仅接受 format5，旧 format1–4 返回 unsupported_format / 2。

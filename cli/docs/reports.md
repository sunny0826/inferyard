# 查看测评报告

`inferyard report` 从保存的运行证据投影出供 Studio 阅读的报告，不连接模型服务。生成时另建目录，原始答案、分数和历史产物保留。

```bash
inferyard report --runs results/RUN_ID --out reports/NEW_REPORT
```

报告目录只含一个 `studio.json`，由 InferYard Studio 的报告页读取；字段、单位与 nullable 规则见[报告契约](contracts/report-contract.md)。`studio.json` 是可重建的展示投影：`source.path` 只是来源定位，不是内嵌证据或完整性承诺；Studio 不重新评分，成功读取也不称为 verified。证据权威仍是原始 run/batch/plan 及其 manifest，`verify` 继续覆盖它们；对报告目录执行 `verify` 固定返回 `studio_report_not_verifiable` / 退出 4。

报告包含私有路径等本机信息，公开发布使用已有 `public` 命令流程。旧版报告（`index.json` / `report.html` / `report_format_version = 8` 及更早）明确拒绝、不迁移：可识别旧版本返回 `unsupported_format` / 退出 2，损坏输入为退出 4；分类细节见[离线读取契约](contracts/offline-reading-contract.md)。需要新格式时，从仍有效的原始 run 在新目录重新派生。

## 页面内容（Studio）

- **运行概览**：完成率、排除计数、停止原因与质量计数；缺测为 `null` 并附原因，不是 0。
- **模型与机器**：模型名、引擎 adapter/backend/binary、CPU、逻辑核数与内存总量快照。统一内存不能与 GPU 内存重复相加。
- **分类与耗时**：六类质量分类计数、完成请求耗时的 min/P50/P95/max；耗时只对完成且计时完整的请求采样，不足 20 个样本时 P95 为 `null`。
- **内存卡片**：服务采样峰值 RSS 与系统可用内存最小值；多来源或缺测时给出原因，不混算极值。
- **逐题明细**：`case_id`、分类标签、执行终态、质量与单题耗时。题面、原始答案、reference answer、reasoning、SVG 画廊与资源曲线不进入报告；相应内容仍在原始证据中。

`dashboard.quality.complete` 只表示展示质量输入是否完整，不是证据 verified。计数为整数；耗时为毫秒、内存为字节（由 MiB 换算）。

## 报告格式

当前报告为 `schema_version = 3`、`kind = "studio-report.v1"`，registry key 为 `studio_report`，导出 Schema 为 `cli/schemas/studio_report.schema.json`。报告不再携带 `report_format_version`，没有 `index.json`、`report.html` 或 `artifact-manifest.json`。历史设计依据见 [ADR 011](decisions/011-report-dashboard.md) 与 [ADR 045](decisions/045-report-content-refs.md)，本次退役决策见 [ADR 059](decisions/059-studio-json-only-report.md)。

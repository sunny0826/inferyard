# 查看离线测评报告

`inferyard report` 从保存的运行证据生成模型测评档案，不连接模型服务。生成时另建目录，原始答案、分数和历史 HTML 保留。

```bash
inferyard report --runs results/RUN_ID --out reports/NEW_REPORT
inferyard verify --path reports/NEW_REPORT
```

用浏览器打开 `reports/NEW_REPORT/report.html`。样式、脚本和图表全部内联，单独复制这个 HTML 也可断网阅读和检索；核验与重建仍需要 `index.json` 和原始来源目录，当前报告还需要 `artifact-manifest.json`。报告包含私有路径、原始输出等本机证据，公开发布使用已有 `public` 命令流程。

新报告按相对路径引用来源，一起移动目录树后可直接核验。来源单独移动时，使用
`verify --path REPORT --source-root OLD=NEW` 显式映射；保存文件不变。
默认核验封存字节和来源语义；`--rerender` 另检查当前渲染结果。
旧版本仍按原规则核验，详见[离线读取契约](contracts/offline-reading-contract.md)。

## 页面内容

- **运行概览**：已记录通过数、完成率、完成请求中位耗时和服务采样峰值 RSS；分别呈现答案质量、运行完整性与比较资格。
- **模型与机器**：模型文件、量化、来源 / revision、文件大小、引擎、CPU、GPU、系统、内存与电源快照。统一内存不能与 GPU 内存重复相加；名称中的 27B 不视作已核验结构参数量。
- **生成与执行参数**：配置值和核验的实际值分列，显示核验来源；上下文、输出预算、线程、并发、槽位、缓存、超时、预热及采样周期另列。
- **测试结果与资源**：六类质量率及其原分母、五种执行终态、逐题耗时、P50 / P95、首次答案时间、独立采集来源的 CPU / RSS / 可用内存曲线；温度等传感器可展开。
- **逐题答案与数据来源**：按题目、内容、题型和结果筛选；查看参考答案、原始输出、逐规则评分、token、停止原因，以及运行 / manifest / 模型 / 源码的哈希和文件链接。

筛选只改变显示，成绩与原分母保持不变。点击结果格子会展开对应题目；“打印 / 保存 PDF”使用浏览器打印，已展开的题目保留详情。JavaScript 关闭时全部内容仍可阅读和手动展开，筛选不可用。

`—` 表示缺测，不是 0。机器参数是测试时保存的快照；缺失或旧证据中的非数值硬件标签不会用当前机器补齐。耗时分位数采用最近秩，仅计完成且时间完整的请求；不足 20 项不显示 P95，20–99 项标为探索性。资源峰值来自离散采样，多个来源不混算。

## 报告格式

当前新报告为 `report_format_version = 8`，核心仍为 `schema_version = 3`。仅接受 v8；旧报告 v1–v7 和核心旧 v1/v2 明确拒绝，原字节留在原项目。同一段题面、输出或参考答案在报告内只存一次，页面按该份文本显示。字段来源及限制见[报告契约](contracts/report-contract.md)，封存与核验见[离线读取契约](contracts/offline-reading-contract.md)，历史设计依据见 [ADR 011](decisions/011-report-dashboard.md) 与 [ADR 045](decisions/045-report-content-refs.md)。

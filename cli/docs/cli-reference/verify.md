# verify 的核验范围

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「verify 的核验范围」的正文；内容不变，入口保留原节标题与锚点。

`engine-fit verify` 专用于[引擎适配诊断](../engine-fit-common.md)的 run/比较产物；
普通 `verify` 支持下面八类派生产物，以及原始 run/batch、冻结 plan 和 engine-fit 产物。
规划、执行、比较的全部新选项见 `engine-fit <动作> --help`。

八类产物为开销对照（overhead）、重复分析（repeat）、重评分（rescore）、指标导出（export）、公开包（public）、报告（report）、比较（comparison）和扩展包（extension）。

- 使用特有文件及必要元数据识别；损坏、无法识别或类型冲突均拒绝，不回退到只核验 manifest。
- comparison 与 report 共存时应核验两者；extension 内嵌报告由 packet 验证器负责。
- 原始 run/batch、冻结 plan 由原读取器核验；单独 analysis 仍不属于此入口。
- `--config` 和 `--source-run` 仅用于 public，可同时提供；`--target-run` 仅用于 overhead。不适用选项返回输入阻断。
- 配置匹配只核对本地声明，不要求当前端点/PID，也不证明就绪；`ready_to_run` 仍为 false。配置不匹配、开销阈值或绑定未通过时保持退出码 3。
- 结果保留专用验证器的状态、完整性、限制和退出码，details 附 `artifact_type`；组合比较包含 comparison 与 report 两项结果。

核验只读，不发送模型请求，不授予当前测量或性能比较资格。报告、比较、重评分、导出和两类开销协议有不同输入与来源规则，不能因共享入口而合并语义。

`report` 的[离线 HTML](../reports.md)包含模型 / 机器档案、生成参数核验、分类成绩、独立来源资源曲线与逐题检索。新产物使用报告格式 v8，核心 schema 仍为 3；仅接受 v8；v1–v7 返回 unsupported_format/2，旧数据回原项目处理。

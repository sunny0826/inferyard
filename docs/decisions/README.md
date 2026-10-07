# 架构决策

本目录只保留长期设计理由，编号稳定、不补齐删除后的空号。现行字段以[主题契约](../contracts/README.md)为准，进度见[backlog](../backlog.md)。
Accepted 表示采纳设计，不代表所有平台已验证。

| 决策 | 状态 |
| --- | --- |
| [ADR 001：单一活动契约与显式迁移](001-unified-contract.md) | Accepted |
| [ADR 002：领域包与薄 CLI](002-cli-architecture.md) | Accepted |
| [ADR 003：统一核验与命令分组](003-cli-surface.md) | Accepted |
| [ADR 004：设备检测的呈现与平台边界](004-device-check.md) | Accepted |
| [ADR 005：macOS 原生执行](005-macos-runtime.md) | Accepted |
| [ADR 006：macOS 原生来源与完整采集开销](006-macos-performance-prerequisites.md) | Accepted |
| [ADR 007：同次 macOS 身份核验共用新鲜进程文件视图](007-macos-process-view.md) | Accepted |
| [ADR 008：macOS 环境查询与同次换页读取](008-macos-environment-reads.md) | Accepted |
| [ADR 009：独立单文件系统与进程监测器](009-native-observer.md) | Accepted |
| [ADR 010：监测器与测评结果只读适配](010-observer-benchmark-adaptation.md) | Accepted |
| [ADR 011：自包含离线测评报告](011-report-dashboard.md) | Accepted |
| [ADR 012：同机同模型引擎适配诊断](012-engine-fit.md) | Accepted |
| [ADR 013：macOS 原生 engine-fit](013-engine-fit-macos.md) | Accepted |
| [ADR 014：常用引擎的显式观察接口](014-macos-common-engines.md) | Accepted |
| [ADR 015：LM Studio 本机 CLI 身份](015-lmstudio-local-cli-identity.md) | Accepted |
| [ADR 016：显式冻结临时温度停止覆盖](016-engine-fit-temperature-override.md) | Accepted |
| [ADR 017：MLX 总序列上限与线程本地 GPU stream](017-macos-hf-three-engines.md) | Accepted |
| [ADR 018：持续负载在客户端发送起点准入](018-duration-send-admission.md) | Accepted |
| [ADR 019：显式冻结临时内存停止覆盖](019-engine-fit-memory-override.md) | Accepted |
| [ADR 020：Windows 原生 engine-fit 诊断](020-windows-engine-fit-native.md) | Accepted |
| [ADR 031：源码获取由一个宿主保管进程](031-source-acquisition-process-owner.md) | Accepted |
| [ADR 032：Windows Prism 批量执行与原生资源采集](032-windows-prism-batch-run.md) | Accepted |
| [ADR 033：SVG 生成题与报告 v3](033-svg-generative-category.md) | Accepted |
| [ADR 034：命令内复用与显式预算快照](034-command-local-reuse.md) | Accepted |
| [ADR 035：按用途区分描述、执行与审核条件](035-purpose-specific-admission.md) | Accepted |
| [ADR 036：按职责绑定身份、持久化与校准适用域](036-scoped-measurement-cost.md) | Accepted |
| [ADR 037：原始证据读取与封存呈现分离](037-offline-evidence-reading.md) | Accepted |
| [ADR 038：InferYard 当前格式边界与主机状态迁移](038-inferyard-current-format.md) | Proposed；T0 待审核 |

ADR 038 列明拟局部取代的兼容承诺及继续有效的安全规则；用户已授权清理范围，实施仍须通过 T0 门。
锁方案优先评估一次性迁移与固定旧入口退休，持续桥接仅为对照；安装结果 scope 变化拟独立升版。

证据解释统一遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。设备专属实验与失效提案已移出当前文档，原历史仍可由 Git 追溯。

# 主题契约

[数据契约](../data-contract.md)定义活动 v3、来源与迁移；下列文档各自负责专用接口。

| 职责 | 契约 |
| --- | --- |
| 当前格式清理与锁迁移（P0 已批准） | [格式矩阵、旧入口退休事务与安装资格](inferyard-current-format.md) |
| 执行窗口 | [持续负载发送准入](duration-send-contract.md) |
| 平台身份与采集 | [macOS](macos-contract.md) · [Windows Prism 批量](windows-prism-batch-contract.md) |
| 引擎适配 | [engine-fit](engine-fit-contract.md) · [Windows KVMem/NInfer](windows-ninfer-adaptation-contract.md) |
| 观测与原生接口 | [lab 解析与 Windows 身份](windows-ninfer-adaptation-t0.md) · [独立 observer](observer-contract.md) |
| 分析与呈现 | [职责身份与校准](scoped-measurement-contract.md) · [报告](report-contract.md) · [离线读取与定位](offline-reading-contract.md) |
| 工程辅助库 | [源码获取宿主](windows-source-host-contract.md) |

旧 definition 的字段和拒绝语义在对应契约中注明，不能用新版本重新解释旧证据。
读取支持集合以当前格式契约矩阵为准；软件实现、交付审核与原生验收分别记录在 backlog。
操作步骤见[使用指南](../usage.md)，长期取舍见 [ADR](../decisions/README.md)。

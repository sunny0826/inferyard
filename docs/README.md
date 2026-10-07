# 文档导航

## 使用工具

| 入口 | 内容 |
| --- | --- |
| [项目说明](../README.md) | 产品、安装和最短工作流 |
| [安装指南](installation.md) | uv tool/uvx、工作区、资产准备、外部服务和绑定 |
| [使用指南](usage.md) · [CLI 参考](cli-surface.md) | 计划、运行、恢复、比较、迁移及参数 |
| [平台状态](platforms.md) · [macOS](../MACOS.md) · [Windows](../WINDOWS.md) | 已实现范围、原生覆盖与操作限制 |
| [配置说明](../configs/README.md) · [设备检测](device-check.md) | 配置字段、容量推荐与身份 |
| [报告](reports.md) · [引擎诊断](engine-fit-common.md) | 离线阅读与同机 engine-fit |
| [流程图](show-me-system-flow.html) | 安装、准备、执行与离线证据通路 |
| [inferyard-observer](../observer/README.md) | 独立进程监测器 |

## 修改项目

| 入口 | 内容 |
| --- | --- |
| [贡献指南](../CONTRIBUTING.md) · [Agent 约定](../AGENTS.md) | 工具链、检查、文件边界与提交 |
| [架构](architecture.md) | 模块职责与运行设计 |
| [数据契约](data-contract.md) · [主题契约](contracts/README.md) | 活动格式、来源和功能接口 |
| [实验设计](experiments/design.md) · [指标](experiments/metrics.md) | 冻结协议、分母和计量定义 |
| [方法目录](reference/methods-matrix.md) · [执行说明](reference/methods-guide.md) · [术语](reference/glossary.md) | 方法适用前提与解释 |
| [ADR](decisions/README.md) | 长期设计理由 |
| [backlog](backlog.md) · [变更记录](../CHANGELOG.md) | 当前剩余工作与候选范围 |
| [脚本](../scripts/README.md) · [Schema](../schemas/README.md) | 工程工具与生成文件 |

指标和方法文档是 catalogue 生成源，修改时同步导出且保留稳定 ID。
原始运行留在本机，`validation/` 不属于公开工作树；证据规则统一见[数据契约](data-contract.md#证据血缘与比较结论)。

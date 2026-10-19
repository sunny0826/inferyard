# engine-fit 引擎诊断

本页保留命令边界和阅读入口。职责、执行路径、引擎绑定和验证分别维护：

- [职责与事实源](engine-fit-sources.md)
- [运行路径](engine-fit-flow.md)
- [引擎绑定与平台分派](engine-fit-bindings.md)
- [验证](engine-fit-checks.md)

`engine-fit` 记录同一台机器、同一份模型资产在不同引擎上的协议完成情况、客户端耗时、
token 与进程树资源，形成独立诊断产物；不计算正式质量分数或性能赢家。它与
[运行时执行与引擎适配](runtime-execution.md) 共享主机锁、dirty 恢复与原生资源采集机制，
但使用独立的诊断计划定义（`engine_fit_plan.v1`–`v4`）与运行定义
（`engine_fit_run.v1`–`v6`），不复用正式基准的 workload/trial 通路，也不进入正式评分
分母。

字段语义、枚举值与版本规则只维护在
[engine-fit 契约](../../cli/docs/contracts/engine-fit-contract.md)；操作步骤见
[engine-fit 使用指南](../../cli/docs/engine-fit-common.md)；长期理由见
[ADR 012](../../cli/docs/decisions/012-engine-fit.md)、
[ADR 013](../../cli/docs/decisions/013-engine-fit-macos.md)、
[ADR 014](../../cli/docs/decisions/014-macos-common-engines.md)、
[ADR 015](../../cli/docs/decisions/015-lmstudio-local-cli-identity.md)、
[ADR 016](../../cli/docs/decisions/016-engine-fit-temperature-override.md)、
[ADR 019](../../cli/docs/decisions/019-engine-fit-memory-override.md)、
[ADR 020](../../cli/docs/decisions/020-windows-engine-fit-native.md)。

## 职责与事实源

见 [职责与事实源](engine-fit-sources.md#职责与事实源)。

## 运行路径

见 [运行路径](engine-fit-flow.md#运行路径)。

## 约束与失败行为

见 [运行路径：约束与失败行为](engine-fit-flow.md#约束与失败行为)。

### 各引擎绑定方式

见 [引擎绑定](engine-fit-bindings.md#各引擎绑定方式)。

### 温度与内存停止覆盖

见 [停止覆盖](engine-fit-bindings.md#温度与内存停止覆盖)。

### 平台定义分派

见 [平台分派](engine-fit-bindings.md#平台定义分派)。

## 验证

见 [验证](engine-fit-checks.md#验证)。

# ADR 006：macOS 原生来源与完整采集开销

状态：Accepted。电源来源由 [ADR 042](042-environment-collection-slim.md) 改为 IOKit。温度与完整开销的其余条款仍有效。

## 决策

为 macOS 引入只读 AppleSMC 温度和原生 `pmset` 电源策略来源，不填造 Linux governor/EPP。
完整观察成本须覆盖正式通路的采集、校验、写盘、flush/fsync、封存与收尾，不能用轮询微基准替代。
`total_observer_control.v2` 绑定冻结 plan、配置、题包与题序，开关采集使用同一正式运行通路。

## 理由与代价

仅新增传感器字段无法证明测量环境或采集成本；将 Mac 字段伪装成 Linux 来源会误授比较资格。
完整对照成本较高，且私人 SMC ABI、电源字段和采样开销因设备而异；失败和缺测必须保留原因。
历史对照未过冻结阈值不改变阈值或补造通过。

总开销与可选增量诊断的现行分级由 [ADR 036](036-scoped-measurement-cost.md)部分替代，
旧证据按原规则读取。平台接口见[性能契约](../contracts/macos-contract.md#原生温度与完整开销)，新适用域见[职责身份契约](../contracts/scoped-measurement-contract.md)。

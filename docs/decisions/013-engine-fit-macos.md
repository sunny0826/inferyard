# ADR 013：macOS 原生 engine-fit

状态：Accepted

## 决策

macOS engine-fit 使用原生进程身份、lsof 监听和 AppleSMC，按稳定活进程树读取 CPU 秒与 RSS bytes。
新定义 run.v2 区分 Linux run.v1，保留核心 v3；后续扩展不改旧字段来源。

## 理由与代价

只移除 Linux 限制会误用 `/proc`、启动时间和温度单位。独立原生后端保证来源明确，
但有进程树变化与权限缺测；整树统计拒绝部分合计。后端声明不能独立证明 Rosetta 状态或 GPU 驻留。
引擎环境独立于测评器，服务外部管理。

Windows 后续支持见 [ADR 020](020-windows-engine-fit-native.md)。字段见[macOS 诊断契约](../contracts/engine-fit-contract.md#macos-原生来源)。

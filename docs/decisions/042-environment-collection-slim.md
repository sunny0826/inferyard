# ADR 042：环境采集只复制本轮常量，macOS 电源与换页改原生来源

状态：Accepted

## 背景

[ADR 041](041-environment-persistence-slim.md) 只减少周期落盘字段。
`environment_snapshot()` 仍每次整份新读。macOS 上周期采样还要启动 `pmset` 和 `vm_stat`。

本决定改采集次数和这两个 macOS 来源。不扩大测量，不改采样频率，不改 `schema_version`。

## 决策

同一轮在 `environment.start.json` 写入之后，该轮周期采样把开跑快照里已经出现的常量抄进后续周期快照。当时读到的空值照抄，不在周期里重试。开跑快照没有的键不补造。

抄这些字段：`platform`、`kernel`、`architecture`、`cpu_model`、`logical_cpus`、`memory_total_bytes`、`os_release`、`cpu_flags`、`boot_id`、`page_size_bytes`、`gpu`、`evidence_durability`。

抄写发生在 ADR 041 的写盘投影之前。投影仍去掉 `cpu_flags`、`os_release`、`gpu`、`mem_available_bytes`、`page_size_bytes`。周期文件里留得下的，是其余被抄下的字段，包括身份字段和 `boot_id`。

每秒仍新读：`ac_online`、`ac_sources`、`profile`、`governor`、`epp`、`scaling_driver`、`cpu_policies`、`macos_power_policy`、`swap_pages`、`mem_available_bytes`。

没有这份开跑快照的调用整份新读。这包括结束快照、预检、`device-check`、请求边界观测，以及未写 `environment.start.json` 的采样。

抄到的字段不再为周期行重读一遍再覆盖：

- Linux：`cpu_model` 与 `cpu_flags` 都已抄到时不读 `/proc/cpuinfo`；`os_release` 已抄到时不读 os-release。`meminfo` 仍读。`memory_total_bytes` 已抄到时，快照里的总量用抄到的值。
- macOS：`cpu_model` 已抄到时不读 CPU brand string；`boot_id` 已抄到时不读 boot；`os_release` 已抄到时不读系统版本。可用内存仍读。总量已抄到时用抄到的值。
- Windows：`cpu_model` 已抄到时不读注册表处理器名。

macOS 新采集的电源来源是 `macos.iokit.power-policy.v1`。对象字段仍是 `source`、`ac_online`、`low_power_mode`、`power_mode`、`power_mode_supported`。供电源只认 `AC Power` 与 `Battery Power`，其他情况电源为缺测。活动区里的 `LowPowerMode` 只接受 0 或 1。电源模式先看 `powermode`，没有再看 `PowerMode`；两个都没有则 `power_mode` 为 `unsupported`，`power_mode_supported` 为 false。`ac_sources` 的键是 `iokit:providing-power`。

macOS 新采集的换页来源是 `host_statistics64:swapins` 与 `host_statistics64:swapouts`。计数仍是 Swapins / Swapouts，不是 pageins / pageouts。同一次 counters 调用共用这一次读取和同一个读取区间。

这两处每次采样都新读。失败是缺测，不回退到 `pmset` 或 `vm_stat`，也不沿用上一笔。`device-check` 的现有读取不在本决定内，仍以 [device-check](../device-check.md) 为准。

## 契约与旧证据

证据字段见[数据契约](../data-contract.md#周期环境与调度记录)。macOS 接口见[环境与换页契约](../contracts/macos-contract.md#环境与换页读取)。

本决定修订 [ADR 008](008-macos-environment-reads.md) 里「换页共用一次 `vm_stat`」和「环境每次全量新读」。同次换页共用一个读取窗口、失败不沿用旧值，这两条仍有效。也修订 [ADR 041](041-environment-persistence-slim.md) 里「`environment_snapshot()` 每次新读」。ADR 041 的周期落盘字段集仍有效。 [ADR 006](006-macos-performance-prerequisites.md) 的电源来源改为本决定的 IOKit 来源；温度与完整开销的其余条款仍有效。

核心 `schema_version = 3` 和 ADR 038 支持集不变。配置里的电源 `source` 同时接受 `macos.pmset.power-policy.v1` 与 `macos.iokit.power-policy.v1`。资源样本同时接受 `vm_stat:Swapins` / `vm_stat:Swapouts` 与 `host_statistics64:swapins` / `host_statistics64:swapouts`。同一次运行里的冻结条件、周期观测和换页样本应是同一个来源；来源不同是变化，不是相等。

旧证据按原字节读取，不迁移，不重新解释。读侧不用 start/end 回填周期缺字段，也不抹掉旧记录里已经写下的中途变化。新源码不继承旧采集开销资格，遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)。

已经冻好、来源仍是 `macos.pmset.power-policy.v1` 的条件，与新观测对不上，需要重新冻结。

## 后果

周期记录看不到这些常量中途变化又在结束前恢复的情况。`logical_cpus` 与 `memory_total_bytes` 的热插拔也只出现在结束快照。变化若保持到结束，结束时的新读仍能发现。

不宣称实测提速。软件回归使用合成证据，不代表原生设备或真实模型验收。

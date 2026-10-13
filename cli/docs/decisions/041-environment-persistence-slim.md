# ADR 041：周期环境与调度记录的持久层瘦身

状态：Accepted。`environment_snapshot()` 每次新读由 [ADR 042](042-environment-collection-slim.md) 修订。本决定的周期落盘字段集仍有效。

## 背景

周期 `environment.jsonl` 重复保存完整环境快照。`cpu_flags`、`os_release`、
`gpu` 等信息没有周期记录消费者，持续写盘增加证据体积。
`mem_available_bytes` 的实时安全检查、资源准入与报告展示仍需要完整快照，
不能因此删除运行时字段。身份常量参与环境资格判定，也不能从周期记录删除。

## 决策

仅在两处周期写入边界投影快照，移除 `cpu_flags`、`os_release`、`gpu`、
`mem_available_bytes`、`page_size_bytes`；其余字段及观测 envelope 原样保留。
`LINUX_FIELDS` / `MACOS_FIELDS` 全集、平台、电源策略、动态状态、CPU policy 清单、
换页计数、来源与读取区间均保留，不修改身份、准入或资格判定。

`page_size_bytes` 的资源归约来自 `memory.jsonl` 换页样本；资源比较还与
`collector.json` 的 scale 绑定。两者不读取周期环境记录，保持其页大小和校验不变。
`schedule.jsonl` 的周期记录不再写恒为 0 的 `queue_depth`，读侧不再要求或消费它；
四个时序/成本整数与 resource boundary 读取区间的校验保持不变。

`environment.start.json` / `environment.end.json` 继续全量保存；报告系统版本、GPU
与开始时可用内存仍读取 start 快照。`environment_snapshot()` 的签名、输出字段、
构造过程和每次新读保持不变，遵循 [macOS 来源契约](../contracts/macos-contract.md#环境与换页读取)。
不缓存快照，不改变采样频率和采集成本；Linux 每秒读取成本留待单独评估。

## 契约与旧证据

这是 v3 周期持久化字段集的契约修订，见[数据契约](../data-contract.md#周期环境与调度记录)。
核心 `schema_version = 3` 和 ADR 038 支持集不变。旧 v3 全量周期记录按原字节读取，
不迁移、不重写；额外字段不导致拒绝。消费保留字段时仍用 `.get`，缺失身份字段仍
产生原有 unknown/资格失败，不能拿 start 快照填补周期缺测。

合法旧记录、新投影记录及非法时序/结构样例由
`tests/unit/test_environment_persistence.py` 固定。等价性覆盖环境、调度、资格判定，
包括变化和缺失身份的失败路径；封存 fixture 覆盖完整读取、资源摘要和报告 profile。

## 后果

周期证据体积下降，但不能再从单条周期记录重建全量环境快照；start/end 仍保留全量。
保留字段的摘要与测量资格判定不变，持久化字节及 manifest 哈希会改变。
不宣称实测提速，也不继承旧源码的采集开销资格；遵循
[证据血缘规则](../data-contract.md#证据血缘与比较结论)。
软件回归使用合成证据，不代表原生设备或真实模型验收。

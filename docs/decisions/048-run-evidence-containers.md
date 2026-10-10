# ADR 048：运行原件容器与领域读取

状态：Accepted（实施中，未获验证资格）。日期：2026-10-10。
基线：`9be73c8b82edc55d0b3f21f493692442ea2cb8c8`。
实施与真实验证进度仅登记在 [backlog](../backlog.md#运行证据文件合并)。

## 决策与取代范围

用户已授权拆分任务、并行实施并在完成后 review。新生产 run 使用独立
`evidence_layout_version=2`；内层核心 `schema_version=3`、五终态、采样范围与资格不变。
保留当前合法 v3 快照布局的严格只读支持；不转换、不补写、不重签、不保留旧生产 writer。
未知布局退出 2，混合权威源、版本冲突、坏哈希或缺已引用原件退出 4。

本决策局部取代 [ADR 038](038-inferyard-current-format.md) 中核心 run 只有当前快照布局的
物理支持集合，以及 [ADR 044](044-prompt-content-hash.md) 中逐请求原件必须单独成文件的约定。
ADR 038 的迁移源拒绝、独立 engine-fit/extension 格式、旧报告拒绝继续有效；ADR 044 的
完整脱敏原件、消息哈希和发送前耐久约束继续有效。报告仍为 v8 的三个文件。

运行证据保存为 inputs、observations、request-bodies、events、memory、environment、schedule、
manifest 八文件。四份采样/事件日志不合并，选用高频扩展流、batch 外层和原始父链另计。
具体结构、共享接口、生命周期和来源引用见 [容器契约](../contracts/run-evidence-layout.md)。

## 替代方案

| 方案 | 代价与选择 |
| --- | --- |
| 业务访问虚拟旧文件名 | 混淆成员内容与物理封存；拒绝，全部消费方迁移到领域查询 |
| SQLite / 全局大日志 | 新增事务、跨写者排序和恢复协议；本轮不采用 |
| 分别封存五输入与逐请求文件 | 原件随请求数增长，保留为旧证据只读适配，不作为新 writer |
| 跨 run 共享输入 | 破坏独立携带与封存，继续每个 run 自包含 |
| JSON/JSONL 容器与同次读取领域视图 | 采用；代价是成员定位、索引与新增竞态核验 |

## 风险与验收

全部读取方先迁移，生产 writer 再一起切换。原子 snapshot、通用日志和封存仍供
ExtensionJournal 使用；独立封包与 baseline 格式不随 run 布局改变。
prepared append/flush/fsync 必须先于 dirty/started/send，native receipt 耐久先于 clean。
request-bodies 扫描一次，索引只保留 O(N) 元数据；再次取 body 时核对已验证的记录字节。

软件验收要求旧证据原字节不变、新旧语义一致、容器/成员篡改与生命周期故障严格拒绝，
普通初始 single 八加三、默认双 scope 十六加三实际清点，全部派生/恢复/来源入口可用。
模拟 CLI、真实离线浏览器、性能预算和两项独立源码 review 分别取得回执后才能记完成。
真机、长时负载和发行资格另验；模拟通过不推定原生能力或性能收益。

## 实际基线计数

`adapter_collector_id(batch=False)` 为普通 single 选择 `*-memory.v1` 的 Sampler，构造不写
collector 声明；完整 lab single 固定 18 份加请求原件，默认两 scope 为 170 份含报告。
使用 ResourceSampler 的同事实场景才额外写 collector，为固定 19 / 合计 172。
native 还按实际能力声明及收据另计。不能因文件合并把 single 改成资源采集或补测。
上述为源码推导，实际验收记录各次 sampler、输入、请求数和物理清单。

# 运行读取、预算与周期记录

[返回主参考](../data-contract.md)

## 命令内复用与预算快照

Schema 注册表在模块内缓存，导出返回独立副本。`Document` 保存完整校验后的不可变
JSON 快照；内部编译可消费同 kind 快照，原始 dict 仍完整校验。路径解析后、Journal
写入前及 `read_trial` 读取时保留完整校验。bundle 的结构与语义各检查一次，不在语义
分派中重复检查结构。人工审核成功判定按完整快照 SHA256（包含审核记录与证明）在
单次 CLI 命令内复用；命令结束即丢弃，原始可变输入始终先完整校验。

评分身份每命令每版本计算一次；选中 case 的规则准备一次，结构 validator 复用。
公开评分输入、自定义 scorer 结果与外部证据仍在边界核验。源码修改改变真实评分身份；
`verify` 的重评分核验在递归血缘身份不匹配时返回 `rescore_scorer_identity_changed`、退出 4，
不返回 verified，不证明篡改。来源 manifest 和已有血缘哈希应继续核验。

预算 payload 的 definition 为 `token-budgets.v2`（layout2 为 prepare 阶段的 token_budgets 观测；layout1 为 `token-budgets.v2.json`）：
`{"definition":"token-budgets.v2","entries":[{"phase":"probe","case_id":null,"budget":{...}}]}`。
顺序为 probe、可选 warmup、选中 formal 题序；辅助 case_id 为 null，formal 为冻结题 ID。
probe/check 只有 probe；run 的 warmup_count = 0 不含 warmup；resume 只含续测选中题。
budget 保留适配器原记录；lab 严格六键放在 budget 内，不允许附加键或类型放宽。
旧 `token-budgets.json` 的 probe/warmup/选中题位置数组返回 `unsupported_format` / 2，
与 v2 并存时也拒绝；v2 定义不匹配、重复/错序/未选中项均拒绝。预算缺测不能补造为 0。
日常直接 run；独立 probe 是可选排障，不能产生跨命令探测凭证。

同命令复用已核验数据及比较结果；日志哈希和解析消费同一读取的字节。
首事件日期显式传递，不重新打开 events。不跨命令复用，不把新打开路径的字节视为已核验。
`TrialReads` 在一次归约内复用环境起止快照、周期环境、调度和外部 CPU 日志的
解析结果与原字节摘要。manifest 核验和环境归约消费同一读取，不跳过坏哈希、
严格 JSON 或末尾截断检查。下一次归约重新读取，不能继承前次成功。
当前 ledger 的规范 JSON 输出保持等价；report 仅接受 v8，comparison 仅接受 format4 / phase2.v3。
旧 report v1–v7、comparison format1–3 不再读取或重建，返回 `unsupported_format` / 2。
命令内复用见 [ADR 034](../decisions/034-command-local-reuse.md)，旧格式支持已由
[ADR 038](../decisions/038-inferyard-current-format.md) 收窄。

## 批次历史投影

`run --plan` / `resume` 的 `history()` 使用 `read_run_projection`，只还原后续发送和
结果摘要实际消费的字段。`verify`（run/batch，以及重复摘要核验）、`report` 和 `repeat-summary`
继续使用完整 `read_trial`；用途分离的理由见
[ADR 040](../decisions/040-batch-history-projection.md)。不改变 v3 格式或跨命令保存缓存。

| 消费数据 | 来源与校验 |
| --- | --- |
| run / tool 身份、diagnostic、relation、parent_run_id | `TrialEvidence.input("run")`；现有结构、冻结计划绑定与批次身份检查 |
| config、bundle、selection、plan | inputs 冻结成员；旧布局为对应冻结文件；结构、语义、选择哈希和绑定检查 |
| case_id、执行终态、发送/终止时间、category、quality_state、score | 流式 `events.jsonl` 与题包；生命周期及 score 使用原校验器 |
| stop_reason、counts、completeness、duration | 事件重建；持续窗口用原 reducer；不信任可缺失的 `summary.json` |
| events_sha256、path、manifest_sealed | 同次事件字节哈希、目录与 manifest；封存标记不等于全量完整性核验 |
| service_drain | 原 `service_drain`、idle/start 事件、engine_capabilities 观测及物理 manifest 绑定；lab 还核验 service_properties 观测和已引用请求原件 |

投影每行严格解析完整 JSON 行；重复键、非有限数、中间损坏仍拒绝。未完成的末行沿用
截断语义。消费事件保留身份、序号、时钟、生命周期、评分与窗口检查；不消费的
content/reasoning、wire、到达时间、usage 等事件不重放其语义。内存日志只检查文件存在性、
封存大小和末尾至多一字节，保留截断对完整性的影响；不解析采样。
不打开 `environment.jsonl`、`schedule.jsonl`。

投影不执行：manifest 全文件字节哈希重验、逐样本/环境/调度校验、响应 chunk 与终态
内容/协议/用量的交叉重放、资源/性能指标重建、固定输出/缓存执行证明、引擎参数及
token budget 的离线全量复核、native 引擎内部排空标记与 lab 事件来源一致性的语义
拒绝（`engine_internal_drain` 非空、非 lab 引擎的 `lab_wire`/`lab_usage`）。以上仍由
完整 `read_trial` 及其被调用模块执行。
manifest 的结构、合法文件路径、原始文件存在性和大小检查保留；配置/题包选择哈希
及真实事件哈希仍计算。预算文件的 run_id、数值范围与封存成员检查不变。
多类损坏并存时，投影与全量读取报告的优先错误可能不同，两者都拒绝该 run。

## 周期环境与调度记录

[ADR 041](../decisions/041-environment-persistence-slim.md) 修订 v3 周期持久化字段集，
[ADR 042](../decisions/042-environment-collection-slim.md) 修订本轮常量的采集次数和 macOS 电源、换页来源；
核心 `schema_version = 3` 与现有格式支持集不变，不新增 Schema kind。
`environment.jsonl` 的 `snapshot` 在写盘时仅移除 `cpu_flags`、`os_release`、`gpu`、
`mem_available_bytes`、`page_size_bytes`，其余快照字段原样保留。特别保留：

- 身份字段 `platform`、`kernel`、`architecture`、`cpu_model`、`logical_cpus`、
  `memory_total_bytes`，覆盖 `LINUX_FIELDS` / `MACOS_FIELDS` 的全部要求。
- 动态字段 `ac_online`、`profile`、`governor`、`epp`、`scaling_driver`、
  `cpu_policies`、`macos_power_policy`、`swap_pages`。
- 已有来源、boot、限制和读取区间字段，以及外层 `monotonic_ns`、`phase` 等观测字段。

`environment.start.json` / `environment.end.json` 保持完整快照。
结束快照、预检、`device-check` 和请求边界观测每次都是新读。采样频率不变。

同一轮在 `environment.start.json` 写入之后，周期采集把开跑快照里已经出现的下列字段抄进后续周期快照，包括当时的空值；开跑快照没有的键不补造：
`platform`、`kernel`、`architecture`、`cpu_model`、`logical_cpus`、
`memory_total_bytes`、`os_release`、`cpu_flags`、`boot_id`、`page_size_bytes`、
`gpu`、`evidence_durability`。
抄写发生在上面的写盘投影之前，所以被投影去掉的五项不会因为抄写而重新出现在 `environment.jsonl`。
已抄到的字段不再为周期行重读后覆盖，具体停读范围见 [ADR 042](../decisions/042-environment-collection-slim.md)。

下列字段每秒新读，不抄：
`ac_online`、`ac_sources`、`profile`、`governor`、`epp`、`scaling_driver`、
`cpu_policies`、`macos_power_policy`、`swap_pages`、`mem_available_bytes`。
没有开跑快照的采样整份新读。

因此周期记录看不到这些常量中途变化又在结束前恢复的情况。
`logical_cpus` 与 `memory_total_bytes` 的热插拔也只出现在结束快照。
变化若保持到结束，结束快照的新读仍能发现。
报告系统版本、GPU 和开始时可用内存来自 start 快照。
资源换页计算的 `page_size_bytes` 来自 `memory.jsonl` 样本，并与 `collector.json`
核对；不从周期环境记录补值或推导 scale。

周期 `schedule.jsonl` 不再写入或要求 `queue_depth`，仍要求 `scheduled_ns`、
`actual_ns`、`late_ns`、`collector_work_ns` 为非负整数（拒绝 bool）。
`kind=resource_boundary` 仍校验非负整数 `read_started_ns` / `read_finished_ns`，
且结束不得早于开始。环境观测仍要求非负整数、严格递增的 `monotonic_ns` 和对象 `snapshot`。

旧证据读取政策：旧 v3 全量周期记录、带 `queue_depth` 的调度记录继续按原字节读取，
不因这些额外字段拒绝，不迁移或改写封存。保留字段用 `.get` 读取；身份缺失仍记录
`environment_unknown:*` 并阻止资格成立。读侧不使用 start/end 回填周期身份，
也不抹掉旧记录里已经写下的中途变化。
ADR 041 的落盘投影不改变同一份快照的 `assess_environment`、`assess_schedule` 与环境资格。
ADR 042 改的是新运行怎么采集：新周期身份与开跑快照一致，只有结束快照能发现保持到结束的身份变化。
原有严格 JSON、封存哈希、时间和来源校验不变。

合法旧/新样例及非法缺键、bool、负值、错误结构的回归位于
`tests/unit/test_environment_persistence.py`。周期记录不再独立包含全量环境信息；
来源变化后的结论仍遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)。

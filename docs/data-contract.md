# 统一数据契约

活动契约唯一版本为 `schema_version = 3`，适用于单次测评和批量实验。结构定义在 `src/inferyard/contracts/`，导出位于 `schemas/`；结构合法仍须通过语义与来源校验。

## 唯一入口

- `SCHEMA_VERSION = 3`；`schemas_for()` 返回唯一 kind 注册表，`export_schema(kind)` 不接受版本选择。
- `Document.parse(kind, data)` / `validate_document(kind, data)` 只接受活动契约；InferYard 不提供旧运行迁移，旧数据交原项目处理。
- Schema 按配置、题包/评分、事件/样本、实验、存储/汇总划分模块；不使用旧版本 Schema 继承来拼接新版本。

## 一种运行包

统一沿用实验/轮次身份：`experiment_id`、`trial_id`、`run_id`、`request_id` 和计划哈希。`run --config` 在请求前编译为一个 workload、一个 trial；`run --plan` 执行显式冻结实验。`probe`（兼容入口 `check`）只探测，不产生正式成绩。

运行包的 wire 枚举仍为 `kind = check | run`（`probe` 写入 `check`）、`execution_mode = single | experiment`、`origin = measured | migrated`（Schema 枚举保留，但应用拒绝 migrated）。两种执行方式使用同一 Journal 和 ledger。Windows 单次及受支持的批量入口保留原生身份与内存采样；macOS 通过平台工厂使用原生采集器，不调用 Linux 专用采集。

| 文件 / kind                                              | 责任                                                 |
| -------------------------------------------------------- | ---------------------------------------------------- |
| config、config_input                                     | 唯一配置、默认值、路径解析与凭据引用                 |
| bundle、score                                            | 统一题型与评分结构，审核来源可核验                   |
| experiment、plan、run、selection                         | 冻结顺序、预算、重复和父子关联                       |
| event、sample                                            | 同一实验/轮次 envelope，按事件或采集种类区分 payload |
| manifest                                                 | 文件哈希、运行身份、封存状态与来源                   |
| summary、metric_definition、metric_observation、analysis | 重建、指标、缺测与比较资格                           |

固定题序与持续负载按 protocol 表达不同分母：固定计划五终态守恒；持续负载保留窗口、请求上限、实际发送与排空语义，`planned = null` 不转换成零。指标、引擎、评分算法的 definition 版本独立于 wire revision，不全局改写 `.v1` / `.v2` 标识。

单次执行使用 `compile_single_plan` 冻结的 `budget.max_wall_seconds` 总截止时间，
从冻结完成后的准备开始计时，覆盖预检、空闲等待、探测、预热、基线与正式请求。
耗尽时停止后续发送，记录 `single_wall_budget_exhausted` 并退出 3；用户取消仍退出 130。
已发送请求走原取消/有界排空流程，未确认空闲时保留 dirty，清理与封存仍须完成。
同步检查不能被异步截止抢占，返回后复查预算；截止不强行中断证据封存。

`execution.warmup_count` 接受整数 0..3，省略时仍默认 3；0 表示不发送预热请求。
冻结配置记录实际值，plan 通过配置哈希绑定，manifest 封存对应原始字节，不在读取时补改。
普通/流式 probe 仍执行；预热不计入正式五终态分母。历史值 3 的 v3 plan/run 继续按原规则读取。
缓存比较按实际冻结次数核验完整请求历史；预热次数或历史不一致仍不授比较资格。

## 共享接口与边界

`category = svg` 为 `quality` 协议下的免评分生成题：`rules` 只能为 `{}`，
`reference_answer` 仍必填。评分记录的 `quality_state = not_applicable`，
与 `performance` 一样不进入 Q01–Q08 的分子、分母及排除计数；执行终态、生成期性能
与资源仍按现有管线采集和核验。SVG 解析/大小状态仅为报告观察量，见
[报告契约](contracts/report-contract.md#v3-svg-展示与-csp)。

- 统一评分器 `score_case(case, answer, policy)`、`unscorable(category, reason)`、`scorer_hash()`；instruction/extraction 算法作为类别函数，不先生成旧 score 再包装。
- 统一题包入口 `content_hash(bundle)`、`require_review(bundle)`、`validate_bundle(bundle)`；迁移审核证明须核验原哈希、原批准及内容等价。
- 存储 IO 与 Journal envelope 分离；单次及实验均写 plan/run/selection、冻结输入、events、memory 和 manifest。
- `read_trial(root)` 是活动结果重建入口；报告/比较读取已保存事件和评分。显式 rescore 另存新评分分析。

## 命令内复用与预算快照

Schema 注册表在模块内缓存，导出返回独立副本。`Document` 保存完整校验后的不可变
JSON 快照；内部编译可消费同 kind 快照，原始 dict 仍完整校验。路径解析后、Journal
写入前及 `read_trial` 读取时保留完整校验。bundle 的结构与语义各检查一次，不在语义
分派中重复检查结构。人工审核成功判定按完整快照 SHA256（包含审核记录与证明）在
单次 CLI 命令内复用；命令结束即丢弃，原始可变输入始终先完整校验。

评分身份每命令每版本计算一次；选中 case 的规则准备一次，结构 validator 复用。
公开评分输入、自定义 scorer 结果与外部证据仍在边界核验。源码修改改变真实评分身份；
rescore-check/递归血缘身份不匹配返回 `rescore_scorer_identity_changed`、退出 4，
不返回 verified，不证明篡改。来源 manifest 和已有血缘哈希必须继续核验。

新预算文件 `token-budgets.v2.json` 为
`{"definition":"token-budgets.v2","entries":[{"phase":"probe","case_id":null,"budget":{...}}]}`。
顺序为 probe、可选 warmup、选中 formal 题序；辅助 case_id 为 null，formal 为冻结题 ID。
probe/check 只有 probe；run 的 warmup_count = 0 不含 warmup；resume 只含续测选中题。
budget 保留适配器原记录；lab 严格六键放在 budget 内，不允许附加键或类型放宽。
旧 `token-budgets.json` 的 probe/warmup/选中题位置数组返回 `unsupported_format` / 2，
与 v2 并存时也拒绝；v2 定义不匹配、重复/错序/未选中项均拒绝。预算缺测不能补造为 0。
日常直接 run；独立 probe 是可选排障，不能产生跨命令探测凭证。

同命令复用已核验数据及比较结果；日志哈希和解析消费同一读取的字节。
首事件日期显式传递，不重新打开 events。不跨命令复用，不把新打开路径的字节视为已核验。
当前 ledger 的规范 JSON 输出保持等价；report 仅接受 v7，comparison 仅接受 format4 / phase2.v3。
旧 report v1–v6、comparison format1–3 不再读取或重建，返回 `unsupported_format` / 2。
命令内复用见 [ADR 034](decisions/034-command-local-reuse.md)，旧格式支持已由
[ADR 038](decisions/038-inferyard-current-format.md) 收窄。

## 批次历史投影

`run --plan` / `resume` 的 `history()` 使用 `read_run_projection`，只还原后续发送和
结果摘要实际消费的字段。`verify`（run/batch）、`report`、`repeat-summary` 和
`repeat-check` 继续使用完整 `read_trial`；用途分离的理由见
[ADR 040](decisions/040-batch-history-projection.md)。不改变 v3 格式或跨命令保存缓存。

| 消费数据 | 来源与校验 |
| --- | --- |
| run / tool 身份、diagnostic、relation、parent_run_id | `run.json`；现有结构、冻结计划绑定与批次身份检查 |
| config、bundle、selection、plan | 对应冻结文件；结构、语义、选择哈希和绑定检查 |
| case_id、执行终态、发送/终止时间、category、quality_state、score | 流式 `events.jsonl` 与题包；生命周期及 score 使用原校验器 |
| stop_reason、counts、completeness、duration | 事件重建；持续窗口用原 reducer；不信任可缺失的 `summary.json` |
| events_sha256、path、manifest_sealed | 同次事件字节哈希、目录与 manifest；封存标记不等于全量完整性核验 |
| service_drain | 原 `service_drain`、idle/start 事件、`engine-capabilities.json` 与 manifest 绑定；lab 还核验 `service.props.json` 和请求快照 |

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

## 证据血缘与比较结论

结论绑定实际设备、引擎、配置、题包、测量源码和采集来源。历史运行说明当时的测量；模拟、短诊断、迁移、重评分、报告重建与离线核验分别记录自己的来源和范围，不构成当前源码的新实测。派生产物保留原来源与缺项，在新目录生成，原件不覆盖。

比较是否成立以对应产物的机器字段及原因列表为准，不从文档中的“通过”推导。`engine_fit_*` 中性能结论由 `performance_comparison_qualified` 表达；现行实现固定为 `false`。核心测评的质量、性能和资源比较继续使用各自既有字段，不把 `engine-fit` 字段套到所有格式。

新文档只引用本节，必要时补充该功能独有的输入、失败原因和证据链接。长期设计理由保存在 ADR，过期实施记录不作为当前接口。详细计量口径见[指标定义](experiments/metrics.md)，当前进度见[backlog](backlog.md)。

## 证据存储与自存

Git 工作树只保存代码、现行文档、题包与必要的小型测试夹具。`validation/`、`reports/`、`artifacts/` 原始证据不提交 Git，也不作为默认测试或构建依赖，由操作者本机或外部存储保存；源项目的旧 Git 历史保留在原仓库，InferYard 新仓库不导入旧提交。仓内需要引用时只登记其 SHA-256 哈希与位置指针，指针登记不改变证据资格语义。核验以自存原件的字节哈希为准；原件缺失时如实标记，不补造观测。

## macOS 来源与后端

`engine.backend` 接受 `cpu`、`cuda`、`metal`，具体组合仍须通过实时平台、服务身份和适配器校验。Metal 的硬件观察、启动 offload 声明、动态库绑定与模型 GPU 驻留是不同证据；无法观察的驻留情况保留 `not_observed`。

`macos-resource.v1` 复用 v3 样本结构，保留独立 collector 与 source。CPU user/system 秒转换为整数微秒，`clock_ticks_per_second = 1000000`；RSS/内存用 bytes，主机换页用 pages 并保存页大小。换页不包含压缩内存，也不直接归因于模型。macOS `process_start_ticks` 来自原生内核启动时间的整数微秒，仅用于同平台进程身份核验，不能与 Linux jiffy 或 Windows FILETIME 比较。

来源变化、计数回退、权限失败和缺段均拒绝或标缺测；温度、频率、能耗和独立显存不补零。历史 v3 样本保持可读，离线归约按证据来源选择语义，不按报告生成机器选择平台。新增来源不继承 Linux、历史运行或旧源码的采集开销资格，详见 [macOS 平台契约](contracts/macos-contract.md)。

## Windows 批量资源来源

`windows-resource.v1` 复用 v3 内存样本结构，以 psutil 读取主机可用内存和绑定 PID 的 RSS，
单位 bytes，采集来源分别为 `psutil:virtual_memory:available` 和
`psutil:Process.memory_info:rss`。RSS 前后核验原生创建 FILETIME；不包含子进程或 GPU 内存。
未实现的 CPU、换页、温度、频率和能耗保留 null + 原因，不借用其他平台计数。
Schema 不扩展 CPU/传感器枚举；正式 Prism 批量分派与缺测证据见
[Windows Prism 契约](contracts/windows-prism-batch-contract.md)。软件支持与真机门进度分开记录。

## InferYard 名称与兼容边界

公开项目、Python 包和 CLI 名称统一为 `inferyard`，独立监测命令为 `inferyard-observer`。
监测流仅接受 `lab_observer.v2`。核心 Schema 仍为 v3，`urn:local-ai-bench:` 不变；
题包与审核证明原字节保留。报告仅接受 v7、比较仅接受 format4 / phase2.v3、公开包仅接受 v5。
新锁使用 inferyard-host.*，实时入口首次运行自动初始化，旧工具文件完全忽略，见[当前格式契约](contracts/inferyard-current-format.md)。
源码与模板变化产生新身份，不继承旧测量、评分或性能资格。

## 历史输入边界

InferYard 不迁移核心 v1/v2、不接受 origin=migrated（即使 schema3），递归来源也遵循相同集合。
旧报告、比较、公开包与混合历史来源返回 unsupported_format/2；可核验的坏封存先返回 4。
旧数据保留原字节并交原项目处理。内嵌题包 source_json 仅用于审核证明，不能作为活动输入。
engine-fit plan1–4/run1–6 都有现行 writer，继续保留；只删除 manifest1 与历史资产回退。
通用 verify 可直接核验当前 run/batch/冻结计划及 engine-fit，不改保存字节。

## 保持的行为

CLI stdout 为 JSON，`device-check` 默认人类摘要，Agent 使用 `--json` / `--format json`，保存的设备产物仍为 JSON，见[设备检测](device-check.md)。诊断走 stderr；退出码 0/2/3/4/130 分别为完成、预检阻断、运行不完整、工具或证据错误、取消。模型答错不等于进程失败。模型服务外部启动，保持本机端点、主机锁、dirty 恢复、无隐式重试、取消及排空。缺测为 null 并说明原因；严格拒绝重复键、非有限数和损坏证据。


## 按用途分派

[ADR035](decisions/035-purpose-specific-admission.md) 定义新的离线比较、诊断准入、
评分继续、服务复用与逐题审核边界。

- comparison.json 的 format_version=4、definition=phase2.v3；旧 format1–3 不支持。
  plan/run 的 definition_versions.comparison 保持 phase2.v1。
  observed_differences 与 eligibility 分开：前者描述当前样本，后者仍为受控结论。
  总体质量必须同题/同规则/同评分器/同分母且评分完整；性能仍需原逐指标资格。
- report_format_version=7 使用当前比较定义及根模板；旧报告1–6不支持。
- 可选 environment_admission={definition: environment-admission.v2, required_fields: [...]}
  在配置 conditions 或 experiment 声明；字段限定 ac_online/profile/governor/epp/
  macos_power_policy。未声明保持旧正式执行准入；probe/diagnostic 仅记录差异。
- 已完成请求的 unscorable 不改变执行终态；无身份/存储/服务异常时继续下一请求。
  评分原因保留，最终 partial/code3；旧停止证据不补造后续请求或分数。
- 服务复用必须 clean、非冷启动且加载条件/身份一致，当次核验有效配置。
  service-reuse.json 记录同进程或替换、缓存未知、当次预热次数。替换需旧进程退出。
- case_review_records 为可选新审核数组，每项 definition=case-review.v1、case_id、
  content_sha256、reviewer、reviewed_at、conclusion。哈希绑定 prompt/reference_answer/
  rules/category/task_protocol/answer_policy（含 case_id）。旧 review_records 保持旧哈希。
  未命中新记录的题必须由仍匹配的旧整包签名/迁移证明覆盖，否则要求人工审核。


旧整包 content_hash 仍只排除 review_records。新增逐题记录也会改变整包身份；不伪造
旧哈希，不从失配的旧整包记录或活动内容快照推导逐题批准。旧 v1/v2 迁移证明继续严格
核验原内容等价。编辑后的新副本若不再满足迁移证明，应移除不适用的 review_provenance，
保留历史原件及原 review_records；新副本必须有匹配的逐题记录或新的整包人工批准。

逐题摘要生成和人工填写示例见[使用说明](usage.md#逐题人工审核)。摘要计算不是审核。

环境准入优先级为 experiment.environment_admission > conditions.environment_admission >
旧严格策略；identity.json 的 environment_admission 封存 policy_source、policy、原始差异
和应用后的 blockers。诊断仅豁免预检，显式 safety.check_environment 仍按冻结 conditions
触发运行期停止，不受 required_fields 或 diagnostic 覆盖。

新增 rerun/跨 workload handoff 的同进程复用需主机非 dirty、旧 run 封存事件中的最后请求排空证明及当次 idle/身份/有效
配置核验。评分失败或 partial 本身不是 dirty；缺证明返回 service_reuse_drain_evidence_missing。
排空摘要只随同次读取显式传递，不改变历史 read_trial 输出，也不缓存日志原始字节。
native HTTP 收据仅证明客户端完成，无法证明引擎排空，因此不能用于此复用路径。
同一冻结 workload 的串行 repeat/resume 沿用既有准入，不新增 engine_idle 要求。
native client_http 完成只允许原串行流程继续；不确定响应、dirty 或取消仍按原路径停止或
要求绑定恢复。service-reuse.json 的 serial_continuation 明示此范围，previous_drain
可为 null，不能据此声称已观察引擎排空。换进程仍核对旧进程退出；冷启动要求不豁免。

冷启动/加载实验用 `execution.require_fresh_process=true` 显式要求每次换进程。
批量模式每次只推进一个此类 trial，随后交接；缺此字段时 clean 同配置可复用。
复用不证明缓存冷却；service-reuse.json 或 service-binding.json 披露缓存 unknown 和预热次数。


## 身份与测量资格分派

[ADR036](decisions/036-scoped-measurement-cost.md) 和[职责身份契约](contracts/scoped-measurement-contract.md)
定义新 run/batch 的可选 implementation_identity、目录资产 model-assets.v2，以及
comparison format4 / phase2.v3、report7。历史重算已由 ADR038 取消，
plan/run 的 comparison 标记仍为 phase2.v1。总源码摘要继续保存溯源；新执行身份只比较
measurement/scoring，未知依赖不能作相等证明。新运行只持久化原始请求事件，不写 requests.jsonl；
混入旧副本的来源明确拒绝。校准总开销与增量诊断分开，适用域及未覆盖条件见主题契约。

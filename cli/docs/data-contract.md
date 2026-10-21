# 统一数据契约

活动契约唯一版本为 `schema_version = 3`，适用于单次测评和批量实验。结构定义在 `src/inferyard/contracts/`，导出位于 `schemas/`；结构合法仍须通过语义与来源校验。

运行目录的物理布局独立于核心版本。[ADR 048](decisions/048-run-evidence-containers.md)
定义 `evidence_layout_version=2`；容器字段、成员绑定和严格只读的 layout1 支持见
[运行原件容器契约](contracts/run-evidence-layout.md)。新运行写入 layout2，现有合法 v3 快照布局只读；
实际实施与验收状态见 [backlog](backlog.md#运行证据文件合并)。独立 frozen-plan、engine-fit
与 extension 封包不改布局；本文旧快照文件名在 layout2 对应领域成员，不要求双写旧文件。

## 唯一入口

- `SCHEMA_VERSION = 3`；`schemas_for()` 返回唯一 kind 注册表，`export_schema(kind)` 不接受版本选择。
- `Document.parse(kind, data)` / `validate_document(kind, data)` 只接受活动契约；InferYard 不提供旧运行迁移，旧数据交原项目处理。
- Schema 按配置、题包/评分、事件/样本、实验、存储/汇总划分模块；不使用旧版本 Schema 继承来拼接新版本。

## 一种运行包

统一沿用实验/轮次身份：`experiment_id`、`trial_id`、`run_id`、`request_id` 和计划哈希。`run --config` 在请求前编译为一个 workload、一个 trial；`run --plan` 执行显式冻结实验。`probe` 只探测，不产生正式成绩。

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
- 存储 IO 与 Journal envelope 分离；单次及实验均在 inputs 保存 plan/run/selection 与冻结输入，观测和请求原件同步追加，events、memory 等日志独立封存。
- `read_trial(root)` 是活动结果重建入口；报告/比较读取已保存事件和评分。显式 rescore 另存新评分分析。

## 命令内复用与预算快照

见[命令内复用与预算快照](contracts/runtime-records.md#命令内复用与预算快照)。

## 批次历史投影

见[批次历史投影](contracts/runtime-records.md#批次历史投影)。

## 周期环境与调度记录

见[周期环境与调度记录](contracts/runtime-records.md#周期环境与调度记录)。

## 证据血缘与比较结论

结论绑定实际设备、引擎、配置、题包、测量源码和采集来源。历史运行说明当时的测量；模拟、短诊断、迁移、重评分、报告重建与离线核验分别记录自己的来源和范围，不构成当前源码的新实测。派生产物保留原来源与缺项，在新目录生成，原件不覆盖。

容器布局的来源引用分别绑定物理文件和逻辑成员：外层 `path/sha256` 为实际文件及其字节摘要，
可选 `member` 为冻结输入、观测序号或请求身份及对应内容摘要。所有派生产物使用同一核验接口，
不得把成员摘要当作旧文件摘要，或创建虚拟旧文件定位。旧 layout1 引用保留原物理含义。

比较是否成立以对应产物的机器字段及原因列表为准，不从文档中的“通过”推导。`engine_fit_*` 中性能结论由 `performance_comparison_qualified` 表达；现行实现固定为 `false`。核心测评的质量、性能和资源比较继续使用各自既有字段，不把 `engine-fit` 字段套到所有格式。

新文档只引用本节，必要时补充该功能独有的输入、失败原因和证据链接。长期设计理由保存在 ADR，过期实施记录不作为当前接口。详细计量口径见[指标定义](experiments/metrics.md)，当前进度见[backlog](backlog.md)。

## 证据存储与自存

Git 工作树只保存代码、现行文档、题包与必要的小型测试夹具。`validation/`、`reports/`、`artifacts/` 原始证据不提交 Git，也不作为默认测试或构建依赖，由操作者本机或外部存储保存；源项目的旧 Git 历史保留在原仓库，InferYard 新仓库不导入旧提交。仓内需要引用时只登记其 SHA-256 哈希与位置指针，指针登记不改变证据资格语义。核验以自存原件的字节哈希为准；原件缺失时如实标记，不补造观测。

## macOS 来源与后端

macOS/Linux 的普通 single 与 batch 都通过平台资源采集器记录 CPU、内存和可得传感器，
包括 probe 及重跑/续测产生的新运行。collector 观测声明实际来源和缺测；
同时保留既有换页、外部 CPU、请求边界观测。此范围变更依据
[ADR 054](decisions/054-single-resource-collection.md)，不改变 v3 字段或单位。
旧内存 single 继续只读，不要求补出 collector、CPU 或温度原件；新增采集不能继承旧开销资格。
Windows single/batch 与 lab 适配器的既有范围不随此变更扩展。

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

公开项目、Python 包和 CLI 名称统一为 `inferyard`。独立监测器已退役，旧监测流与摘要的边界见
[格式支持矩阵](contracts/inferyard-current-format.md#格式支持矩阵)。核心 Schema 仍为 v3，`urn:local-ai-bench:` 不变；
题包与审核证明原字节保留。报告仅接受 v8、比较仅接受 format4 / phase2.v3、公开包仅接受 v5。
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

见[按用途分派](contracts/purpose-specific-admission.md#按用途分派)。

## 身份与测量资格分派

[ADR036](decisions/036-scoped-measurement-cost.md) 和[职责身份契约](contracts/scoped-measurement-contract.md)
定义新 run/batch 的 implementation_identity、目录资产 model-assets.v2，以及
comparison format4 / phase2.v3、report8。历史重算已由 ADR038 取消，
plan/run 的 comparison 标记仍为 phase2.v1。新运行不再把 `tool_source_sha256` 当作第二道身份门；
执行身份只比较 measurement/scoring，未知依赖不能作相等证明。
没有 `implementation_identity` 的旧运行仍用其 `tool_source_sha256`。
新 `request_started` 不内嵌 `messages` 全文，保存 `messages_sha256`，须与请求快照一致；
旧事件里的全文按原字节读取。回答 chunk 与终态全文的双份核对不在此项。
每次身份守卫核对 PID、启动时刻、可执行文件、启动参数、监听和已声明的 slots_debug。
模型绑定只核对启动参数路径的设备号与 inode；Linux 不为此读 `maps`。
新运行只持久化原始请求事件，不写 requests.jsonl；
混入旧副本的来源明确拒绝。校准总开销与增量诊断分开，适用域及未覆盖条件见主题契约。

## 本地引擎显式定义

`local-engine-config.v1`、安装快照、完整服务绑定与逐请求队列记录在现有 v3 外壳下
独立识别。字段与旧单文件模型身份的边界见[本地引擎契约](contracts/local-engines.md)；
旧配置和旧 native/lab 定义不重解释，完整本地身份验证失败不降级为旧读取路径。

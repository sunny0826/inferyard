# InferYard 当前格式与主机状态契约

状态：现行契约。软件检查与原生验证范围见 [backlog](../backlog.md)。
依据 [ADR 038](../decisions/038-inferyard-current-format.md)（格式边界）与
[ADR 039](../decisions/039-remove-host-state-migration.md)（主机状态），writer 核查基线为
`fe1052a41afbddc1b7ceff852e794d6d872977ce`。
证据解释沿用[血缘规则](../data-contract.md#证据血缘与比较结论)。

## 格式支持矩阵

“当前”指该基线公开生产入口能够新建的格式，不含仅供旧格式重算的 helper 参数。
同格式旧产物不按日期或旧品牌拒绝；支持格式仍须通过结构、语义、封存与来源校验。
下表数字属于各自产物命名空间，不能相互替代。删除栏是明确删除集合。

| 产物 | 必须保留 | 删除或拒绝 | writer / 依赖证据（相对 src/inferyard） |
| --- | --- | --- | --- |
| 核心 config/bundle/experiment/plan/run/selection/event/sample/manifest/summary/analysis | `schema_version=3`；现行 kind、ID、可选字段与定义版本 | 核心 v1/v2 读取与 migrate；`origin=migrated` 运行及 migration.json 递归读取 | [常量](../../src/inferyard/__init__.py)、[planning](../../src/inferyard/config/planning.py)、[Journal](../../src/inferyard/evidence/journal.py) |
| 题包审核 | 原 review_records、case-review.v1、review_provenance 原字节及验证 | `upgrade_legacy_bundle` 生成入口；旧 bundle 作为活动输入 | [bundle_review](../../src/inferyard/config/bundle_review.py)；[zh-smoke](../../bundles/zh-smoke.json) 内嵌 v1 证明，不可连同迁移器删除 |
| 核心报告 | `report_format_version=7`、presentation-seal.v1、当前根模板 | report v1–v6 分派；templates/v2、v3、v4、v5 与根 report_v1.html | [write_report](../../src/inferyard/reporting/report.py)、[模板映射](../../src/inferyard/reporting/report_assets.py)；v6/v7 共用根模板，根模板必须保留 |
| 核心比较 | `format_version=4`，计算 definition=`phase2.v3` | comparison v1–v3 的重算与投影 | [write_comparison](../../src/inferyard/reporting/comparison_report.py)；plan/run 的 comparison 标记仍为 phase2.v1，不改写 |
| engine-fit plan | engine_fit_plan.v1/v2/v3/v4 全部保留 | 未知定义；不删除 v1 全目录模型清单 | [prepare](../../src/inferyard/config/engine_fit.py)：目录且仅 vLLM/SGLang、无覆盖写 v1；普通新引擎/单 GGUF 写 v2；温度覆盖写 v3；内存覆盖写 v4 |
| engine-fit run | engine_fit_run.v1–v6 全部保留 | 非法 plan/run/platform 组合及未知定义 | [run writer](../../src/inferyard/runtime/engine_fit.py)：plan.v1 在 Linux 写 run.v1、Darwin 写 run.v2；plan.v2/v3/v4 在非 Windows 写 run.v3/v4/v5；Windows 单 GGUF llama.cpp 写 run.v6 |
| engine-fit 封存/比较/中断 | engine_fit_manifest.v2、engine_fit_comparison.v1、engine_fit_checkpoint.v1 | manifest.v1 重渲染兼容；坏 seal 不可回退 checkpoint | [seal/compare](../../src/inferyard/reporting/engine_fit.py)、[checkpoint](../../src/inferyard/evidence/engine_fit_checkpoint.py) |
| observer | schema 3、lab_observer.v2、lab_observer_summary.v2 | lab_observer.v1 的流解析/缺字段回退 | [Go writer](../../observer/internal/observe/types.go)、[summary writer](../../scripts/analyze_observer.py)、[流校验](../../scripts/observer_stream.py) |
| 公开包/复现 | public-summary.v5 / format 5、public-metrics.v1、核心 v3 复现计划 | public-summary.v1–v4 的投影、核验及 public-plan 回退 | [package](../../src/inferyard/reporting/public_package.py)、[metrics](../../src/inferyard/analysis/public_metrics.py)、[public_plan](../../src/inferyard/config/public_plan.py) |
| 评分/重评分/导出 | 实时 scorer phase2.v1；显式 rescore 可选 phase2.v1/v2；rescore/export format 1 | 旧格式评分转换；任何隐式重评分 | [scoring](../../src/inferyard/analysis/scoring.py)、[revision](../../src/inferyard/analysis/scoring_revision.py)、[rescore](../../src/inferyard/reporting/rescore.py)、[export](../../src/inferyard/reporting/export.py) |
| 身份/资产 | implementation-identity.v1、model-assets.v2；plan.v1 当前使用的无 definition 全目录清单 | 仅由历史 plan.v2–v4 触发的无 definition 目录回退；旧比较身份算法 | [IdentityContext](../../src/inferyard/implementation_identity.py)、[assets](../../src/inferyard/config/engine_fit_assets.py)、[model_manifest](../../src/inferyard/platforms/engine_fit.py) |
| token 预算/原始日志 | token-budgets.v2.json / token-budgets.v2；原始 events、样本与请求前身份检查点 | token-budgets.json 位置数组；旧 requests.jsonl 派生副本专用重建/兼容 | [预算 writer](../../src/inferyard/evidence/token_budgets.py)、[ledger](../../src/inferyard/evidence/ledger.py)、[Journal](../../src/inferyard/evidence/journal.py) |
| 扩展及开销 | closed_concurrency.v1、native_tools.v1、total_observer_control.v1/v2；extension_event.v1、trial_control_baseline.v1；当前开销、重复和谱系定义 | 不按 v1/v2 扫除；此任务不重编独立实验协议 | [freeze/run](../../src/inferyard/extensions/workflow.py)、[extension journal](../../src/inferyard/extensions/extension_evidence.py)、[baseline](../../src/inferyard/extensions/trial_control_baseline.py) |
| 准备/发行/锁 | community_init/config_assets/config_candidate/config_binding.v1；installed_safe_checks.v3；community_distribution.v2；运行 state 版本 1 | 安装旧 v2 不继承新资格；其他编号不因数字小而删除 | [CLI 字段](../cli-surface.md)、[发行工具](../../scripts/README.md)、[HostLock](../../src/inferyard/runtime/lock.py) |

保持 `urn:local-ai-bench:`、目录方法/指标 ID、题目 ID、bundle ID 与哈希算法。
核心 Schema 中 `origin=migrated` 等历史枚举不借本次改变结构定义；应用读取入口明确拒绝其运行包。
删除 migrator 不删除审核证明解析：后者只验证嵌入原字节、原批准及内容等价，不接受旧 run，
不生成新审核。题包、随包副本、审核 HTML 与 `phase2_review.html` 不改字节。

当前 Schema 允许缺省的字段仍按现行语义处理。缺少 implementation_identity 不补当前摘要，
未知身份不授比较资格；缺预算的未发送/中断证据不补 0。不能以“当前格式”为由新增全局必填字段。
同一产物混入旧预算、迁移回执或旧派生请求声明时显式拒绝，不忽略后继续出成绩。
冻结计划、来源链、重评分、公开复现等入口递归应用相同支持集合，不允许从派生产物绕过。

## 拒绝语义

| 情形 | 结果 |
| --- | --- |
| 能严格识别版本，但在删除集合或未知集合内 | `unsupported_format`，退出 2；诊断包含 artifact、保存版本、支持集合，提示旧数据回原项目 |
| `migrate` / `host-state` 旧命令 | 命令注册已删除；CLI 参数错误退出 2，帮助不提及历史迁移，不保留转换 stub |
| 缺版本/版本类型不合法、坏 JSON、重复键、非有限数、bool 冒充整数 | 输入配置退出 2；被核验证据退出 4；不默认当前或猜测旧版 |
| 当前声明但 seal/来源/哈希/语义坏，或格式声明冲突 | 退出 4；不降格为 unsupported 或未封存，不切换模板重试 |
| 当前未封存证据，仅末尾截断 | 按原完整前缀规则 partial/3；不补终态、评分或 dirty 结论 |
| 当前格式且验证通过 | 保持既有 0/3/130 及产物专用语义，证据完整不等于执行完整或可比 |

先按各产物既有结构与已声明的大小限制，做严格 JSON、类型及可用封存完整性检查，再确定版本拒绝。
通用证据 JSON 读取不新增全局 16 MiB 上限；配置、observer、host state 各自已有或明确冻结的限制保持不变。
当前报告可由合法输入聚合超过单份配置大小，普通核验及显式 rerender 均应接受；不为核验旧格式而恢复旧评分器/模板。
可识别旧封存的哈希损坏仍为 4；无从解释的未知格式只报告 unsupported，不声称其内容完整。
冻结 plan 的可解释 `plan_sha256`、rescore 的既有自哈希与来源摘要先于版本拒绝核验；
坏哈希或 plan 内外版本声明冲突为 4，不能因同时带有旧格式标记而降格为 2。
任何拒绝都不覆盖输入、不生成成功产物、不调用模型或自动 rescore。
report v7/comparison v4/engine-fit manifest.v2 保持保存字节与显式 `--rerender` 分离；
普通相对来源与显式根映射保留，携带包继续拒绝路径逃逸与外部映射。

## 主机状态

依据 [ADR 039](../decisions/039-remove-host-state-migration.md)，主机状态只服务 InferYard 自身的
互斥与崩溃 dirty 持久化，不再迁移、退休或核验任何旧工具状态。

### 固定路径与初始化

| 平台 | 运行 lock / state |
| --- | --- |
| Linux、macOS | `/var/tmp/inferyard-host.lock`、`/var/tmp/inferyard-host.state.json` |
| Windows | 原生 Common AppData 下 `inferyard-host.lock`、`inferyard-host.state.json` |

实时入口首次获得 `HostLock` 时，若 state 不存在则直接创建
`{"schema_version": 1, "dirty": false}` 的 clean 状态；不需要任何显式初始化命令。
不接受 cwd、环境变量或 CLI 任意锁根，不在路径失败时回退；测试仅注入隔离临时根。

### 遗留文件与旧工具

- 旧工具的 `local-ai-benchmark-host.lock/.state.json`（含 Windows 旧 D 根位置）不读取、
  不加锁、不退休。InferYard 与旧工具之间没有跨工具互斥；需要隔离旧工具时由操作者自行管理。
- v0.0.1 状态中的 `migration` envelope 不再核验，`dirty` 语义照常执行；envelope 在下一次
  状态写入时消失。遗留 `inferyard-host-migration.json` 凭据文件视为无关文件，保持原样。
- `host_state_migration_required`、`host_state_initialization_required`、
  `host_state_migration_pending` 等迁移错误码已删除。

### 保留的运行边界

主机互斥（InferYard 进程之间）、崩溃 dirty 持久化、人工恢复确认（token、注释、
新进程身份）、锁/根/文件身份核验、安全文件检查（权限、nofollow、链接数、reparse）、
非阻塞锁竞争失败即退出、句柄清理失败保留错误、不删除锁文件，全部不变。
文件权限、路径逃逸、权限失败、锁占用、dirty 为预检阻断/2；原子写/flush/fsync 工具故障为 4；
取消为 130；所有阻断分支请求数为零。

## 安装检查与资格版本

清理前的 [installed_probe.py](../../tests/packaging/installed_probe.py) 在三次安装探针中生成 report v1–v6，
并计算 v1–v7 模板摘要；[release_common.py](../../scripts/release_common.py) 的旧 SCOPE 包含
`synthetic_historical_reports`，旧 `validate_installation` 要求 installed_safe_checks.v2。
这不是可原样保留的当前安装资格。

- 探针改用当前 writer 生成 report7、verify 和显式 rerender；只核验当前模板摘要。
  对 v1–v6 构造最小旧格式负例，通过 CLI 确认退出 2 / unsupported_format；不打包旧 renderer。
  不以篡改当前 seal 后得到的哈希错误冒充旧版本拒绝。安装/第二环境/回退三个探针都执行新范围。
- 探针输出改为 `current_report_formats_verified=[7]`、
  `unsupported_report_formats_rejected=[1,2,3,4,5,6]`、`template_hashes={"7": ...}`；
  删除 synthetic_historical_reports_verified，逐版失败不能被总 returncode=0 掩盖。
- 安装结果 **必须升级为 installed_safe_checks.v3**。scope 保留原六项，将
  synthetic_historical_reports 替换为 `current_format_reports` 和 `unsupported_format_rejection`。
  既有 probe 命令名可保留，执行语义由结果 v3/scope 绑定；测试结果仍是零模型请求，host_lock_tests=not_run。
- v3 新增必需 `report_format_checks` 对象，恰含 probe-install-a、probe-install-b、probe-rollback 三键；
  每项保存对应探针实得的 current_report_formats_verified、unsupported_report_formats_rejected、template_hashes。
  run_installed 先校验三次 stdout 再汇总；validate_installation 要求三项都完整，模板摘要与已核验 wheel 的当前模板一致，
  不能仅写新 scope 或只检查探针退出 0。
- run_installed 与 release_common 的 writer/validator、prepare/verify_release_candidate 消费链及夹具同步；
  明确拒绝 v1/v2 结果、旧 scope、缺负例、缺当前成功、来源/字节失配。不把旧结果改个 kind 即视作重验。
- community_distribution.v2 的 build/candidate 外壳和安装结果摘要引用结构不变，故不连带升版；
  新候选仅接受安装 v3，旧候选中的安装 v2 明确失败。旧核验器也会因不认识安装 v3 而失败，
  双方不默默继承资格。至少一个平台重新完成同批字节安装，其余保持 not_verified。

这是安装验证语义改变所需的单独升版，不改变产品版本、核心 schema 3 或业务协议编号。
安装安全探针不操作系统真实锁；锁互斥与 dirty 持久化证明由隔离临时根的进程测试提供。

## 验证边界

安装结果 v3 与候选外壳 v2 绑定同批源码及产物字节；旧安装资格不自动继承。
隔离目录进程测试不代替 Windows 原生验证，平台实际覆盖见 [backlog](../backlog.md)。

## 不变的运行边界

主机锁、dirty、请求前身份检查、停止阈值、取消/排空、五终态分母、来源哈希和人工审核保留。
不因清理改变模型/引擎支持、SCHEMA_VERSION、SVG 免评分语义或 null 缺测规则。
模板删除后的职责清单按真实源码更新；旧来源哈希继续如实验证，不映射成当前身份。
测试只在隔离临时目录，以真实子进程加合成服务完成，不操作系统实际锁或发模型请求。

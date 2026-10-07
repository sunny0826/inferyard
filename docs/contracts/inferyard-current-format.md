# InferYard 当前格式与主机状态契约

状态：T0 冻结提案，待 Reviewer/父会话通过；不代表源码已经实现。
依据 [ADR 038](../decisions/038-inferyard-current-format.md)，writer 核查基线为
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
| 准备/发行/锁 | community_init/config_assets/config_candidate/config_binding.v1；installed_safe_checks.v2、community_distribution.v2；LOCK_FORMAT_VERSION=1 | 不因数字小而删除；已拒绝的旧发行回执仍拒绝 | [CLI 字段](../cli-surface.md)、[发行工具](../../scripts/README.md)、[HostLock](../../src/inferyard/runtime/lock.py) |

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
| `migrate` 旧命令 | 删除注册和执行器；CLI 参数错误退出 2，帮助只说明历史处理移交；不保留转换 stub |
| 缺版本/版本类型不合法、坏 JSON、重复键、非有限数、bool 冒充整数 | 输入配置退出 2；被核验证据退出 4；不默认当前或猜测旧版 |
| 当前声明但 seal/来源/哈希/语义坏，或格式声明冲突 | 退出 4；不降格为 unsupported 或未封存，不切换模板重试 |
| 当前未封存证据，仅末尾截断 | 按原完整前缀规则 partial/3；不补终态、评分或 dirty 结论 |
| 当前格式且验证通过 | 保持既有 0/3/130 及产物专用语义，证据完整不等于执行完整或可比 |

先做有界严格 JSON、类型及可用封存完整性检查，再确定版本拒绝；不为核验旧格式而恢复旧评分器/模板。
可识别旧封存的哈希损坏仍为 4；无从解释的未知格式只报告 unsupported，不声称其内容完整。
任何拒绝都不覆盖输入、不生成成功产物、不调用模型或自动 rescore。
report v7/comparison v4/engine-fit manifest.v2 保持保存字节与显式 `--rerender` 分离；
普通相对来源与显式根映射保留，携带包继续拒绝路径逃逸与外部映射。

## 锁迁移

### 路径与安全桥

| 平台 | 新主 lock / state | 持续持有的旧安全桥 |
| --- | --- | --- |
| Linux、macOS | `/var/tmp/inferyard-host.lock`、`/var/tmp/inferyard-host.state.json` | 同目录 `local-ai-benchmark-host.lock` / `.state.json` |
| Windows | 原生 Common AppData 下 `inferyard-host.lock`、`inferyard-host.state.json` | Common AppData 下旧名；旧 D 根可用时还包括 `D:/local-ai-benchmark-host.lock` / `.state.json` |

Common AppData 沿用 [原生 known-folder 查询](../../src/inferyard/platforms/windows_host_paths.py)，
不用环境变量、用户临时目录、cwd 或失败回退改变互斥域。
锁/state schema 仍为 1，它不是测量 schema。生产 CLI 不接受任意锁路径覆盖；测试仅注入临时根。
Windows D 根的可用域尚受下面父会话裁定项限制，不能据此宣布无 D 盘路径已安全迁移。

每次实时执行、恢复和维护都按固定顺序持锁：Windows D 旧锁（适用时）→公共旧锁→新锁；
POSIX 公共旧锁→新锁。任一非阻塞获取失败，释放已获得的句柄并退出 2，不等待后偷跑。
从准入前到排空、必要封存结束始终持有全部锁；每个旧版本至少与新版本共享一个内核锁。
锁文件不 unlink、不 replace、不改名；只原子替换 state。不能只在迁移时短暂持有旧锁。

### 首次建立与显式迁移

“首次”只是已检查的所有固定位置均无 lock/state 的文件系统状态，不声称能识别人为删除的历史。
记录排他创建结果，并在全部锁获得后重读状态，不能用 `Path.exists()` 吞掉权限错误来判定首次。

| 已锁定后的观察 | 行为 |
| --- | --- |
| 所有旧/新位置原先均不存在 | 初始化全部 clean state，新 state 最后持久化；任何失败都停，不发请求 |
| 新 state 不存在，任一旧 lock/state 已存在 | 普通运行退出 2 / `host_state_migration_required`，不自动接管 |
| 旧 state 全部存在、合法且 clean | 仅拟新增 `inferyard host-state migrate` 可在全部锁下复制到新位置；无请求、无服务启动/停止、返回 ready_to_run=false |
| 旧 dirty、旧锁被占、状态缺失或损坏 | 维护入口拒绝；dirty 先走原项目原有 token/旧进程退出/新身份及 idle 恢复；本入口不清理、不接受 force/reset |
| 新 state 已存在且各桥 state 完整 | 普通运行核验全部状态；任何 dirty 优先于 clean，不能以较新文件或时间戳选择 clean |
| 新 state 已存在但桥 state 缺失、不同 dirty token/身份并存 | fail-closed；不自动生成 clean 镜像或挑选赢家 |

维护重复调用只在状态完整、均 clean 时报告已迁移；无成功回执的部分初始化不得当作新安装。
初始化/迁移途中中断留下的 lock 或缺失 state 阻断普通运行；全锁下可重做已证明 clean 的复制。
不能证明 clean 的孤立锁、损坏 state 交操作者调查，禁止删锁解阻断。
状态移交不授予服务就绪：后续请求仍完整核验本机端点、资产、PID/启动时间、有效参数与 idle。

### dirty、崩溃与旧工具再启动

1. 请求身份检查点先持久化；同一个 dirty token 与绑定身份写入每个旧 state，再写新 state。
   所有写入和所需 fsync 成功后才允许发送请求；中途失败停止且不清除已有 dirty。
2. clean 只发生在原有可核验排空或严格恢复之后。先写新 clean，再写各旧 clean；
   半途失败保留剩余 dirty，下次不按时间戳放行。取消/超时未证实 idle 时全部保留 dirty。
3. 恢复绑定现有 dirty token、说明、旧服务确已退出、新服务身份与 idle；旧 PID 活着或复用身份不明即阻断。
   同 token 的 dirty/clean 不一致可在全部锁下保守恢复 dirty，再走原恢复检查；不同 dirty 身份拒绝合并。
4. 旧工具以后启动仍被旧内核锁挡住。InferYard 崩溃后，旧 state 已在发送前写 dirty，
   旧工具必须走它自己的恢复；不能把旧路径改成它不认识、可能忽略的迁移标记。
5. 旧工具串行运行后，旧 dirty 仍必须被新工具读取。若它只清理旧 state 而新 state 仍 dirty，
   新工具保守阻断/重新核验恢复，不把旧 clean 当作新恢复证明。双方 clean 可保留各自最后身份，无需改写历史。

该桥只保留固定路径、schema 1 dirty 与内核锁协议，不引入历史 run reader、后台守护进程、
全盘扫描、兼容数据库或无限迁移链。有限镜像的部分写失败允许多阻断，不能少阻断。

### 失败闭合

| 风险 | 强制行为 |
| --- | --- |
| 旧 runner 存活并持锁、多进程迁移竞争 | 同序获取共享内核锁，只有一个进入；失败者无请求，不释放他人锁 |
| dirty 服务仍活着、PID 复用、身份无法查询 | 不清 dirty；原身份/恢复失败继续阻断 |
| state 损坏、重复 JSON 键、非整数版本、dirty 非 bool、dirty 必需身份缺失 | `invalid_host_state` / 退出 2；文件保持，不能当首次或降为 clean |
| 不同 dirty 状态 | `conflicting_host_dirty_states` / 退出 2；不自动协调冲突 |
| 路径穿越、符号链接、硬链接别名、Windows reparse point、不安全所有者/权限 | 拒绝；固定根和打开句柄核验，POSIX 保持本账户 0600，不通过 chmod/chown 接管 |
| 打开/读取权限失败、根路径不可查询、锁不可获得 | `host_lock_unavailable` / 退出 2；不回退别处；跨账户不安全时宁可阻断 |
| 原子写/flush/fsync 失败、磁盘满、迁移中断 | 停止发送，保留状态；持久化工具故障退出 4，取消退出 130；下次缺失/矛盾状态继续阻断 |
| 释放某个句柄失败 | 尝试释放其余自有句柄，保留首个错误；绝不删除文件或放宽下一次准入 |

state 读取须绑定已核验文件句柄，防止检查后路径被替换；固定父目录和 inode/文件 ID、
链接数及 Windows ACL/reparse 核验不能仅靠字符串 resolve。现有实现不足处是实施工作，
不是本次已验证结论。跨账户已有文件不安全则显式失败，不另开每用户锁域。

## 父会话裁定项

1. **持续旧桥**：建议批准旧名 lock/state 作为有限安全协议持续保留，不设按日期自动移除。
   只要未修改旧工具仍可启动，完全去掉旧路径就无法证明互斥。若要求完全移除，需另有系统级禁用证据；
   此次不修改原项目、不安装守护进程，不能以口头停用替代。
2. **Windows D 根稳定性**：启动时“D 不存在”只证明当时旧 D-only 工具不能取锁。
   持锁期间新增/重新挂载 D 可建立独立锁域，轮询检测不能消除竞态。
   建议未能用原生证据保证旧路径域稳定或系统级阻止 D-only 工具时，迁移后的实时入口明确阻断；
   离线入口继续可用。是否接受此支持范围收窄由父会话决定；裁定前不实施 Windows 迁移放行，
   不将“保留当前 no-D 行为”写成已证明安全。已有 D 的卷替换也需原生句柄/卷身份验证。

## 不变的运行边界

主机锁、dirty、请求前身份检查、停止阈值、取消/排空、五终态分母、来源哈希和人工审核保留。
不因清理改变模型/引擎支持、SCHEMA_VERSION、SVG 免评分语义或 null 缺测规则。
模板删除后的职责清单按真实源码更新；旧来源哈希继续如实验证，不映射成当前身份。
测试和迁移演练只在隔离临时目录，以真实子进程加合成服务完成，不操作系统实际锁或发模型请求。

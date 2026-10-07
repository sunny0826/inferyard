# InferYard 当前格式与主机状态契约

状态：T0 已通过 Reviewer/父会话审核，P0 已放行；软件交付与原生验收状态见 [backlog](../backlog.md)。
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
| 准备/发行/锁 | community_init/config_assets/config_candidate/config_binding.v1；installed_safe_checks.v3；community_distribution.v2；运行 state 版本 1 | 安装旧 v2 不继承新资格；其他编号不因数字小而删除；退休标记见下文 | [CLI 字段](../cli-surface.md)、[发行工具](../../scripts/README.md)、[HostLock](../../src/inferyard/runtime/lock.py) |

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

先按各产物既有结构与已声明的大小限制，做严格 JSON、类型及可用封存完整性检查，再确定版本拒绝。
通用证据 JSON 读取不新增全局 16 MiB 上限；配置、observer、host state 各自已有或明确冻结的限制保持不变。
当前报告可由合法输入聚合超过单份配置大小，普通核验及显式 rerender 均应接受；不为核验旧格式而恢复旧评分器/模板。
可识别旧封存的哈希损坏仍为 4；无从解释的未知格式只报告 unsupported，不声称其内容完整。
任何拒绝都不覆盖输入、不生成成功产物、不调用模型或自动 rescore。
report v7/comparison v4/engine-fit manifest.v2 保持保存字节与显式 `--rerender` 分离；
普通相对来源与显式根映射保留，携带包继续拒绝路径逃逸与外部映射。

## 锁迁移

以下为 T0 已批准事务，软件实现与原生验收状态分别记入 backlog。对照方案与代价见 [ADR 038](../decisions/038-inferyard-current-format.md#替代方案与风险)。
目标是退休固定旧工具的实时入口，而不是让新旧工具迁移后继续执行测评。

### 固定旧基线与退休拒绝证明

只读核对源项目提交 `2fa125ccbe97d7d029fbbea494970f2336829ca2` 的
`src/local_ai_bench/runtime/lock.py`，与 InferYard 基线的
[lock.py](../../src/inferyard/runtime/lock.py) 在仅替换包名后逐字节一致。
旧文件 SHA256 为 `4bfa0a676b5c03a6e348cea180501d1e6a0df33dea260c69cc52807a05d138a1`，
新基线文件 SHA256 为 `3c969665902d636fb14b994e03601b7cd6e11ad373f987b066c109dba9d739ae`。
两者 windows_host_paths.py 字节相同；公共根均由原生 Common AppData 查询取得。

退休标记采用严格 JSON：`schema_version=2`、`definition="inferyard-host-retired.v1"`、
`migration_id`、`receipt_sha256` 四键；不含 dirty，不假装普通 clean 状态。
这是独立主机维护标记，不改变测量 schema 3 或现行运行 state schema 1。
旧 `_read_state` 要求 `schema_version == LOCK_FORMAT_VERSION == 1`，因此必抛
`PreflightError("invalid_host_state")`；单加未知 `retired=true` 字段则会被忽略，不能采用。

旧 `__enter__` 先获取所有锁、再读取所有 state，最后才选择 dirty/自动镜像并返回。
任一退休标记使其在镜像和返回之前失败；即使 D state 为 clean，也不能覆盖公共退休标记。
单次/probe、batch/resume、engine-fit、live 扩展、overhead/length 的 HostLock 准入均在模型请求之前；
实施验收须用固定旧实现及这些入口的请求计数确认，而不是只测新实现理解退休标记。
锁外脚本可能先启动服务或查询健康；本方案只证明受 HostLock 保护的生成请求被阻断，
不声称阻止操作者手动请求、其他客户端或任意更古老的无同等准入程序。

### 固定路径与常态责任

| 平台 | 新运行 lock / state | 需要退休的旧位置 |
| --- | --- | --- |
| Linux、macOS | `/var/tmp/inferyard-host.lock`、`/var/tmp/inferyard-host.state.json` | 同目录 `local-ai-benchmark-host.lock` / `.state.json` |
| Windows | 原生 Common AppData 下 `inferyard-host.lock`、`inferyard-host.state.json` | Common AppData 下旧名始终处理；维护时存在的 D 根旧 lock/state 也核验并退休 |

同一新根另存只写一次的 `inferyard-host-migration.json` 维护凭据。
不接受 cwd、环境变量或 CLI 任意锁根，不在路径失败时回退；测试仅注入隔离临时根。
旧 lock 文件永久保留原 inode，不 unlink/replace/rename；旧 state 可在保存原字节后原子替换为退休标记。
新工具提交 ready 后只持有新 lock、读写新 state，校验其绑定的本地维护凭据；
不再获取旧锁、解析旧 dirty 或双写状态。旧 state 的读取/重试只属于维护入口。

### 维护凭据与提交状态

| 对象 | 最小冻结字段与作用 |
| --- | --- |
| 不可变维护凭据 | `definition=host-state-migration.v1`、migration_id、固定旧/新基线 SHA、mode（fresh/migrate）、固定根及文件身份、旧槽位清单；每槽记录 lock/state 原先是否存在、state 原字节 base64、SHA256、长度；不存在用 null，不伪造 clean 原件 |
| 新 state 的维护 envelope | schema_version=1、`migration={definition: host-state-migration.v1, id, receipt_sha256, phase: pending或ready}`；仅 ready 后才允许使用原 dirty/token/进程身份字段 |
| 旧 state 退休标记 | 上述四键；全部绑定同一 migration_id 和凭据摘要；生成字节确定，不读凭据内任意路径来扩大目标 |

pending 不表示 clean，也不允许进入普通恢复流程；即便含 `dirty=false` 也必须阻断请求。
ready 后新 dirty/clean 写入必须保留 migration envelope，不得丢掉提交身份。
凭据不存新运行历史，不是 run 迁移包；原旧状态字节不改写为“等价 JSON”，摘要校验后才能重试。
维护入口不提供 force/reset/rollback，也不输出原始 token 或完整状态到诊断日志。

### 首次部署与显式事务

统一使用 `inferyard host-state migrate` 做首次建立或旧状态迁移；普通实时入口不自行创建 clean。
无新 ready 时，发现任一旧 lock/state 返回 `host_state_migration_required`；全部旧位置无文件则返回
`host_state_initialization_required`。pending、孤立凭据、缺失或损坏新 state 都不当作首次。
只读离线入口不建立主机状态。这样即便从未运行过旧工具，也须先退休旧入口再运行新工具。

维护事务只在全部必要锁下执行，顺序为 Windows D 旧锁（根存在时）→公共旧锁→新锁；
POSIX 公共旧锁→新锁。均为非阻塞锁；竞争失败即退出，释放已获得句柄。
固定旧实现若已持锁，维护不能接管；若正在竞争，它只能在事务之前完成或在退休后拒绝。

1. 记录排他创建结果和原存在性，获得全部锁后从安全文件句柄重读状态，并核对根/文件身份。
   每个原有 state 必须严格合法且 `dirty=false`，版本为整数 1（不接受 bool）；原有身份字段按固定 writer 核验。
   任一 dirty、损坏、不可读均拒绝。
   某槽原 lock/state 都不存在，可作为 fresh 槽建立退休标记；原 lock 已有而 state 缺失则拒绝，
   不把不明中断当空白部署。只有 state 的槽须取得对应锁并核验 clean，原始组合记录进凭据。
2. 排他发布不可变凭据，保存各槽原字节与摘要；持久化成功后发布新 pending state。
   此前不改任何旧 state；新目录中任何不属于本次事务的已有文件都拒绝覆盖。
3. **先退休公共旧 state，再退休已纳入的 D state**；每个标记原子发布、flush/fsync 和读回核验。
   全程持旧锁，旧进程无法在检查与替换之间写状态；中途失败不回写 clean、不撤销已发布标记。
4. 全部槽位的退休标记、凭据摘要、根/锁身份再次一致，才原子提交新 state 的 `phase=ready`、
   初始 `dirty=false`。最后释放旧锁与新锁；结果 `ready_to_run=false`，迁移不宣告服务就绪。
   新运行另行核验本机端点、资产、PID/启动时间、有效参数、idle、预算及停止条件。

迁移原 clean 不等于必须杀掉仍空闲的服务；旧 runner 持锁时不能迁移，旧 dirty 服务则始终拒绝。
维护不调用旧工具恢复、不杀服务、不清空 dirty。操作者须在退休开始前用原项目已有恢复流程解决旧 dirty。

### 中断与幂等重试

| 持久状态 / 故障点 | 重试与放行规则 |
| --- | --- |
| 仅创建了锁，尚无完整凭据 | 不碰旧 state，不发新请求；无来源证明的 lock-only 状态保守拒绝，需调查，不删锁重置 |
| 凭据完整，新 pending 尚未成功发布 | 普通运行阻断；维护重取全锁，仅在 state 原字节/原不存在性与凭据一致时发布 pending；已创建 lock 按凭据记录的句柄身份核验，不要求它重新消失 |
| pending，旧槽未退休或部分退休 | 只接受“与凭据原件完全相同”或“同一事务准确退休标记”；fresh 槽可仍不存在；其他内容包括新 dirty、不同标记、变更 clean 都拒绝 |
| 旧入口已全部退休，新 ready 未提交 | 新旧实时入口均阻断；维护核验后幂等完成同一 ready 提交，不生成第二份凭据 |
| ready 已持久化但调用方未收到成功 | 重复维护核验同一凭据/退休标记后返回 already_migrated，不改新 dirty 或回写初始 clean；新 dirty 时维护拒绝并提示新工具恢复 |
| 新 state/凭据损坏或丢失、摘要错、目标身份变化 | fail-closed，不从旧退休标记反推新 clean，不隐式覆盖异常状态 |

退休首写之前崩溃，旧工具仍可能正常运行；此时新工具还没有 ready，二者不会同时发送。
旧工具后来改了原 clean 字节，维护重试必须拒绝旧快照，不能按过期凭据覆盖。
退休首写之后固定旧基线始终拒绝；新工具直到提交 ready 才能执行。
新运行崩溃后的 dirty 只由新 state 持久化及原有恢复规则处理；旧入口继续退休，不能绕过新 dirty。

文件权限、nofollow、普通文件、链接数/文件身份、Windows reparse/ACL 及固定根边界继续核验。
路径逃逸、权限失败、锁占用、dirty、输入状态冲突为预检阻断/2；原子写/flush/fsync 工具故障为 4，
取消为 130；所有分支请求数为零。释放一柄失败仍尝试释放其余自有句柄并保留错误，不删除锁文件。
Windows 保留 file fsync + MoveFileExW(WRITE_THROUGH) 和 `directory_fsync=false` 的真实范围；
本事务要求进程崩溃/中断验证，不凭模拟宣称跨文件断电持久性。原生证据缺失时明确未验。

### Windows 可证范围与具体反例

| 情形 | 固定旧基线行为与方案范围 |
| --- | --- |
| 无 D 盘部署 | 固定旧 `_paths()` 仍包含 Common AppData；退休公共 state 即阻断该基线，不强制要求 D 盘或全局禁用旧 EXE |
| 维护时 D 存在 | 先取 D 旧锁，再取公共旧锁；D 原状态也须 clean/合法/可读。即使公共位置 clean，D dirty 也不能跳过 |
| 迁移后新增 D，或其 state 为 clean | 固定旧基线取得 D 后仍读公共退休 state，在自动镜像前失败；不能用 D clean 覆盖退休标记 |
| 迁移后 D 上是 dirty、损坏、权限阻断或锁被占 | 旧基线提前拒绝或稍后在公共标记拒绝，均不发请求；已有新 ready 不要求持续镜像或重读 D |
| 事务期间 D 根消失/换卷 | 已纳入的根/文件身份变化则不提交 ready；已写退休标记不撤销。管理员竞态下不声称能以轮询证明无限热插拔安全 |
| 管理员替换公共卷、删退休 state/锁，或修改 known-folder 指向 | 具体反例：旧基线看到新空公共根，且有 clean/无 D 状态，可以重新进入；属于破坏共享状态的外部管理操作，本方案与旧系统均不能保证，不能宣称已防护 |
| 仅用 D 锁、忽略公共根的更古老程序 | 若部署时 D 不存在、后来挂载空 D，该程序可能创建独立锁并运行；不在固定 2fa125c/fe1052a 保证集合，不能将其当作本次必需永久兼容 |

管理员卷操作和状态删除不等于正常并存支持；固定基线在保留公共退休标记的普通部署下可证拒绝。
不因任意古老程序或管理员破坏场景强制增加永久桥接。未发现该固定基线在标记完整、路径稳定、
正常 HostLock 准入下的绕过路径；这是静态结论，仍须原生/真实进程验收。

## 安装检查与资格版本

基线 [installed_probe.py](../../tests/packaging/installed_probe.py) 在三次安装探针中生成 report v1–v6，
并计算 v1–v7 模板摘要；[release_common.py](../../scripts/release_common.py) 的 SCOPE 包含
`synthetic_historical_reports`，`validate_installation` 要求 installed_safe_checks.v2。
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
安装安全探针不操作系统真实锁；迁移证明仍由隔离临时根的进程测试及独立 Windows 原生检查提供。

## 父会话终审边界

1. T0 已通过 Reviewer/root，P0 已批准；实现、定向证据和独立终审仍分开记录。
2. 请接受或具体指出固定 2fa125c/fe1052a 范围内的反例。任意古老 D-only 程序、管理员删除状态/
   更换公共根、断电持久性是明确边界，不再把它们扩大成永久兼容义务；Windows 原生尚未验。
3. 安装结果 v3 和候选外壳 v2 的组合已批准；实际构建安装由父会话终验，不能继承旧安装资格。

## 不变的运行边界

主机锁、dirty、请求前身份检查、停止阈值、取消/排空、五终态分母、来源哈希和人工审核保留。
不因清理改变模型/引擎支持、SCHEMA_VERSION、SVG 免评分语义或 null 缺测规则。
模板删除后的职责清单按真实源码更新；旧来源哈希继续如实验证，不映射成当前身份。
测试和迁移演练只在隔离临时目录，以真实子进程加合成服务完成，不操作系统实际锁或发模型请求。

# ADR 038：InferYard 当前格式边界与主机状态迁移

状态：Accepted。日期：2026-10-08。
软件实现与平台实际验证范围见 [backlog](../backlog.md)。
核查基线：`fe1052a41afbddc1b7ceff852e794d6d872977ce`。

## 决策

InferYard 只支持[格式矩阵](../contracts/inferyard-current-format.md#格式支持矩阵)中的当前产物。
以公开生产入口实际调用的 writer 为依据，不按版本后缀、文件年代或品牌判断。
删除历史运行迁移、旧报告重建和仅供旧格式使用的读取分支；历史数据由原项目处理。
已有自包含 HTML 可继续直接浏览，InferYard 不再核验或重建被移除格式。

核心 `schema_version=3`、现行字段含义、题包字节及审核哈希、已有 ID 不变。
仍在生产的 engine-fit plan.v1–v4/run.v1–v6、评分 phase2.v1/v2 等不是删除目标。
题包嵌入审核证明的验证是当前输入依赖，保留最小验证代码，删除生成旧格式迁移包的入口。
移除读取支持不授权重评分、重签审核、补造身份或改变性能资格。

锁方案采用已批准的“一次性显式迁移 → 固定旧入口退休 → 新工具独立 lock/state”，
不把永久持有旧锁和 dirty 镜像冻结为结论。迁移仅接受已核验 clean 状态；旧锁占用、dirty、
损坏或不可读均拒绝，不代替旧工具恢复、不杀服务、不清空 dirty。
旧状态原字节及摘要先留存，经 pending 状态完成所有旧入口退休后，才提交新 ready 状态。
旧锁文件 inode 不删除或替换；常态运行只持有新锁，不再镜像旧 dirty。
旧项目保留离线读历史数据能力；迁移后不再允许其固定基线实时入口发送请求。
具体证明、崩溃边界和 Windows 路径分析见[锁迁移](../contracts/inferyard-current-format.md#锁迁移)。

## 局部取代范围

| 原决策或规则 | 被取代的兼容承诺 | 继续有效 |
| --- | --- | --- |
| [ADR 001](001-unified-contract.md) | 提供 v1/v2 run/bundle 显式升级及 migrated run 读取 | 单一核心 v3、原件不覆盖、审核证明与血缘校验 |
| [ADR 034](034-command-local-reuse.md) | 读取旧 token-budgets.json 位置数组 | token-budgets.v2、命令内复用、逐次探测、预算与评分身份 |
| [ADR 035](035-purpose-specific-admission.md) | 旧 comparison/report 按版本重建 | 描述与资格分开、环境准入、评分分母、逐题审核和服务排空 |
| [ADR 036](036-scoped-measurement-cost.md) | 旧比较/报告重算、旧派生请求副本专用通路、仅为旧资产定义而设的回退 | 职责身份、全源码摘要、总开销适用域、仍由 plan.v1 writer 使用的全目录模型身份 |
| [ADR 037](037-offline-evidence-reading.md) | report v1–v6、comparison v1–v3、engine-fit manifest.v1 的旧读取/重渲染规则 | 原始产物 verify、请求前 checkpoint、坏 seal 不降格、来源映射及携带包路径边界 |
| [AGENTS](../../AGENTS.md) 的文档与契约、执行与证据、交付条款 | 必须提供旧证据迁移、必须保留历史报告兼容模板、旧兼容成功路径测试 | 主机互斥、dirty、身份、取消排空、题包审核资料、拒绝路径测试及文件所有权 |

[ADR 009](009-native-observer.md)、[010](010-observer-benchmark-adaptation.md) 的原生观测和只读关联继续有效；
其主题契约中 observer.v1 的兼容读取由矩阵收窄为 v2。
[ADR 011](011-report-dashboard.md) 的离线自包含展示不变。
[ADR 012](012-engine-fit.md)、[013](013-engine-fit-macos.md)、[014](014-macos-common-engines.md)、
[016](016-engine-fit-temperature-override.md)、[019](019-engine-fit-memory-override.md)、
[020](020-windows-engine-fit-native.md) 的当前 writer 分派、原生来源和显式停止覆盖全部保留。
旧定义不因此获得新的平台或引擎能力。

上述局部取代已获批准；原 ADR 保留历史理由并添加局部取代说明。
数据契约和 CLI 中冲突的历史兼容描述按本 ADR 同步。
未列入矩阵删除项的命令别名、协议、指标或现行可选字段不顺带清理。

## 替代方案与风险

| 方案 | 取舍 |
| --- | --- |
| 保留全部兼容 | 继续维护旧模板、投影和迁移测试，不满足用户范围；不采用 |
| 按 v1/v2 批量删除或统一重编号 | 会删除现行 engine-fit、评分、锁状态与扩展协议；不采用 |
| 直接换新锁、未退休旧入口 | 旧工具未来可并发运行，崩溃后可绕过新 dirty；拒绝 |
| 不改锁名 | 不增加迁移事务，安全成本最低；保留旧路径/Windows 双路径及旧工具实时能力，未实现独立起步，作为对照方案 |
| 新路径加持续桥接/dirty 镜像 | 可让两工具继续串行运行，但新增第三路径、镜像故障和持续兼容负担；不作为首选或既定结论 |
| 显式迁移加退休标记、新锁独立 | 采用；事务仅在维护入口，正常运行无旧状态镜像；代价是固定旧入口永久拒绝实时执行，须证明事务中断安全 |

固定旧基线为源项目 `2fa125ccbe97d7d029fbbea494970f2336829ca2`。
与 InferYard 基线的 HostLock 仅包名不同；它拒绝非 1 的 state schema，且拒绝发生在
`__enter__` 完成及自动镜像之前。不能仅添加它会忽略的 `retired=true` 字段。
该证明限定经过核对的旧 HostLock 入口；不承担任意古老程序或管理员破坏状态后的互操作。

旧报告 CLI 验证能力消失是有意的破坏性变更；提示回原项目处理，不自动重建。
模板和源码删除会改变真实源码/职责摘要，不伪造旧身份或继承旧测量资格。
旧题包证明、现行低版本协议误删是主要回归风险，须有正向保留测试。
退休标记不是测量或服务 idle 证明，迁移后新运行仍核验服务身份、预算、空闲及停止条件。
首选方案不自动回滚退休标记；恢复旧工具实时执行会重新打开竞争域，不在本次范围内。

安装检查由历史报告成功变为当前格式成功及旧格式拒绝，因此结果升级为
`installed_safe_checks.v3`，不复用旧 v2 的 scope 或通过结论。
`community_distribution.v2` 字节绑定外壳保持，候选生成/核验仅接收新的安装结果；
细节见[安装证据版本](../contracts/inferyard-current-format.md#安装检查与资格版本)。

## 拒绝与验收

完整、可识别但不支持的格式返回 `unsupported_format`/退出 2，并标明格式及支持集合。
损坏 JSON、坏封存、来源/哈希不符仍为证据错误/退出 4，不伪装成普通旧版本。
缺版本不默认当前，不升级、不补评分；当前未封存完整前缀仍按原规则 partial/3。
锁阻断不得发送任何模型请求；失败分类见[契约](../contracts/inferyard-current-format.md#拒绝语义)。

验收要求：矩阵正反例、当前题包审核和哈希不漂移、旧模板不进发行包、当前安装与离线报告可用；
隔离目录真实进程证明迁移与旧进程互斥、退休后旧入口请求数为零、新锁互斥与 dirty 持久化，
逐事务写入点崩溃后新工具不提前运行；安装 v3 正反例与旧安装资格拒绝均通过。
实施验收同时要求集成回归和代表性离线/进程验证；
Windows 原生未验单列，不以模拟或跨平台编译替代，不发送真实模型请求。

实现的适用范围见[验证边界](../contracts/inferyard-current-format.md#验证边界)。

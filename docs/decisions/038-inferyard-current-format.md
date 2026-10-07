# ADR 038：InferYard 当前格式边界与主机状态迁移

状态：Proposed，T0 待独立 Reviewer 与父会话审核。日期：2026-10-07。
用户已授权删除历史数据兼容；本 ADR 的实施门尚未通过。
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

主机锁使用新的 InferYard 主路径，但保留旧路径作为持续互斥桥和 dirty 安全镜像。
仅迁移路径职责，不删除安全状态、不改锁文件 inode，不通过重命名解除阻断。
显式维护入口只迁移已核验 clean 状态，不读取旧 run/报告，也不清除 dirty。
所有实时入口继续共用 HostLock；规则见[锁迁移](../contracts/inferyard-current-format.md#锁迁移)。
Windows 卷路径问题须先裁定，不能以“旧工具不会再启动”为安全前提。

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

上述局部取代在实施批准后生效；本次不改写原 ADR 或活动源码。
数据契约、架构和 CLI 中冲突的历史兼容描述，在实施时按本 ADR 同步。
未列入矩阵删除项的命令别名、协议、指标或现行可选字段不顺带清理。

## 替代方案与风险

| 方案 | 取舍 |
| --- | --- |
| 保留全部兼容 | 继续维护旧模板、投影和迁移测试，不满足用户范围；不采用 |
| 按 v1/v2 批量删除或统一重编号 | 会删除现行 engine-fit、评分、锁状态与扩展协议；不采用 |
| 新锁完全独立或只桥接一次 | 旧工具未来可并发运行，崩溃后也可能绕过新 dirty；不采用 |
| 永久沿用旧主路径 | 安全且改动最少，但不实现命名迁移；若锁方案未过门，维持此现状并报告阻断 |
| 新主路径加有限旧路径桥 | 采用；保留很小的安全互操作面，删除旧运行/报告负担；旧状态失配会保守阻断 |

旧报告 CLI 验证能力消失是有意的破坏性变更；提示回原项目处理，不自动重建。
模板和源码删除会改变真实源码/职责摘要，不伪造旧身份或继承旧测量资格。
旧题包证明、现行低版本协议误删是主要回归风险，须有正向保留测试。
锁桥不是跨项目测量资格证明；两个工具只能串行运行，结果仍按原来源判断。

## 拒绝与验收

完整、可识别但不支持的格式返回 `unsupported_format`/退出 2，并标明格式及支持集合。
损坏 JSON、坏封存、来源/哈希不符仍为证据错误/退出 4，不伪装成普通旧版本。
缺版本不默认当前，不升级、不补评分；当前未封存完整前缀仍按原规则 partial/3。
锁阻断不得发送任何模型请求；失败分类见[契约](../contracts/inferyard-current-format.md#拒绝语义)。

验收要求：矩阵正反例、当前题包审核和哈希不漂移、旧模板不进发行包、当前安装与离线报告可用；
隔离目录真实进程证明旧/新工具互斥、崩溃 dirty 持久化、迁移中断不放行。
父会话完成 Gate A 集成回归及 Gate B 代表性离线/进程验证后才能接受；
Windows 原生未验单列，不以模拟或跨平台编译替代，不发送真实模型请求。

实施前父会话须裁定[两个锁边界问题](../contracts/inferyard-current-format.md#父会话裁定项)。
完整 DAG、责任与检查见[实施计划](../plans/inferyard-current-format.md)。

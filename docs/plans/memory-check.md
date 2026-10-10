# 应用内存诊断实施计划

提供只读 `memory-check`：按应用展示内存占用与判断依据，由 agent 和用户决定是否手动退出。
本轮只修订计划，不实施功能。首个交付单元 MC-1a 只覆盖 macOS；Linux、Windows 分别作为后续独立单元。
不提供退出/kill、自动清理、`safe_to_kill` 或保证释放量，不改变正式采样、服务生命周期和停止条件。
当前进度统一见 [backlog](../backlog.md)。以下命令和新文件均为拟实施内容。

## 交付顺序与完成条件

| 单元 | 依赖 | 交付范围 | 完成条件 |
| --- | --- | --- | --- |
| MC-1a | 无 | 公共契约、macOS 采集、归组/保护、CLI | 本文软件检查、macOS 验收、成本观察和一次源码审查有回执 |
| MC-1b | MC-1a | Linux 采集与平台文档 | Linux 原生与相关回归通过；不阻塞 MC-1a |
| MC-1c | MC-1a | Windows 采集与平台文档 | Windows 原生与相关回归通过；不依赖 MC-1b，不阻塞 MC-1a |

MC-1b/c 保留在 backlog，启动时再细化平台实现与验收，不预先建立空实现。
MC-1a 在非 macOS 上明确返回不支持平台/退出 2，不能输出成功的空清单。
各平台独立标注实现与真机覆盖，完成 macOS 不等于全平台完成。

## 执行约定与所有权

- [ ] 获实施指令后读取实际 checkout 的 AGENTS.md，记录基线 SHA、分支和现有改动；在分支交付，不覆盖他人工作。
- [ ] 用 `rtk proxy mise which python`、`rtk proxy mise which uv`、`rtk proxy mise exec -- python --version`、`rtk proxy mise exec -- uv --version` 核对工具链，保留回执。
- [ ] 主执行者串行拥有公共契约、平台适配、归组/保护、CLI、测试、生成文件和文档。先通过公共类型/语义检查，再依赖这些接口完成采集与集成。
- [ ] 一次源码审查合并检查身份、归组、保护措辞、隐私、无副作用和错误处理；记录真实审查者与结论，不要求固定模型、人数或十路任务。

MC-1a 不安排并行写者或长期调度，因为接口与集成依赖紧密。后续 MC-1b/c 可并行，
各自只写对应平台采集器和平台测试；共享注册/文档/Schema 由主执行者串行集成，越界先报告。
不赋予任何人修改用户应用、服务、host lock/state 或原始 run 的权限。
相关检查通过后创建本地英文提交；push、PR、merge、close 分别沿用仓库的显式授权规则。

## MC-1a：先固定输出与判断契约

- [ ] 新增 `contracts/schemas_memory_check.py`，在 `contracts/schemas.py` 注册诊断 kind，生成 Schema 与合法/非法样例。使用 `schema_version = 3`、`kind = memory_check.v1`，不改变已有 run/sample/device_preflight。
- [ ] 在 `application/types.py` 定义公共请求/结果边界需要的类型。快照包含时间/采集跨度、平台、主机内存/来源/缺测、扫描覆盖、应用组与未归组进程。
- [ ] 每个进程记录 PID、启动身份及来源、已核验父身份、RSS bytes/来源/缺测。采集前后身份变化时丢弃不一致值，记录原因，不合并两代 PID。
- [ ] 应用组包含本次快照内 group_id、显示名、成员身份、归属证据、已知 RSS 小计与完整性。成员缺测时总量为 null，另列已知小计；RSS 合计不等于可释放内存。
- [ ] 为三种判断状态及组内传播写合法/非法样例，每种状态都要求结构化 reason；字段缺测和判断未知是不同概念。

判断按以下优先级执行；`review` 只表示可向用户询问用途，不是关闭建议或授权。

| 状态 | 明确条件 | 输出含义 |
| --- | --- | --- |
| `protected` | 命中有证据的保护规则，或组内有 protected 成员 | 保留，并列明规则与对应进程身份 |
| `unknown` | 未命中保护，但身份/归属/必需保护检查缺失或冲突；任一成员 unknown 则整组 unknown | 无法可靠分类；请补充用途或依赖信息 |
| `review` | 本账户普通 GUI 应用；身份与应用归属已核验；所有成员必需检查完成且均无保护命中/unknown | 可询问用户是否仍在使用、有未保存工作/后台任务/连接；不证明这些情况不存在 |

`review` 的必要检查包括启动身份、所有者、可信应用归属、调用链/显式保护关系、
系统与已识别 Agent/服务/远程连接等关键依赖分类。缺少检查不能解释为“未命中”。
未归组解释器、后台服务和未知可执行文件默认 unknown，不因低 CPU、进程名或高 RSS 判为闲置。
无法保证识别任意伪装或嵌入式 Agent，必须在输出限制中明示；已知对象保护失败属于缺陷。

## MC-1a：采集、归组与保护

- [ ] 新增 `platforms/memory_macos.py` 与最小分派；使用现有 psutil 7.2.2，不加依赖、不调用正式采样器。主机读数注明实际来源，不与不同口径直接作差。
- [ ] 复用 `platforms/identity.py:process_start_ticks` 的 macOS 分派，保留原启动身份口径；适配其异常到诊断 missing reason，不将单个进程错误升级为全表崩溃。
- [ ] 参考 `engine_fit_lmstudio_files.py` 的前后身份与有界遍历模式，但不直接耦合其 LM Studio 专用规则。后续 Linux 也优先复用 `identity.process_start_ticks`，若不适用须记录具体原因。
- [ ] 新增应用层归组/保护模块。普通 GUI 应用按已解析外层 bundle 归组，采集时内部使用规范路径及文件身份区分安装副本；不按显示名合并。
- [ ] 同一 bundle 的多个实例在首版合为“整个应用”的一组，保留每个根进程身份和成员，不推断浏览器 profile 或单个窗口。不同路径的同名 app 分组；group_id 只在本次快照有效。
- [ ] helper 先依据可核验的 bundle 嵌套归属，再依据稳定父子身份；两者冲突则不合并。由 launchd 启动、既无可信 bundle 归属又无稳定应用父链的 helper 独立列为 unknown，不能根据名称猜归属。
- [ ] 组内任一 protected 成员使整组 protected。保护采集器及调用祖先、已识别 Lody/Agent、系统关键进程、远程连接等已识别关键依赖、显式声明服务及可核验的后代；不沿共同祖先把所有兄弟进程保护或合组。
- [ ] 提供重复 `--protect-pid PID`；本次读取并核验启动身份，声明不存在/不稳定时输出 partial/退出 3。声明仅绑定本次快照，不是未来操作令牌，也不证明该 PID 在调用前仍是用户原意的对象。
- [ ] 全机枚举一次，只读必要属性，不读环境变量、完整 cmdline、窗口标题和文档。可执行路径仅内部用于归组；输出限应用显示名、结构化依据和必要进程身份，跨账户对象保护或 unknown。
- [ ] 将采集截止时间、进程上限和截断计数定义为采集器参数，先通过下文成本观察确定默认值。每次调用有界，超限保存部分结果；不得静默跳过高成本对象或返回空成功。

macOS app/helper 归组规则是待验收的产品规则，上一轮按 ps 路径相加不构成正确性证明。
若有未归属 helper，已知组小计仍可展示，但声明应用归属覆盖有限，不宣称完整应用释放量。

## MC-1a：接入 CLI 与文档

- [ ] 新增 `application/memory_check.py`，更新 `cli/commands/discovery.py`、`cli/arguments.py`、`cli/request.py`、`application/dispatch.py`；业务层不依赖 CLI，帮助/版本/Schema 保持 lazy import。
- [ ] 提供 `inferyard memory-check [--protect-pid PID] [--out NEW_DIR]`，默认 CommandResult JSON，不新增等价 `--json`；不传 out 不落盘，传入仅向新目录保存 `memory-check.json`。
- [ ] 冻结覆盖计数与退出码：0 为枚举完成、必需字段可读、保护声明已核验；2 为无效参数/不支持平台；3 为权限缺测、身份变化/退出、声明无法确认或截断；4 为工具/写入故障；130 为取消。身份完整但应用归属未知仍可退出 0，决策 unknown 不等于采集失败。
- [ ] 更新设备检测与 CLI 文档、平台覆盖、新主题契约及其索引。用户自行退出后重新调用获取新快照；结果不沿用旧缓存、不宣称因果释放量，原有 preflight 继续决定运行条件。

每个新代码文件控制在约 500 行内；`telemetry.py`、正式采样器和 observer 不在改动范围。
这只是新诊断契约；若实施改变正式测量、停止或旧证据语义，应先补 ADR，不得借本计划扩大边界。

## MC-1a：验证清单

单元测试承担可重复故障和非法输入；真实 CLI 承担用户入口与原生观察，不重复规定同一故障必须跑多路。
所有回执写在仓库外新临时目录，含源码 SHA、平台/工具版本、命令、stdout/stderr、退出码和测试类型。
模拟、原生、安装证据分别标注，不用退出 0 或 Schema 通过替代内容检查。

- [ ] 新增 `tests/unit/test_memory_check*.py` 与 `tests/integration/test_memory_check_cli.py`。覆盖三态条件/优先级/原因、同名不同 bundle、多实例/helper 归组、重复成员、缺测、PID 复用与父身份变化、声明保护及跨账户情况。
- [ ] 通过真实解析入口拒绝非有限数、布尔整数、非法/未知 kind、重复 JSON key 和不一致覆盖计数；通过应用边界测试写失败/退出 4、不覆盖原件及期限/上限截断，不额外安排磁盘故障现场实验。
- [ ] 保留隐私负向测试：在自有进程环境和参数放置无害哨兵，经真实 CLI 检查 stdout、stderr、文件中未泄露；用注入禁止访问器验证未调用 environ/cmdline。用副作用哨兵检查无网络、用户进程信号或 host-state 写入，不以设计声明代替证据。
- [ ] 执行 `mise exec -- uv run --frozen pytest -q tests/unit/test_memory_check*.py tests/integration/test_memory_check_cli.py`，再跑既有 `tests/unit/test_cli_surface.py`、`test_cli_dispatch_surface.py`、`test_cli_architecture.py`、`test_device_preflight.py`、`test_device_output.py`；agent 在命令外包 `rtk proxy`。
- [ ] 执行 Ruff check/format check；读取 scripts/README.md 后运行 Schema 导出和 `--check`，检查生成 diff。仅在 catalogue 绑定源码变化时同步其生成结果。

| 场景 | 操作与证据文件 | 通过条件 |
| --- | --- | --- |
| 旧入口与新入口 | 准确 BASE_SHA/head 的帮助、device-check；head 的 memory-check，保存 regression.json | 如实记录基线无新命令，旧契约/退出语义保持，新 JSON 到达明确终态 |
| macOS 归组/保护 | 原生多 helper 应用、自有多实例/同名不同 bundle 测试进程、保护父链与 PID，保存 macos.json | 成员唯一，应用级聚合与副本分组符合契约，protected 优先，歧义不误归 review |
| 退出/刷新/取消/部分结果 | 自有短命进程自然退出前后两次调用；隔离 CLI 进程中触发权限/超时注入与 Ctrl+C，保存 boundaries.json | 无跨代合并/旧缓存；缺测 null 且退出 3；取消 130；注入不冒充原生权限证明 |
| 持久化与安装 | 新目录写入/读回/重复路径；源码外安装后调用 console entry point 的帮助、Schema 与 memory-check，保存 installed.json | 原字节不覆盖，保存与 stdout details 一致，不依赖源码目录，JSON 内容可核对 |

- [ ] 完成表中四组场景，读取回执内容；自有测试进程分配上限 32 MiB、寿命 5 秒，不做用户内存压力实验，不终止用户应用。工具缺少某种驱动能力就保留相应缺口。
- [ ] 一次源码审查覆盖身份/归组、review 不误导、保护传播、隐私/无副作用和错误终态；随结果附 review、protected、unknown、partial 四份脱敏 JSON。不要求录像、截图、双独立模型或额外人工签收。

## 成本观察与有界性

这是低频诊断，不声明提速。移除未获测量支持的 2000 进程、2 秒、128 MiB、200 ms 固定验收数字，
也不把三次抽样称为“无退化证明”。复用现有可用测量办法；确需新脚本时才新增
`scripts/benchmark_memory_check.py` 并在脚本 README 说明参数与副作用。

- [ ] 在 macOS 完成最小采集通路后先记录完整枚举的进程数、可读/缺测数、采集耗时和进程启动至退出 wall time/峰值 RSS；正常与部分结果分开，不只记录成功退出码。
- [ ] 依据观察和“不能长时卡住 agent”的目的，在主题契约冻结默认 deadline、max_processes 及选择理由；先冻结再做最终验收。未冻结前不能标 MC-1a 完成，验收后不得为掩盖超限静默放宽。
- [ ] 枚举计数达到上限且仍有记录或触及 deadline 时明确截断/退出 3；截断只证明有界终态，不计完整扫描成功。构造上限前/恰好上限/超上限与超时回归，验证同一参数同时驱动采集和验收。
- [ ] 最终本机完整扫描至少 5 次，记录所有规模、缺测、耗时、RSS、中位数与范围；启动至退出包含序列化和可选落盘。核对是否在冻结预算内及实际完成的工作，不用应用 RSS 合计替代诊断进程开销。
- [ ] 帮助/版本/Schema 的 lazy-import 回归继续证明不加载采集器。若变更触及旧入口公共执行路径或观察到成本异常，再用同输入 BASE_SHA/head 各至少 5 次交替测量并调查热点；否则不强制完整旧命令 perf 基线体系，也不宣称性能不退化。

## 后续平台接手边界

MC-1b：新增 `platforms/memory_linux.py` 及测试，使用 `/proc`，优先复用现有启动身份读取，
不为方便引入正式 Linux psutil 依赖。没有可靠 app 归属时输出进程列表/unknown，不按同名合组。
MC-1c：新增 `platforms/memory_windows.py` 及测试，使用已有 psutil，注明 working set 口径，
不能用 private bytes 冒充 RSS。两个单元都需原生证据，并更新不支持分派与平台文档。
公共契约允许记录各平台来源，但不预先承诺后续任何字段变化都无需 kind 升级。
离线机器只影响对应平台任务；不把交叉编译或模拟结果写成原生通过。

## 依据与修订边界

源码调查基线为 `d8d211f38fdfc9b5c7c0eb291e1a9cf8768f35f1`。现有路径是 discovery → dispatch →
device_preflight.inspect_device；没有全应用清单。上一轮 macOS ps/vm_stat/sysctl 只读观察没有独立原型产物，
不能用于新功能验收。独立 Go observer 的有限服务候选发现也不是现成功能。

本次依据用户对“挑刺者”审查结论的修订指令，按风险缩减原 rigor 多阶段模板：
取消十路固定数量、双独立审查、视频及整套重复性能门槛，保留必要行为与泄露检查。
因此不再将原 `check-plan.mjs` 的模板合规作为本计划的验收；不修改全局技能或校验器，
不为通过其数量/英文标题检查重新添加已删除门槛。文档验收检查本地链接、任务依赖、命令和 diff。

执行前读 [架构](../architecture.md)、[CLI](../cli-surface.md)、[设备检测](../device-check.md)、
[数据契约](../data-contract.md)、[平台状态](../platforms.md)、[macOS](../../MACOS.md)、
[贡献指南](../../CONTRIBUTING.md)、[脚本说明](../../scripts/README.md)；后续 Windows 再读 [Windows](../../WINDOWS.md)。

实施结束后将有效规则保留在主题契约、剩余任务保留 backlog；全部单元收尾后删除过期计划入口。
交付记录本地提交、实际检查和未验证范围，不自动推送、建 PR、合并或关闭任何进程。

# 应用内存诊断 plan

为用户与 agent 提供按应用归组的内存清单、保护原因和待确认事项，帮助判断是否手动退出应用。
本轮只交付计划；以下命令、文件和验收均为拟实施内容，不表示功能已存在。
首版覆盖 macOS、Linux、Windows 的只读进程快照；应用归属无法确定时保留独立进程。
一个完整交付单元 MC-1：契约 → 平台采集 → 判断与 CLI → 验收。本轮不创建 PR。
不提供退出、kill、强制清理、常驻监控、自动释放或模型请求；正式计量与停止条件不变。
当前状态统一记录在 [backlog](../backlog.md)，本文只保留未完成任务。

## How to read this

One box is one unit of work. Every box names the evidence that checks it.
Check a box only when its evidence exists. 中文任务项中的回执、原始输出或测试结果就是证据。
本文按 rigor 的 `playbooks/multi-phase-plan.md` 编排；获实施授权后采用
`playbooks/autonomous-run.md` 推进一个本地完成条件，不启动长期调度。
MC-1 只是工作单元编号。建 PR、推送和合并分别等待用户授权，合并由仓库所有者执行。
Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.
模板中的 PR/head 对应未来实际交付提交；没有真实提交、模型身份或验收产物时不填造。

## Program checklist

### Arm the program

- [ ] 收到实施指令后记录授权范围；本轮计划交付不启动产品修改。证据为用户指令。
- [ ] 读取实际 checkout 的 AGENTS.md 与执行流程，使用 `git show main:AGENTS.md` 核对本地基线；记录 main/head SHA 与已有改动，保留他人工作。
- [ ] 用 `rtk proxy mise which python`、`rtk proxy mise which uv`、`rtk proxy mise exec -- python --version`、`rtk proxy mise exec -- uv --version` 核对路径和固定版本；记录输出后才开发。
- [ ] 阅读 `how`、真实表面验证、领域建模、benchmark-checklist 与代码审查流程；实际启用并行验收时再读 swarm。记录本次采用的文件与版本。

不启用 scheduler 或 audit tick：这是一次性本地交付，没有跨日站立任务。
每完成下列一个阶段提供简短 status message；用户暂停时停止所有写入。
本轮没有派发 agent，也没有独立架构评判；本文是基于已有调查的实施计划。

### Spawn owners

- [ ] 主执行者先完成 MC-1 的契约与公共类型，运行结构/语义回归，再允许平台实现依赖它们。
- [ ] 若采用并行执行，按实际可用角色派发并记录模型身份、checkout 和文件所有权；没有并行执行时同一人串行完成，不冒充独立验收。
- [ ] 平台工作流互不写共享文件：macOS 只拥有 `platforms/memory_macos.py` 及对应测试；Linux 只拥有 `platforms/memory_linux.py` 及对应测试；Windows 只拥有 `platforms/memory_windows.py` 及对应测试。路径均相对 `src/inferyard/`，测试位于 `tests/unit/`。
- [ ] 主执行者独占契约、应用归组/保护逻辑、CLI、公共测试、生成文件与文档；其他执行者需要接口变更时先报告，不越界编辑。
- [ ] 平台结果经主执行者核对后集成，再进行完整 CLI 验收与两个只读审查方向。保存文件清单与实际结果。

共享可变状态仅为公共接口、Schema 注册/生成文件和集成文档，由主执行者串行写入。
正式 run、host lock/state、服务和用户应用没有写入所有者，因为本功能不修改它们。

### PR mechanics, for every PR

- [ ] 本地分支交付，相关检查通过后按仓库约定创建英文提交；记录 SHA 和工作树状态。
- [ ] 若用户另行授权 PR，确认仓库与用户身份、检查差异并执行授权范围内的 push/PR 操作；外部回复、merge、close 不继承授权。
- [ ] 提交前检查代码注释、依赖方向、敏感内容和新增文件大小；保存 diff 审查结论。

### Verdict and merge, for every PR

- [ ] 在准确 head 上核对 unit、十项 live 场景、perf 原始回执及两个源码 audit lane。独立身份由执行时实际宿主记录，不预设模型。
- [ ] Audit A 检查进程身份变化、归组唯一性、保护传播、unknown 降级与误导性关闭建议；Audit B 检查隐私、无副作用、超时/权限、CLI 兼容和测试真实性。各自返回文件定位与判定。
- [ ] 新 patch 重跑受影响检查并按 rigor 的 patch-id 规则判定旧证据是否有效；缺少任何必需证据时报告缺口，不报全平台完成。

### Boot recipe, for every live lane

- [ ] 每项在独立临时产物目录运行实际 CLI；记录 OS/架构、工具版本、head、命令、stdout、stderr、退出码和耗时。原始产物写到仓库外的新临时目录 `ARTIFACT_ROOT`。
- [ ] 使用 `mise exec -- uv run --frozen python -m inferyard`；agent 在命令外包 `rtk proxy`。基线用准确 `BASE_SHA` 的隔离 checkout，禁止覆盖当前工作树。
- [ ] 两个真实 CLI 表面分别为模块入口和已安装 console entry point；后者使用源码外隔离安装。无图形 UI，不伪造截图。
- [ ] 故障场景使用自有短命进程或显式标记的隔离注入；注入不计为原生证明，不终止用户应用、不启动模型服务。

## 提供只读内存清单与判断依据 (MC-1)

**Depends on.** None. 内部顺序为共享契约 → 三平台采集 → 应用判断与 CLI → 验收。

**Files.**

- [ ] 新增 `src/inferyard/contracts/schemas_memory_check.py`，在 `contracts/schemas.py` 注册新诊断 kind；生成对应 Schema，不手改 JSON。
- [ ] 在 `src/inferyard/application/types.py` 定义请求/结果边界所需类型；新增 `application/memory_check.py` 与按职责拆分的归组/保护模块，每个代码文件控制在约 500 行内。
- [ ] 新增 `src/inferyard/platforms/memory_{macos,linux,windows}.py` 及最小平台分派模块；不改 `telemetry.py`、正式采样器和 observer。
- [ ] 更新 `cli/commands/discovery.py`、`cli/arguments.py`、`cli/request.py`、`application/dispatch.py`，按需要更新 CLI 输出边界；保留 lazy import。
- [ ] 新增 `tests/unit/test_memory_check*.py`、`tests/integration/test_memory_check_cli.py`；扩充现有 CLI 注册/帮助/Schema 回归。
- [ ] 更新 `docs/device-check.md`、`docs/cli-surface.md`、`docs/platforms.md`，新增 `docs/contracts/memory-check-contract.md` 并从主题索引链接；进度只写 `docs/backlog.md`。
- [ ] 需要复用 CLI 性能测量时新增小型 `scripts/benchmark_memory_check.py`，同步 `scripts/README.md` 的参数、副作用和证据说明；不复用会启动服务的 verify 脚本。

**Build.**

- [ ] 先冻结 `schema_version = 3`、`kind = memory_check.v1` 的新诊断结构和合法/非法样例；现有 run、sample、device_preflight 不变。记录其非正式测量属性及格式支持边界。
- [ ] 定义 `snapshot`：采集时间/跨度、平台、主机内存值与来源、扫描总数/可读数/退出数/拒绝访问数/截断原因、应用组、未归组进程及限制。字段不可得为 null 加原因。
- [ ] 定义进程身份 `(pid, start_identity, identity_source)`、父身份关系、RSS bytes/来源/缺测、归组依据。前后身份变化时丢弃不一致的数值并记原因，不把两代 PID 合并。
- [ ] 定义应用组：稳定于本次快照的 group_id、显示名、成员身份、归属依据、已知 RSS 合计、完整性。部分成员缺测时只输出明确命名的已知小计，总量为 null；未知不作 0。
- [ ] 用带原因的 `protected / review / unknown` 状态表达判断，禁止 `safe_to_kill`、可释放字节承诺和“闲置”推断；保护优先于待确认，缺身份或依赖信息落入 unknown。
- [ ] macOS 使用已有 psutil 7.2.2 读取进程信息，按外层 app bundle 和可验证父子关系归组；命令行路径归组仅为上一轮调查，不直接当可靠产品算法。
- [ ] Linux 从 `/proc` 读取启动身份、父 PID、RSS 与主机内存；Windows 使用现有 psutil 依赖。不能可靠证明同一应用的进程保持独立，不按同名可执行文件把不同任务混组。
- [ ] 保护采集器与调用祖先链、已识别 Lody/Agent、系统关键进程、已声明服务及其可确认依赖。识别基于身份/路径/父子证据，名称只作线索；不宣称可识别所有 Agent，漏识别对象必须保持 unknown，不能成为默认可关闭项。
- [ ] 提供重复 `--protect-pid PID` 声明保护对象，本次读取其启动身份；不存在/身份不稳定的声明使检查不完整并给原因，不能静默忽略。模型服务由调用 agent 显式声明，不自动读取凭据或调用 Lody MCP。
- [ ] 全机轻量枚举一次，按进程数/时间预算停止并标记截断；昂贵内存细项不进入首版。单次外部调用须有截止时间，过程退出和权限不足分别记账。
- [ ] 新入口 `inferyard memory-check [--protect-pid PID] [--out NEW_DIR]` 默认 stdout 为 CommandResult JSON；不新增与默认等价的 `--json`。`--out` 只向新目录保存 `memory-check.json`，不传则不落盘。
- [ ] 不读取环境变量、完整 cmdline、窗口标题或文档内容；归组所需可执行路径只在内存中使用，输出限显示名和结构化归属依据。跨账户对象默认保护或 unknown。
- [ ] 固定退出语义：0 完成且覆盖要求满足；2 输入无效/不支持平台；3 部分采集或保护声明无法确认；4 工具/写入错误；130 取消。完整性不由 RSS 排名决定。
- [ ] 用户自行退出后再次调用同一命令，比较同来源的整机可用内存；CLI 不自动推导因果收益、不自动重跑 benchmark，原有 preflight 继续决定运行条件。

**You see.**

- [ ] Agent 可直接解析应用与进程清单，看到占用、保护原因、归属可信度和“检查未保存工作/任务/连接”等待确认项，JSON 中没有关闭命令或安全关闭承诺。
- [ ] 权限不足、进程消失、截断均可见；不会以“没读到”呈现零占用，用户能判断清单是否完整。
- [ ] 帮助、版本、device-check 和原有正式运行入口行为保持；memory-check 不发网络请求、不写 host 状态、不停止服务。

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] 契约拒绝非有限数、布尔整数、重复身份/组成员、缺测却填零、protected 无原因及旧/未知诊断 kind；使用真实解析入口检查 path/reason。
- [ ] 归组测试覆盖多 helper、同名不同实例、孤儿/退出/权限、PID 复用、父身份变化；保护测试覆盖祖先/后代、显式声明、Lody/Agent、未知解释器和跨账户对象。
- [ ] 执行 `mise exec -- uv run --frozen pytest -q tests/unit/test_memory_check*.py tests/integration/test_memory_check_cli.py`，保存回执；测试文件在 Build 中创建后才运行。
- [ ] 执行相关既有回归 `tests/unit/test_cli_surface.py`、`test_cli_dispatch_surface.py`、`test_cli_architecture.py`、`test_device_preflight.py`、`test_device_output.py`；跑 Ruff check/format check、Schema 导出及 `--check`，核对生成 diff。catalogue 只在绑定源码变化时再生成。

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `执行时记录宿主实际分配模型；本轮未派发` at the PR head, per the boot recipe.

- [ ] Lane 1. Regression lane against trunk. 在 BASE_SHA 与 head 运行帮助和 device-check，再在 head 运行 memory-check 至退出。Save `ARTIFACT_ROOT/MC-1/lane-01/regression.json`. Pass when 基线无新命令被如实记录，旧输出契约/退出码保持，新输出可解析且正常到达终态。
- [ ] Lane 2. macOS arm64 原生扫描含多 helper 的本机应用，与同窗口独立系统进程快照核对成员和量纲，不要求动态值逐字相等。Save `ARTIFACT_ROOT/MC-1/lane-02/macos.json`. Pass when 同身份每个进程仅归组一次，来源、差异和缺测可解释。
- [ ] Lane 3. Linux x64 原生运行，核对自有进程 `/proc` 身份与 RSS。Save `ARTIFACT_ROOT/MC-1/lane-03/linux.json`. Pass when 原生可读记录单位和身份正确，无法归组时保留进程；无机器则该 lane 未验证。
- [ ] Lane 4. Windows x64 原生运行，核对自有进程启动时间和 working set 来源。Save `ARTIFACT_ROOT/MC-1/lane-04/windows.json`. Pass when 身份和字节值来源一致、不混用 private bytes，无机器则该 lane 未验证。
- [ ] Lane 5. 从自有父子进程调用 CLI 并显式保护父 PID，检查调用链和可识别 Lody/Agent。Save `ARTIFACT_ROOT/MC-1/lane-05/protection.json`. Pass when 保护关系有证据且未知解释器不被当可关闭候选；只结束测试自己创建的进程。
- [ ] Lane 6. 原生短命进程在采集中自然退出；PID 复用另用明确标记的隔离注入走真实 CLI。Save `ARTIFACT_ROOT/MC-1/lane-06/identity.json`. Pass when 无崩溃/跨代合并，退出与身份变化有原因；注入不计原生复用证明。
- [ ] Lane 7. 真实 CLI 配合隔离权限/超时注入；有原生受限进程时另记录。Save `ARTIFACT_ROOT/MC-1/lane-07/partial.json`. Pass when 部分结果退出 3，missing 和 coverage 一致，受限项不是 0，截止时间能结束扫描。
- [ ] Lane 8. 在新目录持久化、读回，再使用同一路径重试。Save `ARTIFACT_ROOT/MC-1/lane-08/persistence.json`. Pass when stdout 与保存 details 语义一致，重复路径拒绝且原字节不变；磁盘故障另有隔离注入回执。
- [ ] Lane 9. 源码外安装 console entry point，执行帮助、Schema 查询与 memory-check，并用测试哨兵校验泄露和副作用。Save `ARTIFACT_ROOT/MC-1/lane-09/installed.json`. Pass when 包内资源可用、未依赖源码目录、输出无哨兵秘密、无网络/用户进程信号/host-state 写入。
- [ ] Lane 10. 两次真实 CLI 快照之间让自有内存测试进程自然退出，另在隔离扫描时发送 Ctrl+C。Save `ARTIFACT_ROOT/MC-1/lane-10/refresh-cancel.json`. Pass when 第二次不复用旧进程缓存、整机读数重新采集、不承诺等量释放；取消退出 130 且不留采集子进程。测试分配上限 32 MiB、寿命 5 秒，不做内存压力实验。

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. BASE_SHA/head 同测 `--help` 与无模型 device-check 的进程启动至退出 wall time、峰值 RSS；新命令另测启动到完整 JSON/退出的 wall time、峰值 RSS、扫描规模和完整性。不得把不同命令做提速比值。
- [ ] Probe. 先固定系统负载、解释器、输入/目录与可见进程规模；同机场景交替 baseline/head 各 5 次，保留所有样本和完整输出；不清缓存、不关闭应用。新命令另用 1000 条固定模拟进程与原生快照各 5 次，分别标注，三平台不合并统计。
- [ ] Baseline. 先记录 BASE_SHA 的实际值及源码 SHA；当前尚未测量，不填预估 baseline。记录 trunk 无 memory-check，再单列新增工作预算。
- [ ] Rule. 预设验收预算而非实测结论：既有命令中位 wall time 不超过 baseline + max(20%, 100 ms)，峰值 RSS 中位数不增加超过 16 MiB；1000 条模拟记录归组/判定不超过 200 ms；原生不超过 2000 进程时新命令每次 5 秒内到达有界终态、完整扫描中位数不超过 2 秒且进程峰值 RSS 不超过 128 MiB。任一超限失败，不能通过截断扫描伪装性能达标；更大规模须单列覆盖/预算，不外推。

**Review gate.** 用户审阅 agent 能看到的输出和判断措辞后才进入未来合并。

- [ ] operator 查看正常、protected、unknown、partial 四份脱敏真实终端回执；screenshot 不适用，因为此功能只有 CLI JSON，不要求图形控件截图。
- [ ] 提供 30–60 秒终端 video 或可回放终端录制，展示调用、读取保护理由和再次扫描；缺录制能力则标为交互评审证据缺口，不伪造媒体。
- [ ] 仅在已获 PR 授权后报告 merge-ready；等待用户对合并的明确授权，计划本身不要求现在批准执行。

**Merge.**

- [ ] 主执行者核验准确 head 的源码、十项场景、perf 与两个独立审查回执；缺平台证据时只报告已验证范围，不勾选全平台完成。
- [ ] 如实际存在 PR，完成 review triage、冲突处理与必要重验；没有 PR 则记录不适用，不创建虚构链接。
- [ ] 获单独合并授权后才由仓库所有者账户操作，核验 git log 作者；未获授权时交付本地提交并停止外部操作。

## Close the program

- [ ] 将实际范围、测试、原生与模拟证据分别记入 backlog；不把尚无证据的阶段标完成。
- [ ] 完成后把有效规则保留在主题契约，删除本计划及索引中的入口；剩余缺口留 backlog，不留过期阶段日志。
- [ ] 交付提交 SHA、实际检查结果和未验证范围；不声称释放了用户内存或取得正式测评资格。

## Appendix A. Prototype evidence

调查基线为 `d8d211f38fdfc9b5c7c0eb291e1a9cf8768f35f1`，原分支 `lody/70c878b5-339`，调查时工作树干净。
上一轮在 San3an.local/macOS arm64 运行只读 ps、vm_stat、sysctl，证明能读取 RSS 并按 app 路径初步聚合。
原始回执在本会话中，未创建可复跑原型或保存独立原型文件，因此不将它计作产品验收。
当前源码入口为 discovery → dispatch → device_preflight.inspect_device；没有全应用清单。
observer 是独立 Go 程序，snapshot 仅找有限模型服务候选，不是本功能现成实现。
三平台归组准确性、保护覆盖、权限/截止时间、实际开销和安装行为均待上述任务验证。
方案不依赖“内存占用等于可释放量”或“所有应用都可准确归组”这两个未成立假设。

## Appendix B. Alternatives rejected

- 扩充默认 device-check：会给原设备/模型检查增加全进程扫描和隐私范围。选独立 memory-check，device-check 文档链接到它即可。
- 放进 observer：可独立发行，但需要用户增加 Go 二进制入口和另一套输出契约；首版沿用已安装的 Python CLI。
- 自动退出或 force kill：用户本轮需求是协助判断，无法从 RSS 确认未保存工作；不在本次范围。
- 全平台立即使用新依赖：macOS/Windows 已有 psutil，Linux 保留 /proc；若原生实验发现必须加依赖，先记录证据与锁文件变化再调整，不偷偷使用系统 Python。

## Appendix C. Risks

- 应用识别不是安全证明。MC-1 使用证据与 unknown 降级，尤其不能把未知 node/python 分成可关闭残留。
- 进程树有竞争与重挂父进程。MC-1 校验身份、唯一成员、归组依据和采集跨度；不伪装原子快照。
- RSS 共享页、压缩与 swap 使占用和释放量不同。MC-1 不提供释放量字段；再次采集仅显示观测变化。
- 权限、离线机器、不同 Windows/macOS 版本可能阻塞原生 lane；由执行时 Lody 可读机器能力确认，不沿用历史在线状态。无远程执行授权或能力则留缺口。
- 十项 live 与独立 audit 在本轮均未执行；模板模型字段是待记录项，不是已发现的模型身份或独立审核证明。
- CLI JSON 是交互表面，人工措辞评审与终端录制安排在实施后；本轮没有录像对象。
- 不新增实际测量领域、不改变旧证据语义，因此本计划不新增 ADR；若实施扩大正式计量/停止/身份边界，先按仓库规则补 ADR 和契约，不能借诊断功能绕过。

## Appendix D. Links and reading list

执行前读 [架构](../architecture.md)、[CLI](../cli-surface.md)、[设备检测](../device-check.md)、
[数据契约](../data-contract.md)、[平台状态](../platforms.md)、[macOS](../../MACOS.md)、
[Windows](../../WINDOWS.md)、[贡献指南](../../CONTRIBUTING.md)、[脚本说明](../../scripts/README.md)。
领域建模原则改变了具体设计：用带原因的三态决策替代 safe_to_kill 布尔值；数值缺测与完整性分开。
how 的源码路径已在调查追踪；technical-writing 和 unslop 用于本计划的步骤/证据表述。
所有权与验收证据在实际执行回执中记录，当前状态只更新 backlog。
Nowledge MCP 检索 query 为“InferYard 应用内存诊断功能调研 只读候选方案”，默认 mode、无额外 filters，
返回 strict/default 范围的第 1 项 `b3e670f0-2067-473a-88fb-439992e0ae35`，score 0.99，状态 proposed。
没有可验证相同范围的图页面会话，因此未自动展示记忆图。

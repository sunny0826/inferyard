# 单 run 十一个文件 plan

将一个完整初始 single run 的证据收敛到八个文件，生成的报告仍占三个文件。
用户要求的是规划；本页不授权产品实施、真实模型请求、push、创建 PR 或 merge。
实施单元依次为 PR1「契约与全部读取方」、PR2「共享写入方切换与完整验收」。
这里的 PR1/PR2 是任务标识，不是已经创建的 GitHub PR。
默认 benchmark 仍分别运行两个题包，最终为两个 run 加一份报告，共十九个文件。
当前进度只在 [backlog](../backlog.md#运行证据文件合并) 登记；本页保留未完成任务。

## How to read this

One box is one unit of work. Every box names the evidence that checks it.
Check a box only when its evidence exists. 框内内容是未来实施任务，本轮不勾选。
执行采用 rigor 的 `playbooks/autopilot-stack.md`，并以本仓库的授权规则约束外部操作。
用户明确要求执行后才启动；每个单元通过后建立本地提交，push、PR 创建和 merge 分别等待明确授权。
merge 只能由仓库所有者账号执行，不使用 Lody GitHub App；完成后检查提交作者。

Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

文件清单、数据结构和边界见 Appendix A；备选结构见 Appendix B。
live 指实际 CLI 和浏览器表面的模拟验收，不等于真实模型或原生传感器验收。

## Program checklist

### Arm the program

- [ ] 在执行开始时记录明确执行授权、最新基线 SHA、干净 diff 和本计划版本；只有规划授权时停止在文档交付。
- [ ] 用 `git show BASE:AGENTS.md` 读取实际基线约定；重新核对 README、数据契约、格式矩阵、scripts README 和 rigor 执行流程。BASE 在执行时解析，不固定猜测 main 的 SHA。
- [ ] 核对 `mise which python`、`mise which uv` 及实际版本，使用 `mise exec -- uv run --frozen`；Agent 命令加 RTK。
- [ ] 将 audit tick 设为单元开始、子任务终态和验证回执到达时的审计；每次核对真实活动句柄与文件所有权，出现已追踪变化才发 status message。本任务不建立长期 scheduler，也不宣称已有持续唤醒。
- [ ] 执行开始时重验模型、浏览器和本地 socket 能力；缺条件的 lane 保持未通过，不把 skip 或 sandbox 阻断计为 PASS。

### Spawn owners

- [ ] PR1 由一名代码 owner 串行承担，PR2 基于已通过的 PR1 精确 head；在同一 worktree 中不并行修改共同的 evidence/contracts 文件。
- [ ] reviewer/verifier 只读独立 checkout 或使用各自隔离产物目录；若实际容量是四个会话，十个 live lane 分批执行，不同时驱动同一浏览器或主机状态。
- [ ] root 只拥有集成拓扑、计划和 backlog；代码 owner 只修改下列 Files 范围，跨界前报告文件和原因。当前文档轮只修改本计划、plans README 和 backlog。
- [ ] 本轮没有交互变更的用户 review gate；两个单元均保留仓库规定的 push、PR 创建和 merge 授权关口。

### PR mechanics, for every PR

- [ ] 保留实际主分支 SHA；PR1 从该基线分支，PR2 从 PR1 通过的精确 head 分支，所有源码变更在分支内完成。
- [ ] 分项回归通过后再跑 Ruff、相关生成物一致性和 diff 检查；不要批量运行会启动真实服务的 `scripts/verify_*`。
- [ ] 每个单元移除被替代的内部文件路径读取和死写入，检查注释与 workaround；旧布局严格只读适配器是明确兼容职责，不重新增加旧 writer。
- [ ] 有相应授权时才向实际 GitHub 仓库 push 或创建 PR；回复外部 review 同样先核对发送授权。单纯本地检查通过不自动触发这些动作。

### Verdict and merge, for every PR

- [ ] 在实际 head 上完成 unit、十个 live lane 和 perf；保留原始 stdout/stderr、退出码、文件清单及内容摘要，root 读回产物后裁定。
- [ ] 独立 audit 1 检查版本、成员哈希、来源和全部消费方；独立 audit 2 检查 send/dirty/clean、并发归属、崩溃和停止路径。两项均须引用源码与回执，不只读 owner 摘要。
- [ ] patch 变化后重验失效的 lane；只按 Shipping 的 patch-id 规则沿用未变化结果，不能将旧 head 的通过标到新 patch。
- [ ] 报告本地可评审 head 和未验证范围；未获外部授权时不 push、merge 或 close。所有者批准 merge 后核对 `git log` 作者。

### Boot recipe, for every live lane

- [ ] 创建源码外的新隔离目录，记录基线/head 与 Python 路径；固定公开合成输入和响应，保留原件。任何 git fetch、共享分支更新均遵守已有授权。
- [ ] 复用现有 integration fixtures 的外部模拟服务与测试注入的临时 HostLock 根；CLI 不获得自动启动服务的新能力，不使用操作者真实 lock/state。
- [ ] 测试 owner 单独启动并回收其拥有的 mock 服务，执行真实 `inferyard run`、`verify`、`report` 等入口，分别保存 stdout、stderr、退出码和原始证据目录。
- [ ] 浏览器 lane 用真实浏览器打开生成的 `report.html` 并断网检查；优先 `file://`，若宿主阻断则明确记录入口限制，回环 HTTP 断网验证不能冒称 file 入口通过。
- [ ] 回执写入源码外 `ARTIFACT_ROOT/PR1/lane-N/` 或 `ARTIFACT_ROOT/PR2/lane-N/`；ARTIFACT_ROOT 是执行时新建并记录的绝对路径。下文 Save 路径相对该根。

## 先迁移全部读取方并固定布局契约 (PR1)

**Depends on.** None.

**Files.**

- [ ] 在 `docs/decisions/` 新增 ADR；更新 `docs/data-contract.md`、`docs/contracts/inferyard-current-format.md` 和相关离线读取/报告引用契约。ADR 编号在执行时分配，不占用已有编号。
- [ ] 在 `src/inferyard/contracts/` 定义容器/记录 schema；在 `src/inferyard/evidence/` 增加布局识别与领域读取模块，调整 `trial_reads.py`、`ledger.py`、`run_projection.py`、`request_snapshots.py`、预算及 lab/native/cache/fixed-output/length/capability 消费模块。
- [ ] 修改 `src/inferyard/application/verification.py`、`raw_verification.py` 的产物识别；核对 runtime 的恢复/重跑准备、analysis、reporting、extensions 和 scripts 中读取 run 原件的真实消费方。独立冻结计划或 engine-fit 的同名文件不随本任务误改。
- [ ] 增补 `tests/unit/`、`tests/integration/` 的新旧布局夹具与拒绝回归，调整 `tests/packaging/` 的源码外取证；生成受影响 schemas/catalogue，记录绑定源码范围。
- [ ] 若既有 harness 无法驱动所有回执，在 `scripts/` 新增一个有界合成布局验收工具并更新 `scripts/README.md`；它只能写新隔离目录，不启动真实模型或使用真实主机锁。

**Build.**

- [ ] 先完成局部 codec 原型：按 Appendix A 无损装入公开合成原件，读回比较逻辑文档/请求/观测与成员哈希；在录有完整请求原件的合成样本上验证偏移索引与同次字节核验，保留原型 SHA 和结果。
- [ ] ADR 定案新 `evidence_layout_version=2`，新 inputs 与 manifest 一致绑定；内层既有领域文档仍为 core schema 3，新记录用独立版本声明。更新法定支持矩阵、合法/非法样例与所有入口的旧数据策略。
- [ ] 建议保留当前合法 v3 快照布局的严格只读支持，不转换、不补写、不迁移原件。只有现有明确旧布局可识别为 layout 1；缺版本不能让任意目录默认成为合法旧布局。原已拒绝格式和迁移源继续拒绝。
- [ ] 以 `FrozenInputs`、封闭类型的观测及请求原件定义 `TrialEvidence` 领域查询；布局适配仅在 evidence 边界，业务模块不接受万能虚拟旧文件名或虚构旧路径。
- [ ] 完整扫描 JSONL 并校验每条记录后，在同次读取内建立请求身份到字节范围的内存索引；不新增磁盘索引。索引读回须绑定已核验字节或检测文件变化，不能对未核验的新字节授资格。
- [ ] 迁移所有读取方、manifest 检查、产物识别和来源引用；报告引用真实容器路径、成员定位及摘要，不能把逻辑 payload 哈希冒充物理文件哈希。现行生产 writer 在 PR1 保持旧布局。
- [ ] 新旧混合权威源、错误版、缺已引用原件、重复身份/阶段、越序、中间坏行、坏哈希和封存截尾严格失败；缺测与未完成保留 null/原因及既有退出语义。
- [ ] 更新 measurement/scoring/presentation 绑定清单与生成源；保留旧运行保存的身份，不给新读取实现或新运行继承旧测量资格。

**You see.**

- [ ] 旧 v3 run 在最新 CLI 下仍能严格核验并生成新 v8 报告，原文件目录清单与所有字节摘要不变。
- [ ] 合成八文件 run 可由实际 `verify`、`report`、`rescore`、`export` 和相关比较/恢复读取入口消费；它明确标记为模拟证据，不冒充模型运行。
- [ ] PR1 期间普通真实运行写入方式未切换；不能用 codec 夹具的八文件宣称生产 run 已达目标。

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] 增加布局识别、成员绑定、观测阶段和请求索引回归；扩展现有 `test_request_snapshots.py`、`test_ledger.py`、`test_run_projection.py` 与报告/预算/native 回归，断言实际摘要与拒绝结果。
- [ ] 用 `rtk proxy mise exec -- uv run --frozen pytest -q tests/unit tests/integration` 运行软件回归；平台跳过与失败分开登记，不执行显式开启的原生场景。
- [ ] 运行 CONTRIBUTING 规定的 Ruff/format、`export_schemas.py` 和 `export_catalogue.py` 导出及 `--check`；核对版本查询和 schema 查询不加载 live backend。

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `gpt-6.1-sol (Lody worker role revision 3)` at the PR head. 这是当前已核对的配置，执行时记录真实返回的模型身份；不可用时沿用用户选择重新派发，不伪造 lane。

- [ ] Lane 1. Regression lane against trunk：同一旧布局合成 run 分别用实际基线与 head 执行 verify/report。Save `PR1/lane-1/receipts.json`。Pass when 退出语义、五终态和资格一致，来源原字节未改；仅明确列出的生成器/布局引用差异可不同。
- [ ] Lane 2. 用新布局合成样本执行 CLI verify/report。Save `PR1/lane-2/receipts.json`。Pass when 八文件证据通过、报告恰三文件、语义与既有同事实样本一致；基线不支持新布局的结果如实记录。
- [ ] Lane 3. 将重复键、bool 整数、非有限值和非法 phase 分别注入独立副本。Save `PR1/lane-3/rejections.json`。Pass when 四类均返回证据错误且没有成功报告或修改输入。
- [ ] Lane 4. 注入未知版本、新旧权威源混合、inputs/manifest 版本冲突。Save `PR1/lane-4/rejections.json`。Pass when 可识别未知格式为 2，损坏/冲突为 4，不回退另一个布局。
- [ ] Lane 5. 分别损坏冻结成员、预算阶段、请求 body/messages hash 和 manifest。Save `PR1/lane-5/rejections.json`。Pass when 四类均严格拒绝，没有用题包重建原件。
- [ ] Lane 6. 检查未封存完整前缀、末尾半行和中间坏行，以及有 started 却缺原件。Save `PR1/lane-6/rejections.json`。Pass when 仅合法完整前缀为 partial/3；坏行/缺已引用原件为 4，封存截尾也为 4。
- [ ] Lane 7. 检查 prepare 未发送记录与普通/流式 probe 的独立观测。Save `PR1/lane-7/projection.json`。Pass when prepared 不进入 executed，两个探测槽及环境 start/end 保留，最终身份投影符合冻结契约。
- [ ] Lane 8. 实际驱动 lab/native、strict fixed output 和 cache 的离线读取入口。Save `PR1/lane-8/receipts.json`。Pass when 来源路径与内容绑定真实存在，关键缺测/坏 receipt 的阻断没有放松。
- [ ] Lane 9. 用新旧布局驱动恢复/重跑输入检查、rescore/export/public 来源链。Save `PR1/lane-9/receipts.json`。Pass when 哈希和父链拒绝一致，只写新目录，不产生布局迁移或隐式重评分。
- [ ] Lane 10. 浏览器打开新旧样本生成的 v8 HTML，断网检查文字、SVG、筛选与无 JS 回退。Save `PR1/lane-10/receipts.json`。Pass when 报告显示完整、CSP 保留、控制台无错误；回执关联截图和实际 `verify --rerender` 输出。

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. 两侧相同旧布局合成输入上的 `read_trial`、报告写出和 verify wall time，完整路径进程峰值 RSS；另量化新布局索引的扫描次数、内存及完整读取耗时，不拿不同布局直接比较文件哈希。
- [ ] Probe. 扩展并复用 `scripts/benchmark_cost.py` 的固定公开输入和交错测量规则；先 profiling 定位，正式测量不带 profiler；每侧至少五次，保存工作量、内容校验、失败计数、次数、中位数和范围。
- [ ] Baseline. 先记录实际基线旧布局结果；新布局在基线不存在时明确记不支持，新增读取工作与终态单独记数值，不虚构基线支持。
- [ ] Rule. 同一旧布局/同一工作量的 head wall time 与 RSS 中位数均不得超过基线的 1.20 倍；超限调查噪声和真实工作差异后重新裁决，不能静默调阈值。新原件索引每次归约恰扫描一次、不得按请求重扫，长样本 body 内存不得随全文总量常驻；无正确结果或完整错误计数即 FAIL。20% 是建议回归预算，不是已经测出的性能收益。

**Review gate.** None. PR1 is not review-gated. 页面与交互未改；本地检查后的外部操作授权仍适用。

**Merge.**

- [ ] root 复核 PR1 精确 head 的完整回执和两个源码 audit；旧生产 writer 与全部读取方仍可共同工作，再将 PR2 基于该 head。
- [ ] 相关检查通过后创建本地英文提交，保留检查与未验证范围；未获明确授权时停在本地，不创建 PR 或 merge。

## 切换全部共享写入方并达到八加三 (PR2)

**Depends on.** PR1 已通过的精确 head；不依赖未经授权的 GitHub merge。

**Files.**

- [ ] 修改 `src/inferyard/evidence/storage.py`、`journal.py` 及 PR1 的容器/原件写入模块；删除旧布局生产写入分支，保留严格只读适配器。
- [ ] 修改 `src/inferyard/runtime/runner.py`、`request_execution.py`、`trial_runner.py`、`trial_lifecycle.py`、`trial_finalization.py`、`service_observation.py` 及真实旁证写入方；collector/observer 与高频扩展日志按契约保留来源。
- [ ] 增补 storage、request lifecycle、duration send admission、fixed output、cache、batch/native 及取消/dirty/排空的软件回归；更新合成验收 harness 和 packaging 场景。
- [ ] 更新相关数据/主题契约、实施后的架构与 usage、plans README、backlog、README 两种语言和 CHANGELOG；绑定源码改变时重新生成 catalogue/schema。社区数据不在本轮所有权内。

**Build.**

- [ ] `TrialJournal` 先原子发布 inputs，再创建日志；每条观测或原件由其唯一拥有者同步写入。不得让多个线程/协程分别写同一 stream，保留现有采样日志的真实归属。
- [ ] 将六份冻结/元数据纳入 inputs、七类普通 single 观测纳入 observations；将 batch/native 的选用低频旁证映射为已声明类型，不删除 collector、safety、预算或 clean receipt。
- [ ] 替换 `TrialRequests.one()` 的唯一请求快照写入点，覆盖 single、batch fixed 和 duration；准备原件脱敏、校验、append、flush/fsync 完成后才可进入 dirty、started/send。
- [ ] 保留 prepared 未发送记录，尤其 duration 的 send admission 期限拒发；用事件绑定证明实际开始，不能用 prepare 行数改 executed 或分母。
- [ ] 保持原快照边界的耐久要求、native idle/clean receipt 先落盘后清 dirty 的顺序、目录 fsync 和最终封存；写入/flush/fsync 失败立即停发，保留已存在的 dirty，不自动重试或恢复。
- [ ] inputs 不重写，observations 只追加；普通/流式 effective 各占明确槽，最终身份遵循现有构造顺序，不简单取最后一条。当前 snapshot 本来禁止覆盖，不能把内存字段变化误说成磁盘覆盖。
- [ ] 停止、取消、排空、安全阈值、身份和资产核验、五终态、评分分母都不变；旧 run 和报告的字节及封存不改，不增加自动迁移或双写。
- [ ] 实际计数普通完整 single 的八证据与三报告，默认双 scope 的十六证据与三报告；列出 batch 外层、父链和选用扩展流的附加文件，不把十九误称所有实验的总数。

**You see.**

- [ ] 新普通完整 single Prism run 目录恰有 Appendix A 的八份文件；生成独立新报告恰有三份，合计十一，逐请求数增加不再增加物理原件文件数。
- [ ] 默认完整测评仍有 `zh-core` 与 `zh-svg-pelican` 两个 run；封存后生成新汇总报告，共十九文件，两个题包的质量分母不合并。
- [ ] 原始旧 run 仍可核验，新的 verify/report/rescore/export/恢复读取链无需伪造旧文件路径；dirty、停止和拒绝理由可在新证据中追溯。

**Verify, unit.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] 对 request prepare、观测耐久、dirty、started、发送、clean receipt、close、seal 各边界注入失败；断言 mock 服务收到的实际请求、锁状态、日志可读前缀和退出语义，不能只断言函数调用次数。
- [ ] 运行相关 unit/integration 回归后执行 `rtk proxy mise exec -- uv run --frozen pytest -q`；记录各平台/显式原生跳过，禁止通过开启真实设备 workload 来补齐软件 PASS。
- [ ] 运行 Ruff/format、相关 schema/catalogue 导出与一致性、文档链接/命令/diff 检查；源码外安装检查使用合成输入，构建与安装按另行覆盖的预算执行，不继承旧发行物资格。

**Verify, live.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked. Ten lanes on `gpt-6.1-sol (Lody worker role revision 3)` at the PR head. 按实际容量分批；每个 lane 保存其独立原件，不与另一 lane 共用 mutable HostLock 或输出目录。

- [ ] Lane 1. Regression lane against trunk：同配方普通 single 合成请求在基线与 head 实际运行到 seal、verify、report。Save `PR2/lane-1/receipts.json`。Pass when 正式计数/请求内容/五终态一致，head 八加三文件，旧布局的基线清单与 SHA 如实保存。
- [ ] Lane 2. 固定 `zh-core` 120 题与鹈鹕 1 题、各三次 warmup，实际驱动两个 run 和新汇总报告。Save `PR2/lane-2/receipts.json`。Pass when 两 run 逐请求数为 125/6、八文件各自封存、报告三文件，总十九，核心分母 120 且 SVG 不计质量分。
- [ ] Lane 3. 执行 batch fixed/repeat 的模拟入口，并检查其独立外层控制与来源链。Save `PR2/lane-3/receipts.json`。Pass when 所有请求使用新原件日志，串行准入与冻结次数不变，附加文件清单全部解释，不套 single 总数。
- [ ] Lane 4. 执行 duration 窗口，分别注入 send 前期限到达和执行中取消。Save `PR2/lane-4/receipts.json`。Pass when prepared 未发送不进入 executed，窗口/排空/终态保持冻结语义，没有自动重试。
- [ ] Lane 5. 在请求原件 append/flush/fsync 失败时执行模拟 CLI。Save `PR2/lane-5/failures.json`。Pass when 服务未收到相应请求，退出失败且不伪造 started/成功封存；已有 dirty 保留。
- [ ] Lane 6. started 后崩溃、采样失败、close/seal 失败分别检查。Save `PR2/lane-6/failures.json`。Pass when 停发、dirty、完整前缀和 partial/error 符合契约，坏 seal 不回退 partial，不补终态。
- [ ] Lane 7. native/KVMem/NInfer 模拟取消、空闲、clean receipt 和 provenance 绑定。Save `PR2/lane-7/receipts.json`。Pass when clean 只发生在合法 receipt 耐久之后，client_http 不升级为 native 资格，缺关键旁证仍阻断。
- [ ] Lane 8. strict fixed output、cache 与 input budget 的共享 writer 场景。Save `PR2/lane-8/receipts.json`。Pass when 两类 probe 原件、token phase/case、预算失败和错误回答的既有语义保留。
- [ ] Lane 9. 旧新父链上的 resume/rerun/rescore/export/public 及来源搬移。Save `PR2/lane-9/receipts.json`。Pass when 当前 CLI 完整消费新布局，原件不变，不隐式迁移，身份/hash/路径穿越拒绝全部有效。
- [ ] Lane 10. 在断网浏览器打开 lane 1/2 的实际报告，执行 verify 与 rerender。Save `PR2/lane-10/receipts.json`。Pass when 三文件封存、文字/SVG/筛选/无 JS 回退正常，真实来源定位可核验；保存桌面/手机截图及原始 CLI 输出。

**Verify, perf.** Tests alone are not sufficient verification. A PR is verified only when its unit, live, and perf boxes are all checked.

- [ ] Metric. 基线与 head 的等工作量合成 run 从准备到 seal 的 wall time，另量 verify/report wall time、进程峰值 RSS、文件数和落盘字节；包含采样、验证、写盘、flush/fsync 和封存。
- [ ] Probe. 用相同公开输入、mock 响应、请求次数和线程设置交错运行，每侧至少五次；固定 mock 耗时后记录计时边界、噪声、计数、原件内容、失败数和全部原始数据。profiling 独立于正式测量。
- [ ] Baseline. 先保存实际基线清单：普通 single 固定 18 加 N+W+2 请求快照；默认双 scope 且 W=3 时共 170 文件。数值由本次 harness 重算，不能直接把上轮推导当本轮实测。
- [ ] Rule. 普通 single 必须恰 8+3=11，默认双 scope 恰 8×2+3=19；每种实际 workload 的计数、内容和拒绝行为完整。等工作量 wall time/RSS 中位数不得超过基线 1.20 倍；超限先核验噪声与漏工作，无法解释则 FAIL。落盘字节不设未经测量的节省比例，不承诺速度提升。

**Review gate.** None. PR2 is not review-gated. 只改变保存布局和对应来源引用，保持页面功能；实际离线渲染仍是必需验证。

**Merge.**

- [ ] root 读回最终新旧证据、十九文件清单、故障回执和独立 audit；确认没有遗漏生产 writer/消费者或生成物，再创建本地英文提交。
- [ ] 有各自明确授权时才 push、创建 PR 或由所有者 merge；之后核对 commit 作者与本地/远端精确 head。

## Close the program

- [ ] 实施任务全部有真实回执且已勾选，源码和生成物一致，原件保留；删除已完成计划前将长期规则归入契约与 ADR、剩余工作归入 backlog，不建立历史计划归档。
- [ ] 分别报告软件实现、模拟 CLI/离线 HTML、原生设备和性能证据；未授权或未执行的真实模型、Linux/Windows 原生耐久/采样、长时负载、发行验收保持未验证，不继承历史资格。

## Appendix A. Prototype evidence

规划基线是 `d8d211f38fdfc9b5c7c0eb291e1a9cf8768f35f1`，开始时工作树干净。
当前实现的文件数量已由源码核对；此前独立审查曾报告 mock single 为 26=18+8 文件、报告三文件。
那次模拟不是本计划的新布局原型，也不是主会话重新运行的实测。
本轮只做结构与源码核对；新容器、偏移索引、崩溃行为和十一文件 CLI 尚未实现验证，PR1/PR2 的任务保留这些证明。

### 文件目标与计数范围

| 目录 | 文件 | 内容 |
| --- | --- | --- |
| run | `inputs.json` | run、plan、selection、冻结 config、bundle 与配置原文，共六份逻辑输入 |
| run | `observations.jsonl` | identity、service props、普通/流式 effective、environment start/end、token budgets；选用低频旁证保留明确类型 |
| run | `request-bodies.jsonl` | 所有 probe/warmup/formal 的完整脱敏 prepared body 与身份/hash |
| run | `events.jsonl` | 当前事件、回答、终态与评分；不把 prompt 全文再复制进去 |
| run | `memory.jsonl` | 当前内存/资源样本与来源 |
| run | `environment.jsonl` | 当前周期环境观测 |
| run | `schedule.jsonl` | 当前调度观测 |
| run | `manifest.json` | 布局、身份与上述物理成员的字节哈希/大小；不哈希自身 |
| report | `index.json` | 当前 v8 结构化报告与内容引用 |
| report | `report.html` | 当前自包含离线 HTML |
| report | `artifact-manifest.json` | 当前报告成员的呈现字节封存 |

十一只适用于一个完整初始普通 single Prism run 加一份普通报告，无恢复父链、自定义采集或显式比较产物。
这不是把默认 benchmark 改为单题包；默认两题包生成两份 run 和一份报告，合计十九。
候选/绑定/准备、HostLock/state、batch 外层控制、原始父链、engine-fit、扩展高频流和显式 comparison 另计。
不以删必需旁证、改变停止/采样或将来源藏到目录之外达成数量目标。

### 领域结构与来源绑定

- `FrozenInputs` 的各逻辑文档保留既有 schema、身份及按现行 `json_bytes` 计算的成员摘要。
  配置原文保留现有读取/脱敏后实际持久化的字节，带媒体类型、明确字节编码与摘要；不能由 parsed config 重新格式化冒充原件。
- `Observation` 是封闭 kind/stage 联合；新增 envelope 明确 run/trial 身份、局部 seq、clock 和记录时间。
  记录时间不冒充实际传感器读取时间，payload 的原始读取区间与单位不变。
  每种类型定义合法阶段、出现次数、是否同步耐久与最终投影；旧快照没有的时间信息保持未知，不补造。
- `PreparedRequest` 保存 request_id、run_id、ordinal、phase、完整 body 与绑定哈希；没有 started 的 prepared 是合法准备原件。
  新布局中 started 必须绑定已经耐久的唯一原件；缺少原件属于证据错误。
- `TrialEvidence` 是同次严格读取的领域视图；代码通过 `inputs`、`request_body(request_id)`、指定阶段的观测查询消费。
  字节核验、逻辑内容哈希、来源链核验和最终结果投影分别保留，不以 manifest 中的容器存在替代必要记录存在。
- 当前 `snapshot` 禁止覆盖已有文件；两次 probe 更新的是内存身份里的 effective 字段。
  新日志要保存各自阶段并按既有语义形成最终视图，不添加全局跨日志顺序或通用 last-write-wins。
- 未封存的末尾半行仅保留完整前缀并标 partial；若 started 引用的原件不能完整读取，则严格报错。
  已封存截尾、中间坏行、重复身份、错 hash 均拒绝；解析不改写、不截断、不修复输入。

### 观测状态和旁证清单

下表是 PR1 要固定的状态约束草案；成功 run 的必需项与停止 run 的缺失原因必须分别验收。
不把 identity 的最终投影误当为必须在运行结束后新增一份身份观测，发布时点沿用当前 writer。

| 观测 | 完整普通 single | 停止/选用条件 |
| --- | --- | --- |
| environment start/end | 各一份，保持完整读取区间与来源 | 准备或收尾失败可缺对应记录，保留原因；不拿 start 填 end |
| identity | 各发布路径的一份权威快照，保留有效参数投影 | single 通常 probe 后发布，batch/停止由现有收尾路径发布；未发布不补造 |
| service properties | 一份当前实际观测 | native 的 pre-idle 与 lab 的 post-idle 阶段保持不同；不重查替代保存记录 |
| effective ordinary/stream | 两个独立槽 | 任一 probe 失败可缺其后续观测，保留失败与未完成语义 |
| token budgets | 明确 phase/case 的 v2 集合 | 按当次选中输入产生；预算准备失败不补 0 或未发送记录 |
| engine capabilities/native clean receipt | 适配器启用时保留 | 按请求身份和 dirty token 绑定；成功 HTTP 不推定 engine-wide drain |
| collector/observer/service binding | 相应采集或 batch 启用时保留 | 来源、scale、声明与实际输出全部核验，不能由容器存在代替记录存在 |
| safety policy/check、execution budget、recovery、service reuse、input target check | 原功能写入时保留 | 有明确类型、阶段与原始 payload；不删除停止阈值、接手或父链条件 |

PR1 清单核对所有 `store.snapshot`、`text_snapshot` 与 `observation` 写入者；上表不能作为遗漏项可删除的授权。
`external-cpu.jsonl`、`request-environment.jsonl`、`safety-checks.jsonl` 及 extensions 自有高频日志先保持独立，计为选用附加文件。
外围 collector/observer 输出不能只因 run 内保存声明就删除；独立工具仍拥有其来源原件。

原件索引优先保留 byte range 与单次读取的完整性凭据，不常驻全部 body；offset 再读取必须检验同次字节一致。
offset 元数据仍是 O(N)，不能宣称内存为常数。PR1 原型以固定 120、12000 条公开合成 body 校验读取、峰值 RSS 与文件变化检测；
较大输入在相同工作量下采用上述 1.20 倍 RSS 回归预算。预算不满足时比较单遍归并或有限缓存后固定签名，
不新增业务请求数上限、不缩短冻结 duration 窗口，也不把一个有限样本的通过写成无界负载保证。
扩大压力规模超出合成 harness 的既定磁盘/时限预算时记录未验证范围，不无限加量或发送真实模型请求。
索引没有磁盘文件、压缩、轮转或通用恢复工具；若规模迫使改变结构，回到契约设计，不增加绕过分支。

## Appendix B. Alternatives rejected

两个独立原生会话继承主会话配置、只读产出候选 A/B，未改文件，未声称跨模型评审。
两者收敛到领域视图加 JSON/JSONL 容器；没有得到两个结构不同的主候选，不将这一结果包装为结构多样性证明。
独立评判会话在两份候选完成后读取完整原文与关键源码，建议以 B 为基础，吸收 A 的同次已验证字节约束。
主会话核对 `TrialRequests.one`、`service_observation`、`TrialReads` 和 CLI 识别路径后采纳；没有用同模型会话数量提高事实置信度。
选择 B 的领域调用方式和真实容器来源绑定，保留 A 的局部日志顺序与快照非覆盖事实。
两份候选的「先切 writer，再迁下游」均不采用：消费者迁移并入 PR1，使 PR2 每次切换都能被当时的读取链消费。

| 备选 | 不采用的理由 |
| --- | --- |
| 对业务暴露虚拟旧文件名 | 现有 path.exists/manifest membership 参与来源资格，虚拟旧路径混淆逻辑记录与物理封存；领域读取接口一次迁移后收口 |
| SQLite 容纳全部证据 | WAL/SHM、checkpoint 与事务耐久会重新定义文件数和跨平台行为，超出本次 JSON/JSONL 合并任务 |
| 一份全局日志装下全部观测和样本 | 合并不同采样写者、时钟与 flush 边界，需新的并发/恢复协议；保留当前四份时间序列日志 |
| 跨 run 共享输入 | 增加外部依赖、封存和可搬运性复杂度；每个 run 继续自包含自己的冻结输入 |
| 报告三文件再合并为二 | 用户明确选十一文件，本轮报告布局保持 v8 三文件；不纳入实施 |
| 直接删除请求原件或封存 | 破坏实际请求核验或完整性，不是可接受的减文件方式 |

## Appendix C. Risks

| 风险 | 单元与处理 |
| --- | --- |
| 旧证据因新布局失去 verify | PR1 前置定案严格只读支持与识别规则；未知/冲突不猜版本，不改原件 |
| 逻辑哈希与物理摘要混用 | PR1 为 inputs 成员与 JSONL 记录定义真实定位/内容绑定，所有来源引用迁移后再切 writer |
| send 前 fsync、native clean 顺序漂移 | PR2 覆盖准备未发送、flush 失败、dirty 后崩溃、receipt 失败和排空；以服务实际收件与锁状态作证据 |
| 稀有 consumer 继续硬编码旧路径 | PR1 检查 application、evidence、runtime 恢复、report/export/public、extensions、scripts 与 packaging；同名独立产物不误迁 |
| 长日志导致 O(N²) 或 body 常驻 | PR1 一次扫描索引与同次字节核验原型，PR2 长合成样本 RSS 回归；不把 120 题结果外推到无界窗口 |
| 十一数量被误当所有 run 的保证 | PR2 明确完整普通 single 口径，附加流、batch、父链与显式比较单独清点 |
| 软件通过被写成真机/性能通过 | 两单元均分开登记实现、模拟、浏览器与原生证据；真实设备和模型需独立设备/预算/停止条件授权 |

缺少的新布局驱动和长期压力证据均在任务中明确建设；现有浏览器与本地 TCP 能力执行前重验。
本计划不要求另一个操作者签收，不要求补齐所有历史实验；受影响原生文件持久化能力必须在平台验证中单独记录。
没有原生证据时可以交付软件实现与缺口，不能宣称全部平台验收完成。

## Appendix D. Links and reading list

- [架构](../architecture.md)、[数据契约](../data-contract.md)、[当前格式支持矩阵](../contracts/inferyard-current-format.md)、[离线读取](../contracts/offline-reading-contract.md)、[报告契约](../contracts/report-contract.md)。
- [默认测评范围](../usage.md#默认测评范围)、[持续发送约束](../contracts/duration-send-contract.md)、[实验设计](../experiments/design.md)、[血缘规则](../data-contract.md#证据血缘与比较结论)。
- [ADR 038](../decisions/038-inferyard-current-format.md)、[ADR 044](../decisions/044-prompt-content-hash.md)、[ADR 045](../decisions/045-report-content-refs.md)。
- [贡献指南](../../CONTRIBUTING.md#changes-and-verification)、[scripts README](../../scripts/README.md#软件成本对比)、[plans 索引](README.md)。
- PR1/PR2 实施前用 how 追踪真实入口与状态归属；audit 执行前读取 rigor code-review/no-comments，具体跨模型审核按实际可用配置与授权执行。
  判断轨迹与回执保存在源码外验收目录，backlog 记录当前进度；不往仓库提交会话交接日志或真实证据。

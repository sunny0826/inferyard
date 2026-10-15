# Backlog

更新于 2026-10-15。当前进度集中在本页；证据解释见[血缘规则](data-contract.md#证据血缘与比较结论)。

## InferYard Studio 后续工作

- 遗留引擎停止的 PID 复用 TOCTOU 窗口可收紧（Windows OpenProcess 句柄钉住 / Linux pidfd）；
  当前由停止全程 25ms 身份重核收窄，本轮不实施。

- 2026-10-14：[Studio Design System](../../studio/docs/design-system.md) 已用于六页、详情和报告窗口外壳。
  DS-01 至 DS-09 均有实现与软件回归，全量 Go 测试、vet 和三平台编译通过；
  独立审查及修复增量复核均无阻塞发现，六条表单路径的重复提交公告缺口已修复并通过回归。
  实际覆盖见[全页面审查](../../studio/docs/design-system-audit.md)。
- 报告打开失败后，再次打开同一失败报告仍不会重新公告相同错误；可见错误保留。
  后续补此路径的重复操作公告与键盘回归（本轮六条表单修复不覆盖报告打开）。
- 按规范补三平台原生窗口、键盘、系统偏好、辅助技术及离线报告验收；
  252 个内存布局场景和 PNG 目检不替代真窗口证据。

- Studio CI 已有 `studio-check.yml`，macOS/Windows 执行 vet 与模拟引擎边界 e2e；
  单测暂仅在 macOS 执行，Windows 单测的路径、权限位和时序假设仍待清理。
  原生窗口、系统通知及正式分发签名/公证仍需分别验收。
- macOS 系统通知权限：T10 验收唯一未通过项为通知实际到达；系统拦截通知权限弹窗，
  非产品缺陷。随仓库迁移继续挂账，待系统设置授权或后续签名后复验。

## 新功能：应用内存诊断

2026-10-11，issue #25 Windows 能力基于 `597367d` 实施。原基线 Windows 的退出 2 是
预期平台限制。新增独立 `memory_check.windows.v1`，保留 macOS v1 字段与读取支持。
Windows 清单分开报告 working set/private commit、TokenUser 账户关系/session 缺测；
EXE 文件/账户/session 分组仅作观察，protected/unknown，不给出终止建议。
首次原生公开 CLI 枚举 246 PID；最终源码再次枚举 250 PID，无截断，186 项必需检查缺测
（150 项所有者/session 不可得，148 项 working set/private commit 缺测，38 项父关系缺测，
这些计数有重叠），退出 3；69 protected、181 unknown、0 review，采集约 0.518 秒。
显式保护已核验，保存读回相等，重复目录退出 4 且原件哈希不变，无效 PID 退出 2。
相关软件回归 356 passed / 3 skipped；跳过两项 POSIX 管道测试及 macOS 原生诊断。
超时测试复现 Windows venv 启动器 PID 与实际解释器 PID 不一致，改为直接持有基础解释器
进程句柄并继承模块路径；实际 worker PID/自有进程退出断言通过，不再只回收启动器。
macOS 归组/旧格式读回/拒绝路径模拟通过；Windows TokenUser/session/FILETIME 本机读取通过。
Ruff check/format、Schema/catalogue 一致性通过。中文 Windows 测试使用 `PYTHONUTF8=1`，
避免旧共享夹具默认 GBK 解码；不改其业务源码。
这不是历史 fallback 的复用，也不构成 benchmark/安装包或 macOS 原生资格。
原始 JSON、退出码/耗时、版本和软件回归日志保存在本机独立临时目录，交付报告提供定位。
多账户、实际 PID 复用、原生交互取消与源码外安装等未验证项见[剩余计划](plans/memory-check.md)。

2026-10-11，PR #19 Bugbot 的父身份查询失败问题已复现并修复：父关系缺测或变化仅清空父边，
保留核验过的自身身份/RSS；受影响对象 unknown，不降低无关可读应用的 review。
新增可选 `parent_identity_missing_reason`，旧 v1 文件仍可读取；缺测计数包含父关系失败，仍退出 3。
注入父查询失败（前/后）与父代际变化的 3 项回归先失败、修后通过；相关软件回归 269 项通过，
覆盖保存读回/退出 3、坏父边及缺测计数拒绝、旧样例读取；Ruff、Schema/catalogue 一致性通过。
真实 macOS CLI 枚举 645 PID，无截断，207 项缺测，退出 3，保存读回一致；
该次未出现父关系缺测，竞态修复由上述注入回归验证，未声称真机复现了竞态。
原始失败/通过日志在 `/tmp/memory-parent-before.log` / `/tmp/memory-parent-final-tests.log`；
原生回执位于仓库外 `/var/folders/7r/gjhs3r216gvgl1yq_pr2nvxh0000gn/T/inferyard-parent-review-xe2pilyx/`。

2026-10-10，MC-1a 已在 `feat/memory-check-macos` 实施，基线 `515c599`。
`inferyard memory-check [--protect-pid PID] [--out NEW_DIR]` 默认输出只读 JSON，
按可信 bundle/稳定父身份聚合应用，输出 protected/unknown/review 及原因；
不自动退出应用、不读取完整命令行或环境、不提供 safe_to_kill 或释放量保证。
规则见[诊断契约](contracts/memory-check-contract.md)；Linux MC-1b 与 Windows 剩余原生验证
继续见[后续计划](plans/memory-check.md)，Linux 仍返回不支持平台/退出 2。

采集冻结为 5 秒 deadline、4096 PID 上限，在独立模块子进程读取，超时/取消只回收自有采集器。
最终 macOS 五次扫描均观察全部 624–627 PID，其中 223–226 项缺测，退出 3；无截断，
不将权限缺测计为完整成功。采集耗时中位数 165.20 ms（153.40–170.07），
启动至退出 wall 中位数 232.62 ms（215.51–248.14），`time -l` 进程峰值 RSS
中位数 34.20 MiB（34.09–34.34）；该指标不是父子同时驻留总内存，不宣称提速或全机上界。
本机观察支持预算选择，不代表其他机器或未来平台成本。

原生自有应用场景验证了多实例/helper（4 个成员）、同名副本（另 2 个成员）、显式保护、
保存读回、重复目录拒绝及自然退出后刷新。进程上限截断原生退出 3；
超时/进程组 Ctrl+C 为注入场景，取消 130 且自有采集器已回收。
最新版 wheel 在源码外固定依赖环境通过帮助、Schema 和真实清单/保存检查。
基线/候选 device-check 均退出 0、kind 保持 device_preflight.v1；帮助各 5 次交替观察，
不据此宣称性能不退化。合法样例包含三态，非法样例经过离线读取边界拒绝。

本轮全量软件回归 **4796 passed / 53 skipped**；53 项为平台或显式启用限制，未计为通过。
旧 CLI 定向回归 240 项、最后离线拒绝路径修复后新诊断定向回归 60 项通过；Ruff check/format、Schema/catalogue
一致性及本地链接/锚点检查通过。Lody“挑刺者”源码审查 approve；按其建议收窄系统路径，修复 /usr/local 自装工具误保护，
先失败复现、修后通过。helper 显示名建议未复现（始终取已分配 bundle 名）；进程组取消已有
仓库外真实注入回执，尚未固化为信号回归。后续完整回执确认另一项为理论窗口：
SIGINT 落在 Popen 返回与进入 try 之间时，自有采集器可能短暂成为孤儿；
该创建窗口未专项验证，不将其写成通过。所有本机原件位于仓库外
`/tmp/inferyard-memory-mc1a-kg_o9862/`（`accepted/` 最终原生场景与最新 wheel/安装，
`final/` 回归与取消/期限边界）；临时目录不是发行或长期证据仓库。
没有启动模型服务、发送模型请求、关闭用户应用或改写旧 run。
2026-10-11 用户授权提交 PR：同步 main 后保留双方 CLI/请求类型与计划索引，
相关内存/CLI/模型获取回归 362 项通过，Ruff、格式及 Schema/catalogue 一致性通过。

## InferYard 首次公开准备

项目名为 **InferYard**，仓库名、Python 包名及 CLI 均为 `inferyard`，版本 `0.0.1`，许可证 MIT。
源码复制、包和 CLI 更名、当前格式清理已完成；新 Git 历史不导入源仓库历史、原始运行、模型或真实配置。
公开源码仓库为 [sunny0826/inferyard](https://github.com/sunny0826/inferyard)，默认分支为 `main`。
`v0.0.1` 的公开状态、最终源码提交、候选清单摘要和手动 package-check 来源，以
[GitHub Release](https://github.com/sunny0826/inferyard/releases) 说明及对应候选清单为准。
[PyPI 0.0.1](https://pypi.org/project/inferyard/0.0.1/) 已发布，wheel SHA-256 为
`f390507672ff60b191550e14c0da9d687a0448b607a5e1d789787c5cdf8624b8`。
后续候选仍须从集成后的干净提交手动生成，不能把 PR 运行改标为发布来源。

### v0.0.2 发布记录

2026-10-12 发布 `v0.0.2`。版本冻结 PR #37（源提交 `d3761bb`，冻结版本校验、publish workflow
tag 与测试 fixture 同步升级）。本地候选五步在干净 worktree 完成（macOS arm64
`installed_safe_checks.v3` passed，26 命令）；手动 package-check run
`38215582766` 三平台安装检查全绿并生成候选 manifest
（SHA-256 `eeebd6b2…3067`，wheel `bf05cf35…` 与本地构建字节一致）。publish workflow
`38216763800` verify-only 演练通过后，`38217556374` 以 `destination=both` 发布：PyPI
`0.0.2`（wheel+sdist+attestations）与 GitHub Release `v0.0.2`（五附件经 SHA256SUMS
本机复验）。发布后 README/README.zh-CN/installation/platforms 更新版本引用。
安装检查不构成原生模型准备或严格性能资格。

### PyPI uvx 真机验证

2026-10-08 从 PyPI 使用固定 `inferyard==0.0.1`、Python 3.14.7、uv 0.12.18 验证：

| 平台 | 本次结果 |
| --- | --- |
| macOS arm64 / Qwen3-4B / Prism Metal | 新核心运行 120/120 completed，质量通过 94/120；新鹈鹕运行 1/1 completed；两者其余四终态均为 0，原始证据及双运行报告 `verify --rerender` 通过 |
| Omarchy / Linux x64 / Qwen3-4B / Prism CPU | 新核心运行 120/120 completed，质量通过 96/120；新鹈鹕运行 1/1 completed；两者其余四终态均为 0，原始证据及双运行报告重渲染核验通过 |

本机已验证 `uvx` 的版本、帮助、两个包内题包导出及缓存齐全后的离线启动。
汇总报告经本机回环 HTTP 载入后切断浏览器网络，运行筛选与内嵌 SVG 渲染正常，控制台无错误；
浏览器工具拒绝直接访问 `file://`，因此未验证该入口。
设备推荐因可用内存不足阻断后，用户明确授权忽略内存和温度限制；本次采用手工候选，
包内单次执行的 1 GiB 内存下限仍生效，未设置实验温度停止项。
这不证明自动候选推荐通过。原件保存在本机 `validation/pypi-uvx-20261008/`，
核心 run `20261008T090738Z-0fb7f9b831cc47c9b06eff64f0d9ebf0`，
鹈鹕 run `20261008T090902Z-d38a4696669d49f8a40b5d5d69c8431d`。
Omarchy 本次原件位于其本机 `validation/inferyard-pypi-uvx-20261008-omarchy/`，
核心 run `20261008T094946Z-5ec95b54157f4def8942de5f46f6fe4d`，
鹈鹕 run `20261008T100000Z-cd172e231a8d46a5bb6724fc685a1b50`；
报告为该目录下 `report/report.html`。远程 Chromium 离线渲染已检查，未检查交互控件和动画时间线。
Omarchy 同样保留 1 GiB 内存下限，单次运行未配置温停，也未采集温度。
鹈鹕输出为可解析的静态 SVG，未满足动画要求；保持不评分且未重跑。
两端模型服务均已退出，最终主机状态均为 clean；原始 run 和历史报告均保留。
不据此推定 Windows、长时负载或跨设备受控性能比较资格。

## 运行证据文件合并

2026-10-11 文件合并的软件实施与验收已完成；随后按用户要求收回范围扩展，
当前修改在 `fix/remove-layout-scope-expansion`；用户已授权提交 PR，提交前合入最新 main。
[ADR 048](decisions/048-run-evidence-containers.md) 与[共享接口契约](contracts/run-evidence-layout.md)
定义当前结构；已完成的实施计划移除。PR1 读取方基线为 `e9d71a5`，最终软件验证源码为 `71b414d`。
共享 writer、运行调用方和故障回归由三个独立 owner 在隔离树实现，再由 root 集成；
成员 hash、发送前耐久、dirty/clean、停止规则、五终态、评分分母和原始证据保持。

| 实际验收 | 结果与范围 |
| --- | --- |
| 全量软件回归 | `71b414d`：5035 passed / 53 skipped；跳过项为平台或显式原生条件，不计为通过 |
| 真实 CLI 模拟 | ordinary single 为八个证据加三个报告；默认 core 120 正式题、SVG 1 正式题分别封存，两个 run 与新汇总报告共十九文件，分母不合并 |
| 故障与专业场景 | 实际写入、flush/fsync、目录 fsync、取消/崩溃、dirty/clean、期限拒发和新旧来源入口通过；27 个专业模拟场景实际 CLI 完成，未启动真实模型 |
| 离线报告 | 桌面、手机展开筛选、搜索、SVG 和无 JavaScript 回退通过；回环 HTTP 打开后断网，宿主阻断 `file://`，不将该入口计为通过 |
| 软件成本 | 当次按 1.20 倍预算验收；三种 producer scope 与 single/default report/verify 每侧五次 AB/BA，18 项耗时/峰值 RSS 全部通过，最高 1.181 倍；完整请求、评分与输入工作量等价 |
| 独立审查 | 来源/成员绑定与生命周期两项审查完成；性能增量另由两位非作者复核，无阻塞缺陷。沿用旧会话，同模型，不称跨模型审查 |

专业 CLI 与生命周期故障原件来自 `9f9e1eb` 的模拟验证；后续运行写入代码未变，
读取/校验与报告改动在最终源码重新回归，未把旧原件重签或标成新运行。
第一次默认双题包 report/verify 成本超预算，失败回执保留。
已消除重复成员分支校验、已验证冻结输入的二次验证和不必要的整图复制/解析；
完整来源重算、JSON 归一化与读取变化拒绝仍保留。历史正式测量冻结当时源码、harness 的前后哈希及原件，
不通过删字段、减少题目或调高预算取得通过。120/12000 条请求原件索引的单扫描与正文读回证据另存，
不外推无界规模。Ruff、格式、Schema/catalogue 一致性及本地文档链接/锚点检查通过。

- [x] PR1：固定布局版本与旧 v3 原件严格只读策略，建立容器/记录契约，迁移全部读取方和真实来源引用；该阶段生产 writer 保持旧布局。
- [x] PR2：一起切换 single、batch fixed/duration 的共享 writer，完成发送前持久化、dirty/排空、故障与新旧证据回归，实证普通完整 single 八个证据加三个报告文件。

默认两题包各自生成一个 run，再生成一份汇总报告，共十九文件；准备、主机状态、batch 外层、父链和选用扩展产物另计。
普通 single 仍使用内存 Sampler，不补造 collector 声明或扩大测量；旧 ResourceSampler 的额外原件按实际采集器另计。
未验证范围：三平台真实文件系统耐久/断电、原生传感器与真实模型、长时负载、新发行物构建与安装。
模拟与软件成本通过不继承旧原生或发行资格；这些能力继续单独验收。

### 范围收回

移除独立 Go observer 的 layout2 绑定、跨语言 canonical 实现和对应测试；
旧布局绑定与独立 PID 诊断保留，新布局绑定明确不支持。
删除五个新增布局验收脚本及其入口说明，保留核心回归和已有源码外验收原件。
移动端筛选 CSS 修复曾随范围收回撤回，随后经用户单独授权恢复。
核心十一/十九文件布局、来源校验、持久化与故障回归保留；固定 lane 数、
审查人数与性能预算不再作为长期门禁。上述历史验收仅绑定原源码，
不作为范围收回后源码的验证结论。

本次删减的 191 项 Python 定向回归与 Go 两个包通过；24 次真实离线 CLI 调用退出 0，
当前 writer 生成的模拟 run 与报告实际清点为八加三，重渲染核验通过。
Ruff、格式、Schema/catalogue 一致性及 75 个本地链接/锚点检查通过；
挑刺者独立静态复审无阻断发现。报告经仅提供合成 HTML 的本地 HTTP 预览，
服务停止后搜索仍工作；`file://` 被宿主阻断，未计为通过。
CSS 回退时的溢出已复现；恢复修复后，新报告重渲染核验通过，390×844 手机尺寸下
展开筛选没有横向溢出（文档与可见宽度同为 375 CSS px）。本次未重跑全量、性能、真机或发行检查。

提交 PR 前的 main 集成回归：460 项定向测试通过；另按全量收集顺序复跑前 290 项，
289 通过、1 项原生显式启用条件跳过。Ruff、格式、Schema/catalogue 一致性通过。
全量尝试中断前曾出现两个失败标记，未取得最终 traceback；前 290 项复跑未复现，
原因未确定，不声明当前集成源码全量通过。当前源码未重验性能、真实模型或发行资格。

PR #20 review：复现并修复 inputs 发布后、日志创建前中断的读取失败；未封存目录
按真实文件读取为 partial/3，未执行分母保留，不给缺失日志补摘要或引用。
已发送请求缺 prepared 原件及已封存文件缺失仍拒绝。相关 223 项回归、Ruff、格式及
Schema/catalogue 检查通过。原生 duration 的 dirty 发现经 KVMem/NInfer 真实入口核验不可达：
既有契约在创建运行和发送请求前拒绝该模式，不扩大支持范围或放松恢复要求。
GitGuardian 命中的八处值为内存诊断的合成应用分组 ID，来自应用身份 hash；公开夹具
改用明确的 fixture 标签，生产分组算法不变，不改写历史。

## 已完成的实现与验证

执行与报告成本优化已实现：资源归约建立引用索引；旁路日志在命令内同读复用；封存流式哈希；
普通核验仅保留后续消费的字节；多运行报告逐 run 释放原始样本；移除重复深拷贝；
无凭据脱敏保留容器复制，流式脱敏保留分块语义；sampler 停止唤醒；CUDA 预检复用同次观测。
报告 JSON 往返仍保留，以维持键与类型转换语义。cold-start 帮助测试显式关闭颜色。

2026-10-09 本机全量软件回归 **4699 passed / 53 skipped**；最后的递归脱敏调整另有
涉及改动的 31 项回归通过。Ruff、格式及 Schema/catalogue/community 一致性通过，community 未重验归档。
帮助测试在 `FORCE_COLOR=1 PYTHON_COLORS=1` 环境下另有 122 项通过。
固定合成输入含 3 个 run、30 个输出、1920 个样本和 4 个 SVG；两侧各 5 次交替运行，
完整产物比较和损坏证据拒绝检查通过。操作耗时如下，归约与脱敏每次测量含 10 次调用：

| 路径 | 基线中位数（范围），ms | 候选中位数（范围），ms |
| --- | --- | --- |
| 资源归约 | 49.28（48.58–51.19） | 10.18（10.04–10.41） |
| 有凭据流式脱敏 | 157.20（151.83–160.46） | 7.47（7.04–7.60） |
| 无凭据容器脱敏 | 111.30（108.63–113.02） | 76.35（74.02–77.76） |
| 三运行报告 | 322.06（319.02–337.91） | 315.46（306.83–327.16） |

资源归约和流式脱敏的改善已获本次合成测量支持；报告总耗时、读取、JSON 与带凭据容器脱敏
差距未超出波动，不宣称提速。独立 profiling 确认扫描/逐字符匹配热点减少，报告主要成本仍是
证据验证。三运行普通核验的进程峰值 RSS 中位数从 110.31 降至 104.86 MiB，包含准备与序列化。
复跑方式见[软件成本脚本](../scripts/README.md#软件成本对比)。本次不测磁盘 I/O、真实模型或
原生 Linux/Windows 资格；不清空 OS 缓存，不据此取得受控模型性能比较资格。
独立 verifier 重算首轮 120 个测量槽的内容与计数，并检查停止、flush 失败和 CUDA 注入路径。
一份合成报告在 Chromium 的 `file://` 断网环境下完成桌面/手机尺寸与 4 个 SVG 渲染检查；
它与最终候选报告仅有生成源码摘要差异。未检查全部报告或交互控件。

P0 PR3 的批次历史投影已实现，读取边界见
[ADR 040](decisions/040-batch-history-projection.md) 与[数据契约](data-contract.md#批次历史投影)。
2026-10-09 本机软件回归 **4555 passed / 53 skipped**；Ruff、格式、Schema/catalogue
一致性通过。新增 90 项回归覆盖消费字段等价、恢复链/预算拒绝、损坏证据和 lab/native
排空边界。只完成本地交付；没有真实模型或提速实测，不继承旧测量资格。

[ADR 038](decisions/038-inferyard-current-format.md) 对应的当前格式清理已完成；
[ADR 039](decisions/039-remove-host-state-migration.md) 移除了主机状态迁移与旧入口机制，
实时入口首次运行自动初始化主机状态，现行规则以[当前格式契约](contracts/inferyard-current-format.md)为准。
核心 schema、43 个指标 ID、199 个方法 ID、题包和审核证明保持；安装结果已升级为 `installed_safe_checks.v3`。

本次发布准备的已验集成基线为 [PR #1 / Package checks](https://github.com/sunny0826/inferyard/actions/runs/37716506876)，
PR 提交 `83def2565f9d057621411e2cc4ab41752e7cb9c5`，已合入 `ce221771e426e11ad6c1af3a2044cc8d845df1f2`。

| 实际检查 | 结果与范围 |
| --- | --- |
| Linux 全量 pytest | **4438 passed / 47 skipped**；跳过项按平台或显式启用条件保留，不计为通过 |
| 静态、资源与构建 | Ruff、Schema/catalogue/community 导出、wheel/sdist 与 sdist 重建通过；未重新核验上游 runtime ZIP |
| 三平台安装 | Linux x64、Windows x64、macOS arm64 均完成源码外 uv tool/uvx 26 项检查，覆盖当前报告、旧报告拒绝及合成升级/回退 |
| 固定主机状态 | 一次性 GitHub runner 上完成自动初始化、合成请求、crash/dirty 持久化及 dirty 拒绝；没有真实模型请求 |
| 早期本机验收 | 来源 `2fcb180` 的离线 HTML 桌面/手机渲染与 macOS 隔离根迁移检查已保存；原件保留，不改标为新候选证据 |

后续源码或发行物变化应建立新候选；最终清单与安装结果随 Actions artifact 保留，Release 记录其准确来源。
Windows ACL/reparse/多账户/卷变化、真实 runtime 准备和模型性能仍未验证。

## 本轮：提速、轻量、去冗余、减过度门禁

2026-10-10 本轮限定这四类，已完成下列 11 项。扩大测量、平台补证和新产品面不排期。
已落地且不再列项：`schemas_for` 缓存、单次/批量共用 `run_trial`、批次历史默认轻量投影、资源归约索引、[ADR 041](decisions/041-environment-persistence-slim.md) 的周期落盘瘦身，以及 PR #13 的写路径、同次读取、封存流式哈希和采样收尾。

下列保护留在热路径上：请求开始、终态、评分和停止的 fsync；dirty、请求前后空闲和排空；温停与内存停；资产哈希；评分分母。逐 chunk 与终态全文的一致性留到存储 ADR 替换第二份全文，不单独删检查。

### 本轮已完成

2026-10-10，11 项实现与分项回归已完成，保留在本地 `perf/slim-round` 分支，未提交、未推送。

- 命令内复用已通过的 config/bundle/plan 校验，写入边界及公共离线 verify 仍完整校验；
  DNS rebinding 检查收口到 adapter 构造，并在请求前核对实际固定的监听地址。
- Linux 两个 swap 指标同次读取 `/proc/vmstat`；完整账本与默认轻量投影共用摘要规则。
- [ADR 042](decisions/042-environment-collection-slim.md) 的常量复制、IOKit 电源与
  `host_statistics64` 换页，以及 [ADR 043](decisions/043-per-request-identity-slim.md)
  的逐请求身份复查减重已实现。`device-check` 未改。
- [ADR 044](decisions/044-prompt-content-hash.md) 的消息哈希与原快照核验、
  [ADR 045](decisions/045-report-content-refs.md) 的报告 v8 内容引用已实现；旧事件可读，旧报告拒绝。
- [ADR 046](decisions/046-single-implementation-identity.md) 的唯一执行身份门与
  [ADR 047](decisions/047-rerun-recorded-comparability.md) 的重跑/比较分工已实现；
  rerun 应更换服务进程，身份差异取消比较资格。
- [CLI 表](cli-surface.md#规范语法与兼容入口)中的旧命令名和参数别名已删除，帮助与回归同步。

最终全量软件回归 **4741 passed / 53 skipped**；跳过项为平台限制或显式启用的原生检查。
Ruff、格式、Schema/catalogue 一致性和文档本地链接/锚点检查通过。
合成 v8 报告已在 Chromium `file://` 离线环境检查文字引用、筛选、SVG、桌面/手机布局
及无 JavaScript 的文本回退；独立浏览器会话已关闭。
本轮没有性能对比、真实模型请求、长时负载、发行安装验收或新增平台测量资格；不声称提速。

### 不排期

扩大测量（原 B01–B11）、Windows 原生准备专项、Darwin 启动身份依赖替换、方法目录缩小、源码获取 CLI、engine-fit 接入、报告架。
报告 JSON 归一化已测过，没有明确提速。生命周期内容级回归是加检查，不排。
`run_preflight` 丢参和空凭据语义是缺陷，不属于本轮四类，单独另开。

### 2026-10-10 追加：校验热路径预编译与重复校验消除

分支 `perf/validator-slim`（本地未推送）完成两项，仍属本轮四类：

- `validate_document` 的 oneOf 判别分支预编译、schema 节点预编译与字段路径后缀缓存；
  `_reference` 改为等价字符串检查（`validation.py`、`contracts_experiment.py`）。
  `export_schemas.py --check` 通过，schema 导出逐字节不变；独立审查对 9196 组新旧
  结构输入与 52 组路径边界做差分，判定与错误 path/reason 一致。
- `read_trial` 内三处 `trial_for` 重复校验消除（`_inputs` 直接返回 trial/workload，
  `run_projection` 同步复用）；`Observations.add` 移除逐条
  `validate_document("metric_observation")`，summary 与 analysis 的
  `array(METRIC_OBSERVATION)` 终校验完整覆盖同一批对象，错误时机移至容器校验。

全量软件回归 **4742 passed / 53 skipped**；Ruff、格式通过。合成 fixture
（120 请求 × 200 chunk，交替运行、无 profiler）`read_trial` 中位数 702 → 371 ms（约
1.9×）；不据此宣称真机报告提速。写路径与采样未改动。
审查遗留一条非阻塞建议：扩展协议（NATIVE_TOOLS 等）本地 spec 每次调用重新编译，
主热路径不受影响，留后续轮次。

## 交付目录组织（#28）

2026-10-11 修复 [#28](https://github.com/sunny0826/inferyard/issues/28)：
Windows Bonsai 真机测评的完整工作区含 1,287 个文件，正式产物与初始化检查、
操作员脚本、服务日志、截图和 4 个 Chrome profile 混放，端到端交接只核验了
正式 run 与报告，未核验完整交付目录清单。[使用指南](usage.md#工作区与交付目录组织)
新增交付组织规则：11/19 为正式输出计数，与工作区总文件数分开表述，不宣称
"每次测评只新增 N 个文件"；交付目录只复制封存 run 与共享报告并保持相对布局，
交接清点完整交付目录；浏览器 profile 放交付树外的专用临时目录，检查后只回收
任务自建临时数据；准备与操作员证据留在原工作区分开存放。
README、README.zh-CN、CHANGELOG 同步计数限定。纯文档改动，本地链接/锚点检查通过。

2026-10-11 二次修订（用户决策：整体移除浏览器 profile 环节）：核实评分链路后
确认 benchmark 执行与评分从不使用浏览器——`zh-svg-pelican` 题 `rules` 为空、
不做质量评分，CLI 仅做 SVG XML 语法与大小检查，动画效果无自动判定；上轮的
"profile 放交付树外临时目录"规则仍默认要创建隔离 profile，为操作员脚本留下
口子。[使用指南](usage.md#工作区与交付目录组织)改为：人工检查用日常浏览器直接打开
report.html，不创建隔离 profile、不编写浏览器检查脚本，任何情况下（含
`--user-data-dir` 指向工作区内部目录的浏览器调用）都不在 benchmark 工作区内
创建或存放浏览器用户数据；确需隔离环境时用户数据放系统临时目录。工作区
预期文件量回到正式输出 19 + 准备/操作员辅助产物量级，不再出现千级 profile
文件。候选增强（未实施）：报告内对 SVG 输出做动画元素统计展示，进一步减少
打开浏览器的需求。

独立挑刺审查（挑刺者会话，只读）确认四项核心事实主张全部成立（评分链路无
浏览器调用、SVG 免质量评分、仅 XML 语法与大小检查、无动画自动判定），并
指出一处阻断残留与三处收紧：usage.md 运行与报告一节残留的"浏览器验证文件"
合法辅助产物表述已删除；交付树内同样禁止浏览器用户数据且不属于可登记的
可选新增；"确需隔离"出口限定为访客/隐身窗口或一次性系统临时目录
`--user-data-dir`，同样不编写自定义检查脚本；CHANGELOG 英文措辞同步消除
"isolation profiles 一律禁止"的歧义。

2026-10-11 三次修订（用户决策：准备与操作员辅助产物移出 benchmark 工作区）：
核实准备链路后确认候选/绑定等准备目录可整体外置——全部准备命令 `--out`
接受任意新目录，`config create --results` 不允许位于候选目录内
（community_candidates.py:97），run 目录自含冻结输入，verify/report 不依赖
工作区外的准备目录，`report --out` 自动创建父目录
（report_common.py:81）。[使用指南](usage.md#工作区与交付目录组织)定义双目录
布局：操作目录（如 `bench-ops/`）承载 init 导出、模型、preflight、候选、绑定、
操作脚本、日志、截图、导出与传输准备；benchmark 工作区（如 `bench-work/`）
只存放 run/batch 与报告，由 `run` 和 `report` 自动创建。操作目录中的证据与
日志原样保留，不为凑数删除（issue #28 验收要求）。README、README.zh-CN、
installation、cli-surface 的示例路径同步改为 `bench-ops/` 准备 + `bench-work/`
正式产物；节名由"交付目录组织"改为"工作区与交付目录组织"并同步全部锚点
引用。此布局为文档约定，CLI 不强制；历史工作区（含 Windows 1,287 文件原工作
区）不迁移不改写。约 131 个非 profile 辅助文件的逐文件归属清单因 San3an-win
掉线暂未枚举，类别层面结论不依赖该清单，待机器恢复后可另行核对。

真机重组由 San3an-win 独立会话（Codex auto-review）完成：原工作区
`D:\bench-work\bonsai-597367d-after-release-20261011-112436` 未改动，重组前后
递归 1,287 文件、全部文件大小与 SHA-256 一致；两个封存 run 与共享报告以
`shutil.copy2` 复制到 `D:\inferyard\deliveries\bonsai-597367d\`，恰好 19 文件
（2×8 证据 + 3 报告）且与原件逐字节一致。报告的 `../work/results/...` 相对
来源引用保持有效，未使用 `--source-root`；入口为源码 venv 的 inferyard 0.0.1
（Python 3.14.7）。报告、core run、SVG run 三次 `verify` 均退出 0
（bytes/sources/semantic verified，两 run sealed，120/120 与 1/1）。
本次未重渲染 HTML（`render_checked=false`）。首次 SVG 核验因 Windows GBK
控制台无法输出自行车 emoji 退出 1，设置 `PYTHONIOENCODING=utf-8` 与
`PYTHONUTF8=1` 后三次全部通过；该控制台编码问题与证据无关，另行开 issue 跟踪。
未启动模型服务、发送模型请求或联网下载。

2026-10-11 候选增强实施（信息性动画元素统计）：report.html 渲染期对 SVG 题的模型
输出（报告内唯一一份 `contents` 文本）现算静态动画元素统计——SMIL 动画元素
（animate / animateTransform / animateMotion / set，含 SVG namespace 形式）计数与
`<style>` 文本中 `@keyframes` 规则、`animation` 属性声明的静态文本匹配计数（不做完整
CSS 解析）——在 SVG 画廊与逐题详情各显示一行，措辞明确标注信息性、不构成动画判定、
质量评分或资格结论。统计仅在渲染时计算：不写入 index.json、不进入封存证据、不改变
report_format_version=8 与 schema_version=3，同输入结果确定；SVG not_found / too_large /
malformed 时计数为 null 并附原因（svg_not_extracted / svg_too_large / svg_malformed），
不以 0 冒充。实现位于 reporting 层（`svg_render.animation_stats` + 渲染环境 Jinja 过滤器
+ 模板宏），无 CLI、参数、退出码或契约字段变化。usage.md"动画效果没有自动判定"结论
不变；模板字节变化由 `template_sha256` 随生成自然反映，旧报告不受影响。单测覆盖
SMIL（含 namespace/裸名）、@keyframes、animation 简写与长名排除、无动画真实 0、三类
缺测 null+原因；集成测试断言统计不进 index.json、HTML 展示正确且 `--rerender` 通过。

2026-10-11 四次修订（用户决策：操作目录全清理 + 双目录同步补全）：独立挑刺审查
b0f8b1b 后修复两处阻断——MACOS.md、WINDOWS.md（issue #28 的平台指南）、
configs/README.md 补同步双目录布局；usage.md 浏览器禁令收回为"benchmark 工作区或
操作目录"（原"任何目录树"单方面扩大了 3df4ac6 定案范围且与系统临时目录例外
自相矛盾）。init 导出的入门说明（data/community/README.md）改称操作目录，
`resources.json` 经 `scripts/export_community.py`（`generated(None)`）再生且
`--check` 通过。按用户决策新增清理规则：本轮全部 run 封存、汇总报告 verify 退出 0
后，操作目录本轮内容可整体清理——封存证据自含冻结输入，verify/report 此后不读
bench-ops；清理为操作员显式手动操作，CLI 不自动删除，补跑重走准备流程（PID 变化
后本须重新绑定）。审查建议同步落实：安装指南说明骨架空 `results/` 不使用、
`--results` 指向工作区；README 与安装指南的自动创建措辞补 `batch`；文档地图不再
称安装指南含"工作区"；CHANGELOG"任意 --out 路径"修正为"父目录须已存在的新目录"。
San3an-win 已恢复在线，131 文件逐文件清单核对仍未执行，与 PR 无关可后续进行。


## 模型获取简化

`model acquire` 已实现固定 Hugging Face/ModelScope 单 GGUF 获取、来源核验和候选绑定。
2026-10-12 按操作者决定简化为 curl 级进程内下载：移除子进程 worker、IPC 进度协议、
远程元数据预核验、手工重定向白名单、三段预算与排他原子改名；新记录为
`remote_model_source.v3`，v1/v2 严格只读。依据 [ADR 051](decisions/051-simplified-model-acquisition.md)。
现行规则见 [CLI 参考](cli-surface.md#各动作的冻结行为)和[模型获取契约](contracts/model-acquisition.md)。
正式 run 仍只使用本地已绑定服务。真实 CDN 性能仍未验证。

## 引擎获取与闭环下载 e2e

2026-10-12 新增 `engine acquire`（用户授权的 curl 级命令）：单 HTTPS 链接流式
下载引擎归档/二进制到新目录，实测 bytes/SHA256 写 `remote_engine_source.v1`
记录；不限 host 与扩展名、无 token、不解压不装配不启动，Windows 归档仍经
`runtime prepare --archive` 固定身份核验，引擎身份由 `config create` 层冻结。
规则见[引擎获取契约](contracts/engine-acquisition.md)，取舍对齐
[ADR 051](decisions/051-simplified-model-acquisition.md)。

同日新增 opt-in 闭环 e2e（`tests/integration/test_closed_loop_download_e2e.py`，
env `LAB_E2E_CLOSED_LOOP=1` + 两个来源 URL）：`model acquire` + `engine acquire`
真实下载后驱动 `ci_model_e2e.py` 全链（装配、`config create --model-source`
血缘绑定、双题包运行、report、verify）。macOS arm64 真机全绿（约 9 分钟），
回执归档本地 `validation/closed-loop-e2e-20261012/`；zh-smoke 40/40 complete、
pass 1（135M 模型低分非 CLI 失败）。硬件安全停止保留为 incomplete 证据不重试。
未验证范围：Windows/Linux 闭环、其他来源、真实 CDN 性能与正式双题包资格。
`ci_model_e2e.py` 增加可选 `--model-source` 透传候选血缘（默认行为不变）。

## 模型 e2e 三平台接入

2026-10-11 新增 `tests/packaging/ci_model_e2e.py` 与 `model-e2e` workflow（分支
ci/model-e2e，基于 #31 分支）：以 SmolLM2-135M-Instruct Q4_K_M（105MB）+ Prism
b10743 归档驱动真实模型全链路——init（zh-svg-pelican 单题包，用户显式固定）→
device-check → 平台准备（macOS 自动候选 / Windows runtime prepare 按预检推荐
CPU 或 CUDA / Linux config assets + 手工模板填实测环境值）→ 外部启动 llama-server
→ bind → run（真实推理）→ report → verify --rerender → 断言工作区 11 文件
（run 8 + report 3）、SVG 统计行、删除整个 bench-ops 后 verify 仍退出 0。

三平台本机实测全绿：macOS arm64（Metal）、Windows x64（CUDA 12.4，San3an-win）、
Linux x86_64（CPU，omarchy 物理机）。Linux 经六轮迭代修复三处脚本结构性问题
（TOML 引号丢失、模板 environment "unknown" 透传而 Linux 准入无豁免、
seed_support 未声明 "supported"），另规避一个引擎侧问题：Prism chat handler 的
peg-native 解析对 seed=42 在 Linux CPU 量化路径生成的无效 UTF-8 字节返回
HTTP 500（/completion 端点同字节 200；seed 1/7/123/2024 均 200；macOS/Windows
同 seed 正常），e2e 改用 seed 1，引擎容错待上游 PrismML-Eng 处理。macOS 首次
执行签名的引擎二进制链需约 13 秒（超出 CLI 10 秒版本查询预算），脚本在解压后
做一次 180 秒预算的 --version 预热。已知前提：云 Linux runner 无 cpufreq sysfs
时（governor 为缺测）严格环境准入会以明确错误阻断 run；经 continue-on-error、
探测加条件跳过两轮迭代后，按用户最终决策 Linux leg 从 CI 矩阵移除，Linux 以
本机物理机（omarchy 全链全绿）验证为准，同一脚本可在任何满足前提的机器上运行；
按用户决定，资产不做字节校验（Windows 引擎归档的固定
SHA 仍由 runtime prepare 内置校验）。Windows/omarchy 会话各轮现场全部保留。

2026-10-11 首次真实 CI 运行（PR #32）另暴露并修复一个 macOS 候选生成缺陷：
无电源 profile 的机器（macos-15 runner）把 None 写入 conditions.profile 导致
契约拒绝、无法生成候选——生成器改为实测 None 时保留既有 "unknown" 兜底
（macOS 准入本就豁免实测 None 的 unknown 字段，严格性不变），带单测回归
（b69f487）。

2026-10-11 e2e 补齐真实评分链路（分支 ci/e2e-zh-smoke）：原 e2e 仅跑
zh-svg-pelican（SVG 类不计质量分），真实模型输出的评分从未被执行。改造后
同一外部 llama-server 依次跑 zh-smoke（40 道 instruction/extraction 真实
评分题）与 zh-svg-pelican，`report --runs SMOKE SVG` 生成双 run 合并报告
（side-by-side comparison 仅 bundle 相关字段 different，80 项 same），并新增
内容级断言：40 题 quality_state 全为 pass/fail、dashboard 分母 40 且
numerator 与 pass 数一致、SVG 题 not_applicable、报告含「答案通过数」与
「动画元素统计」、工作区 19 文件（8+8+3，即正式输出计数）、删除两个
bench-ops 目录后 verify 仍退出 0。操作目录改为 bench-ops-smoke /
bench-ops-svg，Linux 候选的 bundle 版本改为从题包文件读取。macOS arm64
（Metal）本机实跑全绿：SmolLM2-135M 得 1/40（小模型预期低分，验证的是
评分链路而非模型质量），总耗时约 3 分钟。Windows/Linux 两条腿尚未以新
流程实跑（CI 将覆盖 Windows；Linux 仍需本机 omarchy 复跑确认）。

2026-10-13 首次上手引导落地（分支 feat/first-run-onboarding，commit 1ea41cd）：
新增只读 `progress --workspace`（progress.v1，退出码仅 0/2）、`config create
--workspace` 推导模式（显式参数优先，旧入口不变）、init 工作区新增
preflight/candidates/bound/reports 目录与中文路线图 README（`{{WORKSPACE}}`
导出替换）；Studio 新增工作区设置（默认 `~/.inferyard`）、readiness/provision
软门槛、progress 适配层与首页向导。真实真机验收通过：SmolLM2-135M（e2e 同款）
经 Studio 向导接口完整走 acquire → device-check → 双题包候选/绑定/运行 →
report+verify，`default_scope_complete: true`，两 run 封存、报告 verified
（zh-core 120 题约 3 分钟）。遗留：macOS 上引擎 `--version` 查询偶发 12 秒
签名校验卡顿，超过固定 10 秒超时导致 `config create` 暂态失败（重试即过）；
向导或文档宜提示预热/重试，是否放宽超时另行决策。

2026-10-13 版本查询超时问题已处理：两次主动复现（服务空闲 + 生成负载下各
8-12 次 --version）均未复现 12 秒卡顿；结合 e2e 既有记载（新解压 ad-hoc
签名二进制执行期校验可超 10 秒，CI 在超时预算外预热），定性为 macOS 执行期
签名/扫描延迟的偶发表现。修复：`config create` 的版本查询默认超时 10→30 秒
（runtime_verification.query_version），Windows `runtime prepare` 维持冻结
profile 的 10 秒不动（改 profile 需固定归档重生成资源）；契约文档同步，新增
回归断言预算 ≥30 秒且超时仍映射 engine_version_query_failed。

2026-10-13 向导改为 MyGo 原生 UI 窗口（阶段二，分支 feat/first-run-onboarding）：
用户手动验收结论 web 向导交互差（裸命令 textarea、阶段枚举直出、无整体进度
感），方向确认为原生 UI。新增 `studio/internal/wizard`（Manager + viewState
直调 core，六站步骤轨：准备/设备检查/候选/启动与绑定/运行/报告核验；命令仅
在服务启动步出现且只读可复制；阶段全部人话；失败给重试不自动重试；token 只
接受环境变量名）；main.go 在 readiness 未完成或 CLI 未安装时自动开向导窗口，
`mygo.Bind(WizardBridge)` + `mygo generate` 产出 TS client（bindings 路径
web/src/mygo.ts，新增 mygo-runtime 依赖）；web 向导页降级为入口卡（进度人话
摘要 + 打开向导按钮，mock 模式不打包 mygo 代码），移除命令/进程/endpoint 表
单。协调者修正：Go 侧不再改写 progress next_command 的 --out（CLI new_output
已保证新目录，改写破坏 stem-N 产物命名），Bind 移至 App.Run 前顶层（generate
模式不执行 WhenReady）。验证：go vet/test（含 ui.NewTester 五状态 view 测试）、
build:web、verify.mjs（mock 与 wire 全过）、mock 产物无 mygo 代码、首跑/配置后
两态桌面冒烟进程稳定。未验证（留给用户手动验收）：原生窗口视觉呈现、入口卡
点击聚焦向导、完整真机向导流程重跑。

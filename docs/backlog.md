# Backlog

更新于 2026-10-11。当前进度集中在本页；证据解释见[血缘规则](data-contract.md#证据血缘与比较结论)。

## 新功能：应用内存诊断

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
规则见[诊断契约](contracts/memory-check-contract.md)；Linux MC-1b、Windows MC-1c
继续见[后续计划](plans/memory-check.md)，目前均返回不支持平台/退出 2。

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

## 设计中：远程模型链接获取

[设计文档](plans/remote-model-link-design.md) 定义了独立的单 GGUF 获取命令。
它接收固定的 Hugging Face 或 ModelScope 文件链接，核对远程清单后写入新的本地目录。
正式 `run` 仍不接收 URL，也不启动模型服务。设计尚未实施，不改变当前命令行为。

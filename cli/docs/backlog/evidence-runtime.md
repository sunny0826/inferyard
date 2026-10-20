# 运行证据与执行成本

[返回 Backlog](../backlog.md)

## 普通运行 CPU 与温度采集

2026-10-20 按 [ADR 054](../decisions/054-single-resource-collection.md) 接通 macOS/Linux
single 的完整资源采集；Studio 保留逐来源观测，提供独立曲线与温度来源选择器。
旧内存证据保持原样。实施计划已完成，规则见 ADR 与
[资源投影](../../../docs/subsystems/studio-panel/runtime.md#资源投影)。

CLI 全量回归 5637 passed / 54 skipped；Studio 全量、页面/模拟一致性、release 隔离、
Go vet、Ruff 与 Schema/catalogue 检查通过。独立审查发现的 CPU 跨请求缺测已用先失败、
后通过的回归修复。本机原生采集经模拟 HTTP 服务跑通 single 的 probe/run/封存，
新原件包含 CPU 与 57 路温度来源，Studio 投影读回通过；没有启动真实模型。

剩余验收：原生窗口 E2E 被 macOS 锁屏前置检查阻断，解锁后须重跑
`mise run test:studio-ui`。Linux 原生设备、Windows CPU/温度能力、真实模型与完整采集成本
尚未验证；本轮不继承旧成本资格。Windows 普通 single 仍仅采内存。

## 运行证据文件合并

2026-10-11 文件合并的软件实施与验收已完成；随后按用户要求收回范围扩展，
当前修改在 `fix/remove-layout-scope-expansion`；用户已授权提交 PR，提交前合入最新 main。
[ADR 048](../decisions/048-run-evidence-containers.md) 与[共享接口契约](../contracts/run-evidence-layout.md)
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
上述计数对应当时的内存 single。新 macOS/Linux single 的采集范围由
[ADR 054](../decisions/054-single-resource-collection.md) 扩展，额外原件按实际采集器另计，旧证据不补造。
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

## 本轮：提速、轻量、去冗余、减过度门禁

2026-10-10 本轮限定这四类，已完成下列 11 项。扩大测量、平台补证和新产品面不排期。
已落地且不再列项：`schemas_for` 缓存、单次/批量共用 `run_trial`、批次历史默认轻量投影、资源归约索引、[ADR 041](../decisions/041-environment-persistence-slim.md) 的周期落盘瘦身，以及 PR #13 的写路径、同次读取、封存流式哈希和采样收尾。

下列保护留在热路径上：请求开始、终态、评分和停止的 fsync；dirty、请求前后空闲和排空；温停与内存停；资产哈希；评分分母。逐 chunk 与终态全文的一致性留到存储 ADR 替换第二份全文，不单独删检查。

### 本轮已完成

2026-10-10，11 项实现与分项回归已完成，保留在本地 `perf/slim-round` 分支，未提交、未推送。

- 命令内复用已通过的 config/bundle/plan 校验，写入边界及公共离线 verify 仍完整校验；
  DNS rebinding 检查收口到 adapter 构造，并在请求前核对实际固定的监听地址。
- Linux 两个 swap 指标同次读取 `/proc/vmstat`；完整账本与默认轻量投影共用摘要规则。
- [ADR 042](../decisions/042-environment-collection-slim.md) 的常量复制、IOKit 电源与
  `host_statistics64` 换页，以及 [ADR 043](../decisions/043-per-request-identity-slim.md)
  的逐请求身份复查减重已实现。`device-check` 未改。
- [ADR 044](../decisions/044-prompt-content-hash.md) 的消息哈希与原快照核验、
  [ADR 045](../decisions/045-report-content-refs.md) 的报告 v8 内容引用已实现；旧事件可读，旧报告拒绝。
- [ADR 046](../decisions/046-single-implementation-identity.md) 的唯一执行身份门与
  [ADR 047](../decisions/047-rerun-recorded-comparability.md) 的重跑/比较分工已实现；
  rerun 应更换服务进程，身份差异取消比较资格。
- [CLI 表](../cli-surface.md#规范语法与兼容入口)中的旧命令名和参数别名已删除，帮助与回归同步。

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

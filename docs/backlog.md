# Backlog

更新于 2026-10-10。当前进度集中在本页；证据解释见[血缘规则](data-contract.md#证据血缘与比较结论)。

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

# 公开准备与验证

[返回 Backlog](../backlog.md)

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
复跑方式见[软件成本脚本](../../scripts/README.md#软件成本对比)。本次不测磁盘 I/O、真实模型或
原生 Linux/Windows 资格；不清空 OS 缓存，不据此取得受控模型性能比较资格。
独立 verifier 重算首轮 120 个测量槽的内容与计数，并检查停止、flush 失败和 CUDA 注入路径。
一份合成报告在 Chromium 的 `file://` 断网环境下完成桌面/手机尺寸与 4 个 SVG 渲染检查；
它与最终候选报告仅有生成源码摘要差异。未检查全部报告或交互控件。

P0 PR3 的批次历史投影已实现，读取边界见
[ADR 040](../decisions/040-batch-history-projection.md) 与[数据契约](../data-contract.md#批次历史投影)。
2026-10-09 本机软件回归 **4555 passed / 53 skipped**；Ruff、格式、Schema/catalogue
一致性通过。新增 90 项回归覆盖消费字段等价、恢复链/预算拒绝、损坏证据和 lab/native
排空边界。只完成本地交付；没有真实模型或提速实测，不继承旧测量资格。

[ADR 038](../decisions/038-inferyard-current-format.md) 对应的当前格式清理已完成；
[ADR 039](../decisions/039-remove-host-state-migration.md) 移除了主机状态迁移与旧入口机制，
实时入口首次运行自动初始化主机状态，现行规则以[当前格式契约](../contracts/inferyard-current-format.md)为准。
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

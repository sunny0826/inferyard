# Backlog

更新于 2026-10-09。当前进度集中在本页；证据解释见[血缘规则](data-contract.md#证据血缘与比较结论)。

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

后续源码或发行物变化必须建立新候选；最终清单与安装结果随 Actions artifact 保留，Release 记录其准确来源。
Windows ACL/reparse/多账户/卷变化、真实 runtime 准备和模型性能仍未验证。

## 后续工程工作

| 工作 | 边界 |
| --- | --- |
| 原生准备与平台边界 | 按候选字节补 Windows ACL/reparse/多账户/卷变化及原生 runtime prepare/create 专项验证 |
| Darwin 启动身份 | 评估原生接口替换 psutil 私有接口依赖；替换前保持锁定版本和完整身份核验 |
| CLI/字段维护 | 评估兼容入口提示及 `ready_to_run` 字段的下一版本语义；现有读写与命令继续兼容 |
| 方法目录 | 是否缩小机器化目录维护面另行决定；当前 199 个方法 ID 与生成源保留 |
| 源码获取宿主 | 库已有实现，真实获取 CLI 尚未接入；仅在确认实际需求后推进，基础测试不等于引擎构建完成 |
| 引擎观测 | KVMem/NInfer 的原生信号与 lab 完整观测分开；精确模板预算、有效参数和引擎内部排空缺测仍披露；engine-fit 接入未完成 |
| 可维护性 | 按实际触及范围整理模块与兼容分支，不以全库重构作为发行前提 |
| 批次投影摘要去重 | `run_projection.py` 的 `_summary` 与 `ledger.py` 摘要规则为复制逻辑（等价回归已钉住）；后续抽共享函数消除双份维护 |

## 可选测评扩展

以下工作各自冻结模型资产、题包、预算和停止条件后执行，不使用历史授权自动启动。

| ID | 待办 | 所需证据 |
| --- | --- | --- |
| B01 | 扩大第二模型覆盖 | 已有 Qwen3-4B 单设备轮次；继续补目标平台及模型组合 |
| B02 | 输入长度与材料位置扫描 | 模板后 token、位置和变更题目审核 |
| B03 | 30/60 分钟持续负载 | 独立冻结窗口、停止和排空 |
| B04 | 同源 Q4/Q8 量化对照 | 同源 revision、转换工具与配方 |
| B05 | 1/2/4 并发 | 实际槽位、在途请求与客户端开销 |
| B06 | 原生工具调用 | 选择、参数、执行、最终答案分开评分 |
| B07 | GPU、频率与能耗 | 原生来源、权限、单位和窗口 |
| B08 | 真 token 间隔、排队、启动时间 | 原生事件与时钟，不用网络块替代 |
| B09 | 当前源码的原生覆盖 | 各平台正常、故障与完整题包；Linux 历史温停/豁免诊断不作为正式通过 |
| B10 | 严格性能比较 | 完整采集开销与目标绑定，现有历史数据未取得严格资格 |
| B11 | 电源与身份读取开销 | 保留身份/资产校验，优化后重新测量 |

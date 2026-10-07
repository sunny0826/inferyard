# Backlog

更新于 2026-10-07。当前进度集中在本页；证据解释见[血缘规则](data-contract.md#证据血缘与比较结论)。

## InferYard 首次公开准备

项目名为 **InferYard**，仓库名、Python 包名及 CLI 均为 `inferyard`，版本为 `0.0.1`，许可证为 MIT。
新仓库从已清理源码建立全新 Git 历史，不导入源仓库历史、原始运行、模型或真实设备配置。

- 已完成源码复制、包及 CLI 重命名、项目链接和活动报告品牌更新；独立监测命令为 `inferyard-observer`。
- 软件检查通过：pytest **4398 passed / 52 skipped**；观察器更名后另有 27 项相关回归、Go race 测试及六目标编译通过。
- Ruff、Schema/catalogue/community 导出一致性、457 处本地文档链接和公开文件检查通过；桌面与手机宽度报告实际离线渲染通过。
- `0.0.1` wheel/sdist 构建、sdist 重建、候选核验及字节暂存通过；macOS arm64 源码外安装 26 项检查通过。
- GitHub 目标为 `sunny0826/inferyard`；本地公开准备完成，仓库创建与上传待完成。
- 未上传 PyPI、发布发行版本或执行真实模型请求。Linux/Windows 当前候选安装与原生模型准备尚未验证。

本地候选绑定源码 `482cc1b0ade47ef59f3b7f629224de01fd810099`，后续检查状态文档不改候选字节。
候选清单 SHA-256 为 `8a6aa1fae94a9f159e8ab50512058bf63a2e8f54b73344134e021d0796849a43`，
wheel 为 `3dcd2363c31ff3a34f92bde4dbd114e9a82a94ac06f942a7ef5c6d6cbdafbf0b`。
构建及候选原件位于忽略目录 `dist/inferyard-{v0.0.1,candidate-v0.0.1,stage-v0.0.1}-20261007/`，
源码外安装结果由操作者本机保存，不随公开源码分发。
安装使用合成输入；观察器的原生集成使用合成 TCP 服务，均不构成真实模型或性能验收。

## 兼容边界

当前源码按 [ADR 038](decisions/038-inferyard-current-format.md) 实施局部取代；
历史支持承诺的取代范围以该决策和格式矩阵为准。

- 核心数据保持 `schema_version = 3`，Schema URN 保留 `urn:local-ai-bench:`，不改字段与 ID。
- 主机状态按 ADR038 一次性显式迁移到 inferyard-host.*；退休旧入口，pending 不放行，ready dirty 不被迁移清空。
- 历史报告模板和运行迁移器已删除；已审核题包及嵌入审核证明保持原字节，旧名称属于原件内容。
- 源项目的测试和真机记录不自动转移为 InferYard 新候选的验收结论；当前包须独立检查。

## 当前格式清理与主机锁迁移

- 用户已授权只支持当前生产格式、删除旧运行迁移与历史报告兼容；T0 已获独立 Reviewer approve，父会话已批准 P0；单 worker 已完成软件实现与定向回归，待独立 Reviewer/root 终审。
- 基线 `fe1052a41afbddc1b7ceff852e794d6d872977ce`；[格式矩阵](contracts/inferyard-current-format.md#格式支持矩阵) 保留 engine-fit 当前多版本、评分定义及已审核题包证明，不按 v1/v2 名称清理。
- [锁事务](contracts/inferyard-current-format.md#锁迁移) 采用显式 clean 迁移、固定旧入口退休和新锁/state 独立运行。原字节凭据、pending→ready、中断重试及首次部署已实现；测试仅使用隔离临时根，未操作真实主机状态。
- 旧 HostLock 固定为源项目 `2fa125ccbe97d7d029fbbea494970f2336829ca2`，与新基线仅包名不同；公共退休 state 能阻断该基线，Windows 后加 D 不独自构成绕过。管理员破坏公共状态和任意古老 D-only 程序单列边界。
- installed_probe 的历史 report1–6 成功路径改为 report7 成功及旧格式拒绝，安装结果升 installed_safe_checks.v3；不继承旧安装资格，候选外壳 community_distribution.v2 保留。
- 退休事务、固定旧基线、安装 v3/candidate v2 已按 P0 实现；交付审核与集成 Gate A/B 尚未完成。
- 后续 Reviewer 审核、必要定向修复与父会话 Gate A/Gate B 见[计划](plans/inferyard-current-format.md)；不继承此前构建或原生验收，Windows 无新原生证据时保持未验。

### P0 软件交付与定向证据（2026-10-08）

- 已删除历史运行迁移器、旧模板与旧格式分派；格式拒绝独立返回 unsupported/2，损坏 seal/hash 保持 4。
  schema3 migrated 来源在直接、报告、比较、公开包递归入口拒绝；题包内嵌审核证明保留。
- engine-fit plan1–4/run1–6、plan1 全目录身份及 scorer1/2 保留；当前比较只计算 phase2.v3。
  候选筛选同步解析 comparison4 的相对来源，再核验内容及顺序，不能只比较路径字符串。
- 主机维护实现原件凭据→pending→公共退休→D 退休→ready；排他创建记录原 lock 是否存在，
  新 lock-only 可首次初始化，旧 lock-only 拒绝。正常 ready 只访问新锁/state/凭据；dirty 不被迁移清空。
  固定旧 HostLock 在实时入口前拒绝，测试使用真实子进程与隔离临时根，未操作真实主机锁。
- 安装 writer、三探针报告字段、validator 和候选消费链采用 installed_safe_checks.v3；旧 v2 资格拒绝。
  探针源码和资格夹具已验证，实际 wheel/sdist 构建、三次安装闭环仍由父会话执行。

| 检查 | 实际结果与本机证据 |
| --- | --- |
| 95 文件受影响回归 | 1884 passed、7 failed、3 skipped；失败已逐项修正并在下述最终集合复验。诊断日志 `/private/tmp/inferyard-affected-final.log`，不将初轮计为全绿 |
| 最终 17 文件定向复验 | **292 passed、1 skipped**，78.54 秒；`/private/tmp/inferyard-final-verified.log`。唯一 skip 为需要原生 Linux `/proc` 的资源组 |
| 最后增量回归 | 32 passed；主机首写前中断、凭据形状、编码与职责身份，`/private/tmp/inferyard-final-delta.log` |
| Ruff 与导出 | Ruff check/format、Schema/catalogue/community `--check`、`git diff --check` 通过；`/private/tmp/inferyard-final-checks.log`。community 未重新核验归档 ZIP |
| 静态与保护 | 292 个源码 AST/绝对包导入、资源职责路径、本地文档链接通过；28 份保护文件、依赖/元数据、43 metric IDs、199 method IDs 均不变；`/private/tmp/inferyard-final-static.log` |

上述检查不是全量最终验收、原生模型测试或性能结论。未执行最终构建安装、实际 HTML 浏览、
Windows 原生 msvcrt/ACL/reparse/多账户/卷路径验证；未发送真实模型请求、未迁移系统主机状态。
统一提交后停写交审，不 push、不合并；父会话按剩余计划裁定 Gate A/B 与 Windows 未验范围。

### P1 回审修复（基于 87bc484，2026-10-08）

父会话已复现源码外导出夹具缺依赖及 CI 新主机未初始化两项安装回归；原 P0 子集通过不覆盖这两条实际安装路径。

- prepare_inputs 导出补齐 `tests.host_state_helpers` 与其固定旧 HostLock 夹具；新增回归实际生成 inputs，
  在源码外 `python -I` 子进程构建默认 scenario，并用删除导出 helper 的反例确认源码路径不会兜底。
- CI 明确执行 **初始化 → success → crash → dirty 拒绝**。初始化受三项 disposable guard 约束，
  拒绝已有新 state/receipt；请求链及 crash 阶段要求既有 ready，scenario 禁止重复初始化。
  隔离子进程检查初始化缺失时拒绝、成功时 8 个合成请求、崩溃后的 dirty 拒绝为 0 个请求，
  同时逐字节确认 dirty、维护凭据及退休标记未被最后阶段改变；后续阶段若调用迁移器则测试立即失败。
- 清除已不可达的 migrated 性能资格分支、历史错误标签及报告中的迁移徽章/说明。
  **当前 `templates/report.html` 有变更**，模板摘要随之变化；父会话仍须实际 HTML 验证。
  Schema 历史枚举、题包嵌入证明与审核资产保持不变。
- 定向回归：141 passed（`/private/tmp/inferyard-p1-targeted.log`）；其中 6 项源码外夹具/生命周期检查
  使用复制包、临时注入锁根与真实子进程，不是 wheel 安装或 Windows 原生验收。
  旧错误标签清理后另有 30 passed（`/private/tmp/inferyard-p1-delta.log`）；Ruff、Schema/community 导出、保护基线和本地链接通过。
- 父会话 87bc484 全量结果为 11 failed、4386 passed、52 skipped；11 项旧测试构造已同步：
  cache comparison 使用当前正式封存入口；预算 V1/混入 V1 明确拒绝且原件不改；
  实际 token 绑定改用 V2 信封；旧 report/comparison 静态负例保留双 CLI 与有/无 source-root 拒绝覆盖。
  上述四文件与 CI 修复两文件最终 **59 passed**，12.55 秒，
  `/private/tmp/inferyard-p1-parent-regressions-final.log`。未重跑全量，最终全量仍由父会话负责。
- observations.py 的变化使 metrics.json 内 43 处 evidence SHA 过期；已核验差异仅为该文件哈希，
  ID/名称/单位/定义不变。该目录文件不在原 ownership，已报告且尚未修改；
  `export_catalogue.py --check` 仍因该文件过期失败，不能将本批称为全部检查通过。
  按父会话追加提交后停写的指令，提交已授权范围；目录同步转交父会话，或待明确扩展授权后追加。
  精确差异：`/private/tmp/inferyard-p1-metrics-preview.diff`；预览数据：`/private/tmp/inferyard-p1-metrics-preview.json`。
- 最终静态检查：Ruff check/format、Schema/community 导出、28 份保护文件、依赖元数据、
  43 metric/199 method IDs、292 源码 AST/导入、职责路径、本地文档链接和 diff 空白检查通过。
  全部结果（含 catalogue 唯一失败）记录在 `/private/tmp/inferyard-p1-final-checks.log`。

本 worker 不运行固定系统路径脚本或全量 pytest，不修改父会话冻结副本、main 或原项目。

## 后续工程工作

| 工作 | 边界 |
| --- | --- |
| 原生安装与准备覆盖 | Linux/Windows 源码外安装、Windows 路径句柄/代码页/无 D 盘和多账户锁、原生 runtime prepare/create 按候选字节补验证 |
| Darwin 启动身份 | 评估原生接口替换 psutil 私有接口依赖；替换前保持锁定版本和完整身份核验 |
| CLI/字段维护 | 评估兼容入口提示及 `ready_to_run` 字段的下一版本语义；现有读写与命令继续兼容 |
| 方法目录 | 是否缩小机器化目录维护面另行决定；当前 199 个方法 ID 与生成源保留 |
| 源码获取宿主 | 库已有实现，真实获取 CLI 尚未接入；仅在确认实际需求后推进，基础测试不等于引擎构建完成 |
| 引擎观测 | KVMem/NInfer 的原生信号与 lab 完整观测分开；精确模板预算、有效参数和引擎内部排空缺测仍披露；engine-fit 接入未完成 |
| 可维护性 | 按实际触及范围整理模块与兼容分支，不以全库重构作为发行前提 |

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

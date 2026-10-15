# 本项目配置与废弃路径审计

审计基线：`99a04002816417f79aee9bf397a4435eb1e965f1`，初始工作区干净。
用户已将目标明确为 InferYard 本项目，并决定整体退役独立 Observer。
本轮保留 CLI 与原生 Studio 的现有功能，删除独立项目及获证的无效入口。
不修改线上配置、服务、用户设置、已安装程序或原始运行。

## 范围与证据强度

基线枚举了全部 1,269 个 Git 跟踪资产：CLI 1,012、Studio 213、Observer 19、
跨包文档 12、CI 5、根目录文件 8。包括源码、依赖锁、配置样例、Schema、catalogue、
模板、测试、脚本、原型和文档，不以“已废弃”标签为筛选条件。
两个 gzip 夹具在内存解压检查；两个 PNG 只做字节清单，不将图片当作可执行配置。
忽略的模型、环境、缓存与 `validation/` 原件不属于活动项目资产，不改写这些材料。

除全文引用外，Python AST 记录导入及别名、常量、枚举、默认参数、动态调用和可折叠字符串。
删除后、加入本报告前的快照含 1,235 个既有跟踪文件，无读取或 Python 解析错误；
其中应用 Python 源码有 411 个模块级大写绑定、150 处 `enum(...)` 调用、77 个布尔默认参数，
没有模块级布尔开关常量。这些是扫描对象数，不是废弃项数。
Go/TS 的调用、导入别名、常量和构建入口通过全库检索及调用点逐项核对。

动态入口核对包括：`application/verification.py` 的固定 `_HANDLERS` 分派、
`contracts/contracts_experiment.py` 的 `inferyard.extensions.` 拼接、
`registry.py` 的适配器/采样器表，以及报告模板的静态 include/import。
扩展名来自已校验的合法集合，不能仅因字符串拼接便当作无引用。
`getattr` 的请求选项、适配器回调和协议覆写也不作为死代码删除。
仓库未发现 Statsig SDK、配置对象或远程 rollout 调用，本轮不再依赖 Statsig 项目。

**生产取值边界：**下表的“入口取值”来自当前源码的实际应用调用点，不等于部署遥测。
部署版本、用户配置分布、生产调用频率和生产 CPU 均为 `null`，原因是本轮没有对应遥测输入。
本地合成 profile 与性能数据另列，不能换算成生产节省。

## 按归属与文件工作流分组

归属指维护边界，不推定具体人员。若后续并行处理，各组只写其目录；
跨组契约、导航、CHANGELOG 与本报告由文档组统一修改。本轮没有并行写者。

| 归属 / 工作流 | 对象与入口取值 | 引用证据与处置 | CPU 判断 |
| --- | --- | --- | --- |
| 独立监测器 / Go 构建 → 流 → 摘要 | `observer/`，`inferyard-observer`；不是生产常量 | 整体移除 19 个模块文件、`build_observer.py`、`observer_stream.py`、`analyze_observer.py`、`observer_benchmark.py` 及四个专属测试文件。CLI 包、Studio、CI 均无二进制调用 | CLI/Studio 无此调用路径；独立工具历史 CPU 未测，不报节省 |
| Studio / 启动 → 系统通知 | `Subscribe(..., headless, ...)`；唯一应用调用固定 `false`，`true` 仅测试 | 移除参数、早退分支和仅验证已退役模式的测试；保留真实 EventBus 通知及失败投递测试 | 一次启动判断；无昂贵循环证据 |
| Studio / CLI 边界 | `Locate` / `Binary`、`Client.DeviceCheck`、`Client.Progress` | 全部只有测试调用。实际 `core.Service` 通过注入 executor 和保留的解析器完成定位、设备刷新、进度查询；删除重复包装器和专属测试 | 应用调用图不可达；不虚构测量值 0 |
| Studio / 下载 | `evidence.PollDownload`、`DownloadFile`、`DownloadProgress` | 只有自身测试调用；实际下载在 `core/downloads.go` 合并文件采样、速率与任务终态，保留该路径 | 应用调用图不可达 |
| Studio / 旧 Web 与原生页面 | `vite.config.ts`、`ApiError.HTTPStatus`、`unauthorized` 错误项、`statusPanel`、`kvGrid`、`uniqueID`、`nameError` | 已无 Web 构建器/HTTP 服务，辅助函数无调用；全部删除。更新三个内部 README，移除完成且过时的 Studio 计划与 T8 交接文档 | 无构建/应用入口，未发现运行热点 |
| CLI 报告 / 输入 → 展示 → HTML | `presentation` → `request_details` 的 `format_version`；应用传入 `8` | 最末函数从不读取该参数；移除两层内部参数及透传。[外层 `build_index`](../../src/inferyard/reporting/report.py) 仍只接受 v8，旧版本/布尔伪整数拒绝不变 | 每题一次无效传参；见五轮测量，不能确认提速 |
| CLI 服务身份 / 平台绑定 | `startup_binding(..., windows=False)`；Windows 调用 `True`，其他平台默认 `False` | 函数从不读取标志，删除参数及 Windows 传参；保留 OS 身份、可执行哈希、启动参数和模型路径校验 | 无条件分支或额外工作被跳过；无生产量化 |
| CLI 重跑 / 原件 → 新服务 | `load_rerun` 的 `scoring_context`、`identity_context` | [ADR 047](../decisions/047-rerun-recorded-comparability.md) 后不再读取这两个参数；实际评分/身份上下文仍在运行入口使用，移除无效透传及测试内无效设置 | 不是减少评分或身份核验；无生产量化 |
| 契约与文档 / 产品导航 → 支持集 | 旧 Observer 支持声明、过时 Web 操作说明 | [ADR 052](../decisions/052-retire-standalone-observer.md) 取代 009/010；支持集、根 README、脚本导航、子系统与 backlog 同步。历史原始验证日志和图片保留 | 文档变化无运行 CPU 结论 |

删除符号和路径已全局反查；剩余独立 Observer 提及限于退役说明、被取代 ADR 与历史
CHANGELOG。未保留无法执行的兼容 stub。独立监测流的支持边界只以
[格式支持矩阵](../contracts/inferyard-current-format.md#格式支持矩阵) 为准。

## 已核对并保留的配置与实验

| 归属 | 实际约束 / 取值 | 保留依据 |
| --- | --- | --- |
| CLI 契约 | `schema_version=3`、`concurrency=1`、`automatic_retries=0`、当前报告 v8 | 这些常量约束外部输入和旧证据拒绝，不能因固定取值删掉验证 |
| CLI 执行 | 五个请求终态、固定/持续协议、缓存协议、诊断标志、安全阈值 | 参与冻结计划、分母、排空和证据语义；不是 rollout 开关 |
| CLI 缓存 | `command_validation(enabled=...)` 在 run/resume/plan/probe 启用；规划和 journal 信任边界显式关闭 | 两种生产取值都有入口；内容校验缓存不是永真开关 |
| CLI 采样 | `platforms/resources_*`、`telemetry`、`engine_fit_observer_io` | 资源采样、环境与 LM Studio 原生观察仍有调用；名称相似不构成独立项目依赖 |
| CLI 开销实验 | `total_observer_control` 的 `off/on/on/off`，`collector_overhead` 的 common/boundary 配方 | 开关是被测实验因子；移除任一臂会改变实验与安全协议 |
| CLI 协议接口 | `_check_ids`、`CommonGuardSafety.check`、duration request callback 等未使用的形参 | 子类覆写/注入回调的共同签名；不能按函数体单独删参 |
| CLI 原生绑定 | `startup_binding` 的 `executable` 形参 | Linux 实参包含 `resolve(strict=True)` 的身份路径校验副作用；本轮保留该签名与求值 |
| Studio 配置 | `mygo.config.ts` 的 `version='0.0.3'` | `main.go` 通过 `go:embed` 读取，`scripts/package.py` 同源读版本；删除 Vite 不影响它 |
| Studio 运行 | 设置默认目录名 `model` / `engine`、原生状态枚举、固定下载资产与页内事件 | 均有实际调用/用户覆盖或平台分支；不因默认固定而删除 |
| CI | 手动发布 `destination=verify-only` 默认值及显式发布选项 | 是发布边界，不能据默认值消除其他路径或自动开启发布 |
| 资源与模板 | methods/metrics、community profile/bundle、JSON Schema、13 个报告模板片段 | 属于活动契约、身份或审核材料；导出一致性检查核对，未手改生成 JSON |

## CPU 测量与内容等价

结论：**无法确认性能改善。**未发现已证实昂贵且无用的热路径判断。
删除的多数内容没有应用调用；报告内部无效传参的量级也不足以据此次波动宣称提速。

本机 macOS 26.6.2 arm64、10 逻辑 CPU、mise Python 3.14.7。同一解释器与依赖环境，
基线由上述提交的 `cli/` 跟踪资产导出，候选为本轮工作树。最终格式整理后重新测量，两侧各一次预热后五轮交错
AB/BA；不清系统缓存，不终止其他进程。正式测量时 load average 约 1.49–2.10。
无服务启动、无模型请求、无失败、无超时、无重试。

输入复用 `scripts/benchmark_cost_worker.py prepare`：3 个合成运行、30 题、1,920 个
资源样本、4 个 SVG、787,916 字节答案。计时边界为 `write_report` 调用到返回，
包括证据读验、归约、模板渲染、写入和封存；排除导入、夹具生成、后续结果导出与核验。
CPU 为 `time.process_time_ns()` 差值，wall 为 `time.perf_counter_ns()` 差值。
测量记录的源码 SHA：基线 `d14c9342ad6ec2485ff21272f3dbb1f212537597f24adfa5fd8a0b3bfbfa5f0f`，
候选 `35d5daf9c606118ed9bd6779162155e8412802bba2349d2ea460b6d802aa7ca2`；
最终提交前重算候选源码身份与记录相等。

| 样本 | 基线 CPU ms | 候选 CPU ms | 基线 wall ms | 候选 wall ms |
| --- | ---: | ---: | ---: | ---: |
| 1 | 257.799 | 215.318 | 260.716 | 215.712 |
| 2 | 215.592 | 214.692 | 217.343 | 214.900 |
| 3 | 222.638 | 215.688 | 225.296 | 218.472 |
| 4 | 219.738 | 214.840 | 222.370 | 215.681 |
| 5 | 217.033 | 222.105 | 221.051 | 228.587 |
| 中位数 | 219.738 | 215.318 | 222.370 | 215.712 |

CPU 范围分别为 215.592–257.799 ms、214.692–222.105 ms，区间重叠。
独立 cProfile 样本不进入上表：候选累计约 0.491 s，主要路径为 `read_trial` 约
0.258 s 与模板渲染约 0.152 s（含编译）；累计时间有嵌套，不能相加当作独立占比。
`request_details` 两侧各 30 次，self time 约 0.047 / 0.045 ms，含其实际工作，
不能把整个函数时间归给被删参数。

每个结果都校验题数、SVG 数和答案 SHA，执行 `verify_report(rerender=True)`。
两侧 index 与 HTML 全文只允许生成源码哈希及指向同一原件的等价相对路径不同；
所有五对产物通过，输入原件哈希不变。这是离线内容与重渲染验证。
浏览器安全策略拒绝 `file://`，本轮未完成浏览器视觉检查；模板和脚本未修改。

本机可复核材料位于 `/tmp/inferyard-config-cleanup.Pfpr6G/`：
`config-audit.json`、`cpu_report_final.py`、`cpu-final/measurements.json`、
`cpu-final/summary.json`、两侧 `profile-*/cpu.prof` / `profile.txt`、
各次完整 HTML/index/manifest 与测试 JUnit。临时路径不是长期证据服务；关键测量已列于上表。

复跑方法：从基线导出 `cli/` 至新临时目录，用相同 mise/uv 环境运行现有
`benchmark_cost_worker.py --tree BASE --out NEW_CORPUS prepare`，两侧分别导入自身
`src/`，对该 corpus 的三个 run 调用 `write_report`；每侧预热一次后至少五次交错，
计时后按 `benchmark_cost_compare.py` 的 `check_expected` / `compare_outputs` 核对
实际内容、完整 HTML、来源路径和 seal。profile 另跑，不纳入比较。

## 实施顺序与验收

先保存核心观察/离线读取和 Studio 边界回归，再退役独立项目及其唯一消费者；
逐项确认无调用或无效参数后迁移调用点，同轮收拢支持契约和文档。
未采用兼容 stub，也未把独立采集迁入 CLI，避免重新引入另一套运行来源。

- 核心观察、总开销实验、registry、当前格式和离线读取：删除前后同组 68 项通过。
- CLI 无效参数定向回归：原组 104 项通过；补入 Windows 身份模拟后 133 项通过。
- Studio `go test ./...` 与 `go vet ./...` 通过，含真实 CLI + 假引擎的边界 e2e。
- 全量 Python：5325 passed、55 skipped（277.23 秒）；跳过项是平台限制或需显式启用的真实执行。
- 最终格式整理及无效测试导入移除后，重跑/身份相关 10 项再次通过。
- Ruff check/format、Schema/catalogue/community 一致性通过；community 检查未重新核验归档。
- 文档本地链接与锚点检查 390 项通过；`git diff --check` 通过。

以上为本机检查。代码提交 `d6c195c9e14f6c0ebc5aa781d12b866ad9e9f370` 的
CI 随后全部通过，补充证据如下；本节只增加文档记录，没有继续修改应用源码。

- [Package checks](https://github.com/sunny0826/inferyard/actions/runs/38761265227)：
  Linux 全量测试 5331 passed、49 skipped；构建、三平台源码外安装和候选生成均通过。
  已下载并核对 macOS/Linux/Windows 的 `installed_safe_checks.v3` 回执：
  `status=passed`、`completed=true`，均绑定同一构建清单；当前报告 v8 接受、v1–v7 拒绝。
- [Studio checks](https://github.com/sunny0826/inferyard/actions/runs/38761265601)：
  macOS/Windows 的 vet 与假引擎边界 e2e 通过；内部包单测只在 macOS 执行，Windows 跳过。
- [Model e2e](https://github.com/sunny0826/inferyard/actions/runs/38761265365)：
  macOS arm64 与 Windows AMD64 的真实 SmolLM2/Prism 双题包链路均通过。
  两侧均产出 19 个工作区文件，40 道 smoke 题全部评分，SVG 统计存在，
  操作目录清理前后报告均 verified。评分分别为 1/40、3/40，不影响 CLI 成功语义。

安装回执的 `source_commit=2499d71586ca54a67262e34360de21f9dde09f66` 是 GitHub
为 PR 生成的测试合并提交；已核对它的两个 parent 分别为基线 `99a0400` 与代码提交 `d6c195c`。
这不是 PR 已合并或版本已发布的证据。

这些 CI 数据补充了对应平台的安装与 e2e 覆盖，不是生产 CPU 遥测，也不是正式默认题包
benchmark 资格。系统通知到达、浏览器视觉和生产 CPU 仍未验证。
测试跳过不能计为通过；源码变化不继承旧资格。证据解释统一遵循
[血缘规则](../data-contract.md#证据血缘与比较结论)。

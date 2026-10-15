# 使用指南

先按[安装指南](installation.md)安装并准备候选，确认[平台范围](platforms.md)。以下直接调用已安装的 CLI；大写名称、模型路径和端点是待替换值。正式运行前核对设备、预算与停止条件。

## 首次上手与断点续接

onboarding 工作区把准备和执行产物放入各自子目录；下文的双目录 benchmark 布局仍可使用。
CLI 不选择隐式默认目录。先显式初始化，再随时查询下一步：

```bash
inferyard init --out onboarding
inferyard progress --workspace onboarding
inferyard device-check --model /path/to/model.gguf --json --out onboarding/preflight/device-1
inferyard engine check --engine /path/to/llama-server
inferyard config create --workspace onboarding --bundle zh-core --engine /path/to/llama-server --out onboarding/candidates/zh-core-1
inferyard progress --workspace onboarding
```

`engine check` 是可选的只读预核验：Windows 改用 `--runtime-receipt FILE`；
`details.eligible=false` 时按固定 reason 先行排除不合格引擎；macOS 合格时按
`details.modes` 查看 cpu/metal 各自可用性，Windows 看 `details.mode`。核验通过
不构成候选资格，`config create` 仍终审；字段与退出码见[引擎核验契约](cli-surface.md#引擎核验契约)。

按导出的 README 路线图由操作者启动服务，使用真实 PID/endpoint 绑定，再 run。
核心包完成后同样准备并运行 zh-svg-pelican，两个 run 均封存后在 reports/ 的新子目录
生成汇总并 verify。已有完整核心结果时只补跑缺少的鹈鹕；保留原件。
progress 只读输出进度，退出 0 不表示测评完成；查看 default_scope_complete 与 blocked_reason。
未完成 run 显示 resume；单次运行或固定时窗不支持续测时明确阻断，按既有规则创建新运行。
progress 不发送请求，也不启动服务；执行其 run/resume 命令前仍核对设备、预算与停止条件。
完整字段与退出码见[进度契约](cli-surface.md#首次上手进度契约)。

## 默认测评范围

未明确声明固定题包的 benchmark，默认同时覆盖以下两个题包。只有用户明确声明固定题包时，才仅运行指定题包；示例配置或工具的单题包默认值不构成该声明。

| 题包 | 范围 | 报告内容 |
| --- | --- | --- |
| `zh-core` | 120 题，六类各 20 | 冻结规则评分、时间与资源 |
| `zh-svg-pelican` | 1 道动画 SVG 鹈鹕题 | 生成原文、SVG 画廊、时间与资源；不计质量分 |

这是测评执行范围约定。省略 `inferyard init --bundle` 时，导出 `zh-core` 和
`zh-svg-pelican` 两个题包；准备与操作产物统一放操作目录，benchmark 工作区只
存放 run 与报告（见[工作区与交付目录组织](#工作区与交付目录组织)）：

```bash
inferyard init --out bench-ops
```

显式传入 `--bundle zh-smoke`、`--bundle zh-core` 或 `--bundle zh-svg-pelican` 时，只导出该题包；
`zh-smoke` 不再是省略参数时的默认题包。

按[安装指南](installation.md#3-准备候选)为两个题包分别准备候选、绑定外部服务并运行，
候选与绑定产物分别使用新目录。`run --config` 每次只执行配置中的一个题包，CLI 不会自动连续
运行两个题包。各自冻结输出预算、上下文、截止时间及服务参数；SVG 长输出不能直接沿用核心示例的 512 token 预算。调整上下文时同步候选声明和实际服务启动参数，在发送请求前通过预算与身份预检。

两个 run 都封存后，使用 stdout 返回的实际证据目录生成新报告：

```bash
inferyard report --runs CORE_RUN SVG_RUN --out bench-work/report
inferyard verify --path bench-work/report --rerender
```

当前源码候选中，完整普通初始 Prism run 各有八份证据，新汇总报告有三份文件，共十九份；
这是**正式输出计数**。主机状态、batch 外层、父链与可选高频流等 run 自身结构另计；
候选准备与操作员辅助产物存放在 benchmark 工作区外的操作目录，不计入工作区，
格式见[容器契约](contracts/run-evidence-layout.md)。不要把正式输出计数表述为
工作区总文件数。

报告分别展示两个 run，核心评分分母仍为 120，鹈鹕不并入质量分母。不同题包、预算或设备的结果不因此获得受控性能比较资格。

补齐同一轮测评时，已有完整核心结果则只补跑缺少的鹈鹕题包，再创建新汇总报告。保留原始 run 和旧报告；SVG 截断、解析失败或未满足动画要求均保留原文，不根据结果自动改预算重试。任一题包未运行或被阻断时，明确标记默认范围尚未完成。

## 工作区与交付目录组织

benchmark 工作区（如 `bench-work/`）只存放正式执行产物：run/batch 目录与报告
目录。候选准备、初始化检查、模型文件、preflight、操作脚本、服务/CLI 日志、
监控截图、导出与传输准备文件一律放在工作区外的平级操作目录（如 `bench-ops/`），
从准备阶段就不进入工作区；操作目录中的证据与日志按原样保留，不为凑数删除。
run 目录自含冻结输入与封存证据，`run --config` 接受任意路径，verify/report
不依赖工作区外的准备目录，因此该布局不改变任何核验语义。浏览器用户数据
不得写入 benchmark 工作区或操作目录（见下文）。

操作目录是本轮测评的临时配套。"按原样保留"约束的是证据与日志（服务日志、
监控截图、准备检查结果）；资产目录另有技术失效规则——移动或删除 runtime
等资产目录后，引用它的既有候选失效，需重新准备并绑定（见安装指南）。当本轮
全部 run 已封存、汇总报告已生成且核验通过（`verify` 退出 0）后，操作目录中
本轮的准备与操作产物可以整体清理：已封存证据自含冻结输入，verify/report 不
再读取 bench-ops 的任何内容，删除不影响已有证据的可核验性。代价是补跑题包
或对照重跑时需重走准备流程（PID 变化后本来也必须重新绑定）。清理由操作员
显式执行，CLI 不自动删除任何文件；清理前确认交付目录（如有）已经生成。

需要对外交付时，新建独立交付目录，只复制两个封存 run 目录与共享报告目录
（上述配置共十九个文件），保持三者相对布局不变，使报告的相对来源引用直接
有效；原始工作区与历史报告一律保留原字节，不移动、不改写、不删除。
重组后在交付目录上核验：

```bash
inferyard verify --path DELIVERY/REPORT_DIR
```

交接验收核验完整交付目录清单，而不只逐个 run 和报告目录：报告核验已含来源
与语义；另需清点交付目录，不出现预期清单之外的文件，任何额外文件明确登记
为可选新增，不为凑数删除原始证据。浏览器用户数据不属于可登记的可选新增，
交付树内同样不得出现。准备检查与操作员证据保留在原操作目录分开
存放，不并入交付树。

benchmark 执行与评分不使用浏览器：SVG 题不做质量评分，CLI 仅做 XML 语法与
大小检查，动画效果没有自动判定；报告渲染时另行展示信息性动画元素统计
（SMIL 元素与 @keyframes 计数，不构成判定）。报告人工检查是可选环节，
用日常浏览器直接
打开 report.html 即可，不创建隔离 profile，也不编写浏览器检查脚本；任何情况
下都不在 benchmark 工作区或操作目录内创建或存放浏览器用户数据，包括以 `--user-data-dir`
等参数指向工作区内部目录的任何浏览器调用。确需隔离浏览器环境（如避免日常
浏览器扩展干扰渲染检查）时，优先使用浏览器自带的访客/隐身窗口，或用一次性
`--user-data-dir` 指向系统临时目录，检查后回收本次任务自建数据，同样不编写
自定义检查脚本。

## 配置与服务绑定

已有本机服务时，可先发现监听进程与模型 ID：

```bash
inferyard service discover
inferyard --schema service_discovery
```

命令对所有发现的 loopback 监听端点发送 `/health` 和 `/v1/models` 只读 GET，
不按进程名过滤、不启动服务、不发送模型生成请求。查看 `details.services`，
将选中条目的 `pid` 和 `endpoints[].url` 用于下面的显式绑定。
同端口多进程会全部列出并标记 `ambiguous_listener`；发现结果不代表绑定可行，
bind 仍核验当前身份与候选启动参数。空列表退出 0；枚举不全退出 3 并带 limitations。
字段、固定原因及平台边界见[服务发现契约](cli-surface.md#本机服务发现契约)。

模型服务由操作者外部启动。按[配置说明](../configs/README.md)准备候选配置，核对模型、引擎、模板、启动参数和凭据引用，再绑定当前进程：

```bash
inferyard config bind --candidate bench-ops/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-ops/bound/service-1
inferyard run --config bench-ops/bound/service-1/config.toml
```

候选配置须为当前机器准备；历史 PID、路径和启动时间不能复用，测试夹具不能用于真机。`probe` 发送普通与流式诊断请求，`run` 重新预检并冻结正式输入。凭据仅通过 `endpoint.api_key_env` 引用。

正式题包使用[基础包审核](../bundles/REVIEW.md)或[组合包审核](../bundles/PHASE2-REVIEW.md)；内容、答案或规则变化后重新审核受影响项。自动参考答案测试和 `--diagnostic` 不满足正式人工审核要求。

## 计划、重跑与续测

```bash
inferyard plan --experiment EXPERIMENT.json --out PLAN
inferyard run --plan PLAN/plan.json --output-root results/BATCH_ID
inferyard run --rerun-from results/RUN_ID --endpoint-url http://127.0.0.1:48857 --server-pid 12345
inferyard resume --from-run results/BATCH_ID/runs/RUN_ID --endpoint-url http://127.0.0.1:48857 --server-pid 12345
```

`plan` 离线预览或冻结实验，不发模型请求。新批次的 `--output-root` 须为不存在或为空的目录，不同批次分别保存。`run --rerun-from` 整体新建一次重跑；`resume` 的来源须位于同一批次根目录的 `runs/` 下，且是最新未完成轮次的运行，仅选择其中未执行项，不自动重试 failed、cancelled 或中断请求。固定时窗负载不支持 `resume`，须重新冻结并执行新窗口，不能拼成连续完整实验。批量实时入口支持 Linux/macOS，Windows 限 Prism/KVMem/NInfer 支持的协议；原生覆盖见[平台状态](platforms.md)。具体协议见[实验设计](experiments/design.md)。

## 报告、比较与核验

```bash
inferyard report --runs results/A results/B --out reports/A-B-new
inferyard compare --left results/A --right results/B --out reports/A-B-comparison
inferyard verify --path reports/A-B-new
inferyard verify --path reports/A-B-comparison
```

离线命令只读原始证据，不发送模型请求；关闭服务、移除凭据后仍可生成自包含 HTML。报告、比较、重评分、导出与公开包的 `--out` 为新目录，不覆盖旧产物。比较分别判断质量、性能和资源资格，缺条件时显示原因，不强制排名。

`verify` 支持原始 run、batch、冻结 plan、engine-fit 及派生产物；单独 analysis 不属于此入口。未封存取证返回 partial/3，封存核验成功返回 0，原执行状态另行保留。新报告默认核验封存字节与来源语义，可用 `--rerender` 检查当前渲染、`--source-root OLD=NEW` 明确定位已移动的来源。旧格式遇到非空来源映射会拒绝，详见[离线读取契约](contracts/offline-reading-contract.md)。

## 公开包与扩展

```bash
inferyard public package --run results/RUN_ID --out PUBLIC
inferyard verify --path PUBLIC --source-run results/RUN_ID --config CONFIG.toml
inferyard public plan --run PUBLIC --config CONFIG.toml --out REPRO
inferyard extension freeze --spec SPEC.json --config CONFIG.toml --out EXT_PLAN
inferyard extension run --plan EXT_PLAN/plan.json --config CONFIG.toml --out EXT_RUNS
inferyard extension replay --packet FIXTURE_PACKET.json --out FIXTURE_RUNS
```

公开包只生成本地文件；`verify --source-run` 核对原运行投影，`--config` 核对本地声明，不要求当前端点或 PID。声明匹配仍为 `ready_to_run: false`，不能代替正式预检。

扩展 freeze 输入原始 spec，run 输入冻结 plan，replay 输入合成 fixture packet。run/replay 的 `--out` 是输出根目录，核验使用 stdout 返回的 `evidence_dir` 封存子目录。实时扩展入口支持 Linux 和 macOS；每项协议仍须满足自己的传感器和引擎条件，macOS 当前缺少强制温度观测的协议会阻断。合成回放不获得实测资格。各协议准备步骤见配置说明。

## 取消与 dirty 恢复

请求前取得主机锁，发送前持久化请求身份和 dirty 状态。取消或超时后停止发送并按冻结期限排空；不能确认空闲时保留 dirty。Linux/macOS 锁为 `/var/tmp/inferyard-host.lock`，Windows 使用原生 Common AppData（通常为 `C:\ProgramData`）中的同名文件；同目录的 `inferyard-host.state.json` 保存状态。

首次实时入口获得主机锁时，若状态不存在则自动创建 clean 状态，不需要显式初始化命令。
旧工具的 `local-ai-benchmark-host.*` 文件（含 Windows 旧 D 盘位置）不读取、不加锁；
v0.0.1 状态中的 `migration` envelope 不再核验，在下一次状态写入时自然消失；
遗留的 `inferyard-host-migration.json` 凭据文件保持原样。规则见[当前格式契约](contracts/inferyard-current-format.md#主机状态)。

操作者停止旧服务并准备新服务后，使用状态中的 `dirty_token` 及具体恢复说明：

```bash
inferyard run --config bench-ops/bound/service-1/config.toml --recovery-confirm TOKEN --recovery-note "已停止旧服务并核验新进程与端点"
```

程序仍核验旧进程消失、新服务身份及空闲；不要删除锁文件或绕过检查。达到冻结的硬件或温度停止条件后保留原因和计数，不反复重试同一负载。

CLI stdout 输出 JSON；`device-check` 默认人类摘要，Agent 使用 `--json` / `--format json`，见[设备检测](device-check.md)。诊断走 stderr。退出码：`0` 完成、`2` 配置/预检阻断、`3` 运行不完整、`4` 工具或证据错误、`130` 取消；模型答错不等于 CLI 失败。

## 历史输入边界

活动契约为 3；InferYard 不读取核心 v1/v2，也不接受 `origin=migrated` 运行。
旧运行迁移命令已移除，旧证据保留原件并交原项目处理。
各产物支持集合及拒绝语义见[当前格式契约](contracts/inferyard-current-format.md#格式支持矩阵)，
不能仅按版本数字判断是否支持。

日常直接使用 `run`，它包含当次普通/流式探测。独立 `probe` 仅用于可选排障；
probe 只预算探测输入，0 次预热不预算 warmup，run/resume 只预算选中题。
预算仅接受 `token-budgets.v2.json`，旧位置数组拒绝；缺测不补造为 0。
预算快照与命令内复用见[数据契约](data-contract.md#命令内复用与预算快照)。


## 描述性比较与复用

新离线比较显示口径匹配的本次差值、条件差异和样本范围；受控结论资格另列。
性能差值仍须对应逐指标证据。报告仅接受 v8，比较仅接受 format4 / phase2.v3；
旧报告 v1–v7 和旧比较 format1–3 返回 `unsupported_format` / 2，不再核验或重建。
probe/diagnostic 对电源、profile、governor、EPP 缺测或差异只记录；资源安全停止不变。
正式执行的旧配置/计划仍要求原环境匹配。新配置可在 conditions、新实验可在 experiment
声明 `environment_admission = {definition = "environment-admission.v2", required_fields = []}`；
计划声明优先于配置声明，两者缺省时保留旧严格准入。诊断仅豁免预检，
显式冻结的 `safety.check_environment` 仍会检查原 conditions 并可能停止诊断运行。
空数组表示这些环境字段仅作为性能观测。操作者可把 `"ac_online"` 等列入 required_fields。

rerun 应绑定新的服务进程，旧进程须退出；clean 同配置服务只允许适用 handoff 复用，仍做当次普通/流式探测和预热。
冷启动实验设置 `execution.require_fresh_process = true`；更换服务时操作者须先退出旧进程。
工具不替用户关闭服务。孤立评分失败仍收集后续答案，含 unscorable 的结果保持 partial/code3。

### 逐题人工审核

审核记录绑定题目、答案、规则与政策哈希。已有匹配逐题记录时只重审受影响项，不能把失配的旧整包批准自动转成逐题批准。题包维护步骤见[贡献指南](../CONTRIBUTING.md#per-case-manual-review)。

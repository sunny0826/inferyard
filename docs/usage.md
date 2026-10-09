# 使用指南

先按[安装指南](installation.md)安装并准备候选，确认[平台范围](platforms.md)。以下直接调用已安装的 CLI；大写名称、模型路径和端点是待替换值。正式运行前核对设备、预算与停止条件。

## 默认测评范围

未明确声明固定题包的 benchmark，默认同时覆盖以下两个题包。只有用户明确声明固定题包时，才仅运行指定题包；示例配置或工具的单题包默认值不构成该声明。

| 题包 | 范围 | 报告内容 |
| --- | --- | --- |
| `zh-core` | 120 题，六类各 20 | 冻结规则评分、时间与资源 |
| `zh-svg-pelican` | 1 道动画 SVG 鹈鹕题 | 生成原文、SVG 画廊、时间与资源；不计质量分 |

这是测评执行范围约定。省略 `inferyard init --bundle` 时，同一工作区导出 `zh-core` 和
`zh-svg-pelican`：

```bash
inferyard init --out bench-work
```

显式传入 `--bundle zh-smoke`、`--bundle zh-core` 或 `--bundle zh-svg-pelican` 时，只导出该题包；
`zh-smoke` 不再是省略参数时的默认题包。

按[安装指南](installation.md#3-准备候选)为两个题包分别准备候选、绑定外部服务并运行，
候选与绑定产物分别使用新目录。`run --config` 每次只执行配置中的一个题包，CLI 不会自动连续
运行两个题包。各自冻结输出预算、上下文、截止时间及服务参数；SVG 长输出不能直接沿用核心示例的 512 token 预算。调整上下文时同步候选声明和实际服务启动参数，在发送请求前通过预算与身份预检。

两个 run 都封存后，使用 stdout 返回的实际证据目录生成新报告：

```bash
inferyard report --runs CORE_RUN SVG_RUN --out bench-report
inferyard verify --path bench-report --rerender
```

报告分别展示两个 run，核心评分分母仍为 120，鹈鹕不并入质量分母。不同题包、预算或设备的结果不因此获得受控性能比较资格。

补齐同一轮测评时，已有完整核心结果则只补跑缺少的鹈鹕题包，再创建新汇总报告。保留原始 run 和旧报告；SVG 截断、解析失败或未满足动画要求均保留原文，不根据结果自动改预算重试。任一题包未运行或被阻断时，明确标记默认范围尚未完成。

## 配置与服务绑定

模型服务由操作者外部启动。按[配置说明](../configs/README.md)准备候选配置，核对模型、引擎、模板、启动参数和凭据引用，再绑定当前进程：

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
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

首次实时运行前执行 `inferyard host-state migrate`。旧锁只在这次显式维护事务中按顺序获取，
Windows 还处理维护时存在的 D 盘旧位置；旧 dirty 必须先由原项目恢复，旧锁 inode 保留。
维护保存原字节凭据并退休固定旧入口后，才提交新 ready 状态。常态运行只使用新锁、新状态和
不可变维护凭据，不再双持旧锁或镜像旧 dirty。事务与中断规则见[当前格式契约](contracts/inferyard-current-format.md#锁迁移)。

操作者停止旧服务并准备新服务后，使用状态中的 `dirty_token` 及具体恢复说明：

```bash
inferyard run --config bench-work/bound/config.toml --recovery-confirm TOKEN --recovery-note "已停止旧服务并核验新进程与端点"
```

程序仍核验旧进程消失、新服务身份及空闲；不要删除锁文件或绕过检查。达到冻结的硬件或温度停止条件后保留原因和计数，不反复重试同一负载。

CLI stdout 输出 JSON；`device-check` 默认人类摘要，Agent 使用 `--json` / `--format json`，见[设备检测](device-check.md)。诊断走 stderr。退出码：`0` 完成、`2` 配置/预检阻断、`3` 运行不完整、`4` 工具或证据错误、`130` 取消；模型答错不等于 CLI 失败。

## 历史输入边界

活动契约为 3；InferYard 不读取核心 v1/v2，也不接受 `origin=migrated` 运行。
旧运行迁移命令已移除，旧证据保留原件并交原项目处理。
`host-state migrate` 只维护主机锁与状态，不转换运行证据。
各产物支持集合及拒绝语义见[当前格式契约](contracts/inferyard-current-format.md#格式支持矩阵)，
不能仅按版本数字判断是否支持。

日常直接使用 `run`，它包含当次普通/流式探测。独立 `probe` 仅用于可选排障；
probe 只预算探测输入，0 次预热不预算 warmup，run/resume 只预算选中题。
预算仅接受 `token-budgets.v2.json`，旧位置数组拒绝；缺测不补造为 0。
预算快照与命令内复用见[数据契约](data-contract.md#命令内复用与预算快照)。


## 描述性比较与复用

新离线比较显示口径匹配的本次差值、条件差异和样本范围；受控结论资格另列。
性能差值仍须对应逐指标证据。报告仅接受 v7，比较仅接受 format4 / phase2.v3；
旧报告 v1–v6 和旧比较 format1–3 返回 `unsupported_format` / 2，不再核验或重建。
probe/diagnostic 对电源、profile、governor、EPP 缺测或差异只记录；资源安全停止不变。
正式执行的旧配置/计划仍要求原环境匹配。新配置可在 conditions、新实验可在 experiment
声明 `environment_admission = {definition = "environment-admission.v2", required_fields = []}`；
计划声明优先于配置声明，两者缺省时保留旧严格准入。诊断仅豁免预检，
显式冻结的 `safety.check_environment` 仍会检查原 conditions 并可能停止诊断运行。
空数组表示这些环境字段仅作为性能观测。操作者可把 `"ac_online"` 等列入 required_fields。

clean 同配置服务允许 rerun 和适用 handoff 复用，仍做当次普通/流式探测和预热。
冷启动实验设置 `execution.require_fresh_process = true`；更换服务时操作者须先退出旧进程。
工具不替用户关闭服务。孤立评分失败仍收集后续答案，含 unscorable 的结果保持 partial/code3。

### 逐题人工审核

审核记录绑定题目、答案、规则与政策哈希。已有匹配逐题记录时只重审受影响项，不能把失配的旧整包批准自动转成逐题批准。题包维护步骤见[贡献指南](../CONTRIBUTING.md#逐题人工审核)。

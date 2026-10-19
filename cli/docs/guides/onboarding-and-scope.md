# 首次上手、默认测评范围与工作区

本页是[使用指南](../usage.md) 的子文档，收录「首次上手与断点续接」「默认测评范围」「工作区与交付目录组织」的正文；内容不变，入口保留原节标题与锚点。

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
不构成候选资格，`config create` 仍终审；字段与退出码见[引擎核验契约](../cli-surface.md#引擎核验契约)。

按导出的 README 路线图由操作者启动服务，使用真实 PID/endpoint 绑定，再 run。
核心包完成后同样准备并运行 zh-svg-pelican，两个 run 均封存后在 reports/ 的新子目录
生成汇总并 verify。已有完整核心结果时只补跑缺少的鹈鹕；保留原件。
progress 只读输出进度，退出 0 不表示测评完成；查看 default_scope_complete 与 blocked_reason。
未完成 run 显示 resume；单次运行或固定时窗不支持续测时明确阻断，按既有规则创建新运行。
progress 不发送请求，也不启动服务；执行其 run/resume 命令前仍核对设备、预算与停止条件。
完整字段与退出码见[进度契约](../cli-surface.md#首次上手进度契约)。

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

按[安装指南](../installation.md#3-准备候选)为两个题包分别准备候选、绑定外部服务并运行，
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
格式见[容器契约](../contracts/run-evidence-layout.md)。不要把正式输出计数表述为
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

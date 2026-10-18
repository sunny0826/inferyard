# 命令与请求范围

本页是[CLI 接口](../cli-surface.md) 的子文档，收录「命令与请求范围」的正文；内容不变，入口保留原节标题与锚点。

| 用途 | 命令 | 请求范围 |
| --- | --- | --- |
| 安装与配置 | `init`、`runtime prepare`、`config assets/create/bind` | 本地准备；prepare/create 有界查询引擎版本，无模型请求 |
| 设备与目录 | `progress`、`device-check`、`memory-check`、`catalogue`、`engine check` | 本地设备、进程、文件或已安装目录数据；engine check 另有有界引擎版本查询子进程，无网络请求 |
| 实验准备 | `plan` | 离线预览或冻结实验 |
| 服务诊断与执行 | `service discover`、`probe`、`overhead`、`prepare-length`、`run`、`resume` | discover 对 loopback 监听端点发送只读 GET；其他诊断和执行可能发送模型请求 |
| 派生分析 | `repeat-summary`、`rescore`、`export`、`filter-candidates` | 离线读取已有证据或分析 |
| 报告与证据 | `report`、`compare`、`verify` | 离线生成新产物、核验 |
| 公开包 | `public package`、`public plan` | 本地打包和冻结复现计划 |
| 扩展协议 | `extension freeze`、`extension run`、`extension replay` | freeze / replay 离线；run 连接真实服务 |
| 同机引擎适配 | `engine-fit engines/plan/run/compare/verify` | engines 离线列出能力；plan 冻结目录或单 GGUF；run 按引擎范围连接已有本机服务 |

各动作的全部选项使用 `inferyard <命令> --help` 查询。元数据查询为 `--version`、`--versions`、`--schema KIND`，帮助和元数据不加载实时后端。stdout 输出 JSON，`device-check` 默认人类摘要，Agent 使用 `--json` / `--format json`；诊断走 stderr。退出码为 0 完成、2 输入/预检阻断、3 不完整或核验条件未通过、4 工具/证据错误、130 取消。

配置 `execution.warmup_count` 可填整数 0..3，省略时默认 3；0 跳过预热请求。
`probe --config`、`run --config` 与冻结计划加载接受相同范围，实际值随配置冻结并封存。
probe 仍发送普通和流式请求；正式题序与分母不随预热次数变化。
日常可直接 run，它包含当次双探测；独立 probe 为可选排障。
预算只覆盖当次输入，0 预热不预算 warmup，run/resume 只预算选中题；
当前快照读取规则见[预算契约](../data-contract.md#命令内复用与预算快照)。
离线核验显示固定安全原因；rescore 身份变化为 `rescore_scorer_identity_changed`、
退出 4，不返回 verified，也不证明保存记录被篡改。

`verify --path` 也可直接读取原始 run/batch 和冻结 plan 文件/目录，不要求先 report。
封存 run 核验通过为 0，包括原执行取消/不完整；execution_completeness/stop_reason
及顶层 completeness 保留原执行状态。未封存为 partial/3、sealed=false、verified=false；
坏 seal/坏 JSON 为 4，不能降格 partial。batch 逐 run 披露，计划尚未执行完不等于证据损坏。
engine-fit 中断后以 plan.json、checkpoint.json 和 requests.json 只读取证，不自动续跑。
新 report v8/comparison v4/engine-fit manifest v2 默认分开核验字节、来源与语义，
`verify --rerender` 可选重渲染；`engine-fit verify` 也接受 --rerender。
新普通报告/比较可用重复的 `--source-root OLD=NEW` 显式映射源根（相对参数按当前目录解析）；
不修改保存文件、不搜索磁盘。旧 report/comparison 版本按 ADR038 拒绝。详见[离线读取契约](../contracts/offline-reading-contract.md)。

`device-check` 在 macOS/Linux/Windows 只读检测；不指定模型且没有发现模型时返回 `inspected` / 0，显式模型或资源条件不满足仍退出 2。`--out` 要求新目录，保存 JSON，包括模型建议阻断的结果。格式、来源和实时平台边界见[设备检测](../device-check.md)。

`memory-check [--protect-pid PID] [--out NEW_DIR]` 在 macOS/Windows 只读枚举进程内存，默认 JSON，
Windows 分开输出 working set/private commit、账户关系及 session 缺测；EXE 分组不证明应用归属。
保护 PID 可重复；Linux 当前退出 2。权限/身份/截断等部分结果退出 3，输出写入失败退出 4。
`--schema memory_check` 导出新诊断结构；它不是正式 run 证据，也不自动关闭进程。
判断、缺测与来源规则见[应用内存诊断契约](../contracts/memory-check-contract.md)。

`engine-fit engines` 返回引擎 ID、平台、资产类型、空闲观察来源和执行前提，
MLX 项含随安装包交付的 `server_script` 路径，不启动引擎。`plan --model` 接受目录或单 GGUF；
`--engines` 可选 `vllm sglang llama-cpp mlx-lm lmstudio ollama`，其中 Ollama 的 `run`
明确阻断。LM Studio 的 `run` 应同时提供 `--lms-path FILE --models-root DIR`，
并以 `--endpoint-url http://127.0.0.1:PORT` 指定已有服务。LM Studio 的只读 `lms` 观察仅传显式
`--port`，使用本机 CLI 身份，不传 `--host` 或自动发现、启动服务。
其他引擎拒绝这两个 LM Studio 参数。`compare --runs A B --out NEW` 仍严格两两比较同计划、同资产、
同主机与同测量源码的不同引擎。完整前提和命令见[常用引擎指南](../engine-fit-common.md)。

`engine-fit plan --skip-temperature-stop REASON` 将操作者明确决定的临时温度覆盖
冻结为 plan.v3，对应 run.v4。原因非空且最多 1024 UTF-8 bytes，计划中温度上限为
`null`；温度仍采集，其他停止与身份检查照常执行。未传此选项时仍使用默认 85°C，
不改变旧计划；`engine-fit run` 不接受此开关。两引擎须按同一新计划和源码重新运行，
报告明示覆盖原因，不能与旧温停运行混比。示例见[临时覆盖用法](../engine-fit-common.md#显式临时跳过温度停止)。

`engine-fit plan --skip-memory-stop REASON` 显式暂停可用内存下限和内存缺测阻断，
冻结为 plan.v4/run.v5；与显式 `--min-free-memory-mib` 互斥，可与温度覆盖同时使用。
两个原因各自非空且最多 1024 UTF-8 bytes，内存/温度仍采集，缺测保留原因。
未传温度覆盖时仍执行 85°C 温停；磁盘、身份、时限、主机锁、dirty 和排空继续执行。
run 不接受覆盖开关，旧计划不变。见[内存覆盖用法](../engine-fit-common.md#显式临时跳过内存停止)。

Windows `engine-fit run` 仅接入单 GGUF llama.cpp，使用独立 run.v6 接受 plan.v2/v3/v4；
旧定义平台不变，其他 Windows 引擎在请求前拒绝。停止规则由计划冻结，不能在 run 时切换；原生身份和软件/真机边界见 [Windows 诊断](../../WINDOWS.md#windows-engine-fit-diagnostics)。

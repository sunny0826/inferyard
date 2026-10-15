# CLI 接口

入口为 `inferyard` 或 `python -m inferyard`。规范命令如下；例子见[使用指南](usage.md)，通路见[流程图](show-me-system-flow.html)。

## 命令与请求范围

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
当前快照读取规则见[预算契约](data-contract.md#命令内复用与预算快照)。
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
不修改保存文件、不搜索磁盘。旧 report/comparison 版本按 ADR038 拒绝。详见[离线读取契约](contracts/offline-reading-contract.md)。

`device-check` 在 macOS/Linux/Windows 只读检测；不指定模型且没有发现模型时返回 `inspected` / 0，显式模型或资源条件不满足仍退出 2。`--out` 要求新目录，保存 JSON，包括模型建议阻断的结果。格式、来源和实时平台边界见[设备检测](device-check.md)。

`memory-check [--protect-pid PID] [--out NEW_DIR]` 在 macOS/Windows 只读枚举进程内存，默认 JSON，
Windows 分开输出 working set/private commit、账户关系及 session 缺测；EXE 分组不证明应用归属。
保护 PID 可重复；Linux 当前退出 2。权限/身份/截断等部分结果退出 3，输出写入失败退出 4。
`--schema memory_check` 导出新诊断结构；它不是正式 run 证据，也不自动关闭进程。
判断、缺测与来源规则见[应用内存诊断契约](contracts/memory-check-contract.md)。

`engine-fit engines` 返回引擎 ID、平台、资产类型、空闲观察来源和执行前提，
MLX 项含随安装包交付的 `server_script` 路径，不启动引擎。`plan --model` 接受目录或单 GGUF；
`--engines` 可选 `vllm sglang llama-cpp mlx-lm lmstudio ollama`，其中 Ollama 的 `run`
明确阻断。LM Studio 的 `run` 应同时提供 `--lms-path FILE --models-root DIR`，
并以 `--endpoint-url http://127.0.0.1:PORT` 指定已有服务。LM Studio 的只读 `lms` 观察仅传显式
`--port`，使用本机 CLI 身份，不传 `--host` 或自动发现、启动服务。
其他引擎拒绝这两个 LM Studio 参数。`compare --runs A B --out NEW` 仍严格两两比较同计划、同资产、
同主机与同测量源码的不同引擎。完整前提和命令见[常用引擎指南](engine-fit-common.md)。

`engine-fit plan --skip-temperature-stop REASON` 将操作者明确决定的临时温度覆盖
冻结为 plan.v3，对应 run.v4。原因非空且最多 1024 UTF-8 bytes，计划中温度上限为
`null`；温度仍采集，其他停止与身份检查照常执行。未传此选项时仍使用默认 85°C，
不改变旧计划；`engine-fit run` 不接受此开关。两引擎须按同一新计划和源码重新运行，
报告明示覆盖原因，不能与旧温停运行混比。示例见[临时覆盖用法](engine-fit-common.md#显式临时跳过温度停止)。

`engine-fit plan --skip-memory-stop REASON` 显式暂停可用内存下限和内存缺测阻断，
冻结为 plan.v4/run.v5；与显式 `--min-free-memory-mib` 互斥，可与温度覆盖同时使用。
两个原因各自非空且最多 1024 UTF-8 bytes，内存/温度仍采集，缺测保留原因。
未传温度覆盖时仍执行 85°C 温停；磁盘、身份、时限、主机锁、dirty 和排空继续执行。
run 不接受覆盖开关，旧计划不变。见[内存覆盖用法](engine-fit-common.md#显式临时跳过内存停止)。

Windows `engine-fit run` 仅接入单 GGUF llama.cpp，使用独立 run.v6 接受 plan.v2/v3/v4；
旧定义平台不变，其他 Windows 引擎在请求前拒绝。停止规则由计划冻结，不能在 run 时切换；原生身份和软件/真机边界见 [Windows 诊断](../WINDOWS.md#windows-engine-fit-diagnostics)。

## 本机服务发现契约

`inferyard service discover` 不接受进程名过滤参数，枚举 TCP loopback 监听端口，
反查监听 PID、可执行文件基名及 `process_start_ticks`。Linux 读取 `/proc`，
macOS/Windows 使用已声明依赖 psutil 逐进程读取连接表；权限不足、进程消失或身份变化
记入 limitations，不把不完整扫描当作没有服务。身份时间读取复用 bind 的 identity 层。

探测范围是所有发现的 loopback 监听端点，**不限模型服务**。通配监听地址
`0.0.0.0`/`::` 分别通过 `127.0.0.1`/`::1` 探测；其他非 loopback 地址不探测。
每个不同 HTTP origin 依次发送 `GET /health` 与 `GET /v1/models`，HTTP 超时 2 秒，
不重试、不跟随重定向、不使用环境代理、不发送认证或 Cookie。
只输出状态码类别和模型 ID，不保存响应头、凭据、原始响应或进程 argv。
模型列表响应上限 1 MiB，重复 JSON 键、非有限数、非法 ID 列表均拒绝。
CLI 不启动、停止或绑定服务；`model_requests_sent` 恒为 0。

输出为 CommandResult，`command="service discover"`；`run_id`/`evidence_dir` 为 null。
`details={"services": [...], "model_requests_sent": 0}`，services 每项为：

| 字段 | 类型与含义 |
| --- | --- |
| `pid` | 正整数，监听进程 PID |
| `process_name` | 非空 string，可执行文件基名，仅作展示，不作过滤 |
| `process_start_ticks` | 正整数，与当前平台 bind identity 层同源的启动身份 |
| `endpoints` | 非空 array，每项含下表字段 |

| 端点字段 | 类型与含义 |
| --- | --- |
| `url` | 数字 loopback HTTP origin；IPv6 带方括号 |
| `health` | `/health` 的 `1xx`/`2xx`/`3xx`/`4xx`/`5xx`，传输失败为 null |
| `models` | 非空模型 ID string array（去重保序），失败或无模型为 null |
| `reason` | 下表固定值；两项探测均无缺测时为 null |
| `ambiguous_listener` | boolean；同端口观察到多个 PID 时为 true，跨 IPv4/IPv6 也不猜唯一归属 |

同一端口的所有可读监听进程全部列出，同 origin 的探测结果复用。
枚举与探测不是原子快照，不推断 IPv6 是否同时接受 IPv4；歧义标记 false 也不证明唯一性。
可将发现 PID 和 endpoint 显式传给 `config bind`，但 discover 不预判 bind 可行性；
bind 仍重新检查身份、资产和候选 argv，可能返回 `startup_arguments_mismatch` 等拒绝。

固定端点 reason（模型列表失败优先于 health 传输失败）：

| reason | 含义 |
| --- | --- |
| `models_timeout` | `/v1/models` 超时 |
| `models_unreachable` | `/v1/models` 连接、传输或 HTTP 协议失败 |
| `models_http_1xx` | `/v1/models` 返回 1xx |
| `models_http_3xx` | `/v1/models` 返回 3xx，不跟随重定向 |
| `models_http_4xx` | `/v1/models` 返回 4xx，包括需要认证 |
| `models_http_5xx` | `/v1/models` 返回 5xx |
| `models_invalid_response` | 非合法 JSON、缺少 data array 或有空白/非 string ID |
| `models_empty` | data 为空 array，没有可列出的模型 |
| `models_response_too_large` | 模型列表响应超过 1 MiB |
| `health_timeout` | 模型列表可用，但 `/health` 超时 |
| `health_unreachable` | 模型列表可用，但 `/health` 连接、传输或协议失败 |

枚举完整时退出 0、`status=discovered`、`completeness=complete`，包括 services 为空
或所有端点均无模型列表。部分枚举失败退出 3、`status=partial`、`completeness=incomplete`，
保留可用条目。工具错误退出 4、`status=error`、`completeness=incomplete`，保留已收集的
details。取消退出 130、`status=interrupted`、`completeness=incomplete`、details=null、
limitations=`cancelled`。

| limitations 固定值 | 含义 |
| --- | --- |
| `listener_table_incomplete` | 部分 Linux 连接表不可读或含坏行 |
| `listener_ownership_incomplete` | 权限、进程消失或未找到监听 socket 的可读属主 |
| `process_identity_unavailable` | 监听进程可执行名缺失或读取期间启动身份变化 |
| `listener_enumeration_failed` | 无法读取连接表或枚举进程（退出 4） |
| `service_discovery_unsupported_platform` | 平台不支持（退出 4） |
| `service_discovery_error` | 其他工具错误，不输出原始异常（退出 4） |
| `cancelled` | Ctrl-C 取消（退出 130） |

`inferyard --schema service_discovery` 导出此 CommandResult 结构；不改变核心证据契约版本。
回归使用模拟进程表和 HTTP 响应；Linux/macOS/Windows 真机权限、连接表覆盖及模型服务
发现效果须单独验证，模拟通过不代表原生平台验收。

## 引擎核验契约

`inferyard engine check` 对本地引擎做只读核验，**输入面按平台不对称**：macOS arm64
必填 `--engine FILE`，Windows x64 必填 `--runtime-receipt FILE`（`runtime prepare`
目录中的回执）；两参数互斥，平台错配（Darwin 给回执 / Windows 给引擎）、路径不存在
或不是文件等无效输入返回输入阻断（退出 2）。核验与 `config create` 同源：macOS 核验
引擎同目录 `*.dylib` 清单规则，Windows 按包内可信 profile 重验回执、清单与全部实际
文件。命令不创建目录、不写文件、不启动服务、不发送模型请求。

**执行面披露**：核验会以子进程执行引擎二进制 `ENGINE --version`（固定 30 秒超时，
匹配同一冻结版本模式，当前 `prism-b10743-adfffbe`）；macOS 新提取的 ad-hoc 签名
二进制首次 exec 校验可达约 12 秒，超时预算高于该值。除此版本查询外不执行引擎。

macOS 没有 device preflight 时不存在选定 mode（device-check 推荐可随模型大小在
metal/cpu 间翻转），因此不输出单一 mode，而输出**双 mode 各自结果** `modes`：
`cpu` 为版本与清单规则均通过，`metal` 为 cpu 通过且清单含 `libggml-metal.dylib`；
metal 最终是否采用仍由 `config create` 按 preflight 终审。Windows 的 mode 由回执
profile 自带（`cpu`/`cuda`），与候选 mode 的匹配同样留给 `config create`。**核验
通过不构成候选资格**：preflight、题包审核、模板、mode 匹配与写后复核仍由
`config create` 终审。

输出为 CommandResult，`command="engine check"`；`run_id`/`evidence_dir` 为 null。
核验完成（含不合格结论）退出 0、`status=checked`、`completeness=complete`，details 为：

| 字段 | 类型与含义 |
| --- | --- |
| `eligible` | boolean；通过同源核验为 true |
| `reason` | 下表固定值；eligible 为 true 时是 null |
| `reported_version` | 版本查询通过时引擎自报的首行输出（实测值）；版本不匹配或未执行查询时为 null |
| `expected_release` | 期望的冻结发布名常量 string（当前 `prism-b10743-adfffbe`），与实测无关，恒存在 |
| `modes` | macOS 合格时为 `{cpu: bool, metal: bool}` 双 mode 结果；Windows 或不合格为 null |
| `mode` | Windows 合格时为已验 profile 的 `cpu`/`cuda`；macOS 或不合格为 null |

`expected_release` 如实标注为期望常量，只有 `reported_version` 是引擎实测；
不使用 `engine_release`，避免常量冒充实测。

不合格 reason 固定值（均为退出 0 的核验结论）：

| reason | 含义 |
| --- | --- |
| `invalid_library_manifest` | macOS 引擎没有同目录 dylib、库链接悬空/越界或名称冲突 |
| `unsupported_engine_build_requires_adapter_validation` | 版本输出不匹配冻结版本模式 |
| `unsupported_runtime_profile` | Windows 回执声明未知 profile 名 |
| `runtime_receipt_invalid` | 回执为符号链接，或结构、字段、版本记录不合法 |
| `runtime_profile_mismatch` | 回执常量字段与包内可信 profile 不符 |
| `runtime_asset_mismatch` | 实际文件或清单与可信 profile 的哈希/大小不符 |

**退出码偏离声明**：eligible 与 ineligible 都是核验成功，均退出 0——这是对
device-check 家族「blocked→退出 2」语义的明示偏离，理由是本命令为查询式核验、
不阻断任何后续操作，终审在 `config create`；不合格结论只以 details.reason 呈现。
输入或平台错配退出 2、`status=blocked`、details=null，limitations 为
`invalid_input` 或 `unsupported_platform`。工具错误退出 4、`status=error`，
limitations 为 `engine_version_query_failed`（版本查询非零或超时）、
`package_resource_invalid`、`io_error` 或 `internal_error`，stderr 只输出脱敏诊断，
不输出原始异常或引擎输出原文；取消退出 130（`cancelled`）。

`inferyard --schema engine_check` 导出此 CommandResult 结构；不改变核心证据契约版本。
回归使用夹具文件与模拟版本输出；macOS 真机签名链校验耗时与 Windows 真实归档
核验效果须单独验证，模拟通过不代表原生平台验收。

## 首次上手进度契约

`inferyard progress --workspace DIR` 只读扫描显式工作区，不创建文件、不发送模型请求，
不查询现场硬件或服务。stdout 直接输出 `progress.v1`（不套普通 CommandResult envelope），
诊断走 stderr；有效且可读时退出 0，无论进度；工作区不存在、缺少 preparation.json、
产物不可读或核验失败时退出 2，绝不返回 3/4。失败输出沿用 blocked 错误 envelope。

`init --out DIR` 创建 onboarding 工作区：preparation.json 为 `community_init.v1`，
另含 README.md、bundles/、configs/、assets/、results/，以及空的 preflight/、candidates/、
bound/、reports/。包内 README 的 `{{WORKSPACE}}` 在导出时替换为本次绝对路径。
`device-check --out`、create/bind/report 的输出仍须为新目录，应使用这些目录下的新子目录。

`config create --workspace W --bundle zh-core --engine FILE --out W/candidates/core-1`
将裸名解析为 W/bundles/zh-core.json；完整题包路径仍接受。省略 --preflight 时选择
W/preflight/ 递归发现的 device-preflight.json 中修改时间最新者（同时间按路径排序）；
省略 --results 时冻结 W/results。显式参数优先，--out 仍必填。不提供 --workspace 时
--preflight、--results 仍必填，既有平台、互斥、硬件复核、身份和哈希校验保持原规则。

成功 JSON 字段固定如下：

```json
{
  "kind": "progress",
  "progress_version": 1,
  "workspace": "/absolute/workspace",
  "device_preflight": {"status": "missing", "path": null},
  "bundles": [{
    "bundle": "zh-core",
    "phase": "preflight_missing",
    "candidate_path": null,
    "bound_path": null,
    "runs": [],
    "next_action": "device_check",
    "next_command": "inferyard device-check --json --out /absolute/workspace/preflight/device-1",
    "blocked_reason": null
  }],
  "report": {"status": "missing", "path": null, "runs": []},
  "next_action": "device_check",
  "next_command": "inferyard device-check --json --out /absolute/workspace/preflight/device-1",
  "default_scope_complete": false
}
```

上例省略第二题包条目。bundles 按初始化范围列出，zh-core 优先；每包 phase 依次为
preflight_missing / candidate_missing / bound_missing / run_missing / run_incomplete /
run_complete，动作对应 device_check / prepare_candidate / bind / run / resume / none。
候选识别 candidates/*/preparation.json 的 config_candidate.v1，绑定识别
bound/*/preparation.json 的 config_binding.v1，并按候选路径关联。runs 从 results/ 的
run 或 batch/runs/run 读取，包含绝对 path、complete 和 sealed；完成与封存分别复用
现有证据语义，不把诊断 run 算入完成。单次运行和固定时窗不能 resume，仍报告 resume
阶段并给 blocked_reason，操作者按既有规则新建运行，不自动重试。

顶层 next_action 按 device_check → 各题包 prepare_candidate/bind/run → report → verify → done
排序；未完成 run 的顶层动作是 run，包内动作是 resume。report.status 为 missing / present /
verified，path 为报告目录绝对路径或 null，runs 为其来源绝对路径列表；verified 必须通过现有
报告字节、来源和语义核验。所有导出题包的 run_complete 且汇总报告 verified 时，
default_scope_complete 为 true；固定题包只表示其显式范围完成。

next_command 是包含绝对工作区路径的 shell 命令，路径按 shell 引号转义；
准备和绑定时尚未知的本地资产或进程参数使用必须显式设置的 INFERYARD_ENGINE /
INFERYARD_RUNTIME_RECEIPT / INFERYARD_SERVER_PID / INFERYARD_ENDPOINT 环境变量。
这些变量只传路径、PID 与端点，凭据仍仅由 endpoint.api_key_env 引用。progress 不取得
服务启动权限。重复准备时命令选择尚不存在的输出子目录，不覆盖旧产物。

## 社区安装准备接口

本节定义安装包的准备入口，操作示例见[安装指南](installation.md)。
首批为 Linux x64、Windows x64、macOS arm64；帮助、版本和资源查询仍延迟加载后端。
除下述 `model acquire` 与 `engine acquire` 的文件获取外，这些动作不发送 HTTP 或模型请求。
所有准备动作均不操作主机锁/dirty，不启动、终止或重启模型服务。
`runtime prepare` 和 `config create` 的引擎 `--version` 子进程是明确的例外副作用：
只查询版本，不传模型参数；失败不输出成功回执。`config create` 固定 30 秒超时
（macOS 新解压 ad-hoc 签名二进制的执行期校验偶发超过 10 秒）；`runtime prepare`
使用冻结 profile 的 `version_timeout_seconds`（当前 10 秒）。

### 参数、平台和产物

下表是完整的新命令参数集；所有 `--out NEW` 都是**不存在的新目录**，不是 TOML 文件名。
各路径参数按调用 cwd 解析，写出的配置和 stdout 路径为绝对路径；安装资源用包资源 API 读取，
不查找仓库根。所有命令支持 `--help`，不提供 `--force`、启动或跳过核验开关。

| 命令 | 必填参数 | 可选参数及默认值 | 平台和新目录布局 |
| --- | --- | --- | --- |
| `model acquire` | `--source URL --out NEW` | `--token-env NAME`：只接受环境变量名；`--timeout-seconds 1800` | 三平台；单 GGUF 文件的基名和 `source.json`，失败目录保留 |
| `engine acquire` | `--source URL --out NEW` | `--timeout-seconds 1800` | 三平台；单引擎文件的基名和 `engine-source.json`，失败目录保留 |
| `init` | `--out NEW` | `--bundle {zh-smoke,zh-core,zh-svg-pelican}`；省略时导出 `zh-core` 和 `zh-svg-pelican` | 三平台；`README.md`、`configs/{linux,macos,windows}.example.toml`、`bundles/NAME.json`、`assets/`、`results/`、`preflight/`、`candidates/`、`bound/`、`reports/`、`preparation.json` |
| `runtime prepare` | `--profile NAME --archive FILE --out NEW` | `--runtime-archive FILE`，仅 CUDA 必填，CPU 禁止 | 仅 Windows x64；`engine/`、CUDA 的 `runtime/`、`engine/engine-sha256.json`、`runtime-receipt.json` |
| `config assets` | `--model FILE --engine FILE --out NEW` | 无 | 仅 Linux x64；`chat-template.jinja`、`engine-sha256.json`、`assets.json` |
| `config create` | `--bundle FILE_OR_NAME --out NEW`；无 workspace 时另必填 `--preflight FILE --results DIR`；另按平台指定下述参数 | `--workspace DIR` 推导题包、preflight 和 results；`--port INT` 默认 48857，范围 1..65535；`--model-source FILE` 显式来源记录；无记录时 `--model-repo TEXT` 默认 `local`、`--model-revision TEXT` 默认所选模型 SHA256，显式文本须非空 | 仅 macOS arm64 / Windows x64；`candidate.toml`、`chat-template.jinja`、`preparation.json`；macOS 另有 `engine-sha256.json`，Windows 引用回执目录清单 |
| `config bind` | `--candidate FILE --pid INT --endpoint URL --out NEW` | 无；PID 应为正整数 | 三平台及既有可绑定适配器；`config.toml`、`preparation.json` |

`runtime prepare` 的 NAME 只有 `prism-b10743-adfffbe-win-cpu-x64` 和
`prism-b10743-adfffbe-win-cuda-12.4-x64`，可信归档、清单和回执字段见
[Windows 准备契约](contracts/windows-prism-batch-contract.md#社区安装的离线运行时准备)。
`config create` 在 macOS 必填 `--engine FILE` 且禁止 `--runtime-receipt`；
Windows 必填 `--runtime-receipt FILE` 且禁止 `--engine`。两者不能同时提供。
不提供跨平台模拟生成参数；Linux 使用 `config assets` 和示例手工冻结配置。
`--model-source` 只读取显式传入的 `remote_model_source.v1/v2/v3` 记录，不按目录查找旧记录。
记录的大小和 SHA256 必须等于当次所选文件；候选生成复用当次文件哈希，不重复扫描模型。
省略 repo/revision 声明时使用记录中的 repository/revision，显式声明冲突则返回
`model_source_metadata_mismatch` / 2。缺少转换和量化证据时不生成 `model_lineage`；
普通比较仍遵循[证据血缘规则](data-contract.md#证据血缘与比较结论)。
`--results` 只冻结结果根并检查所在卷，不创建运行或覆盖结果；它可为现有目录，
每次实际运行仍按原规则创建新的 run。它不能位于安装包目录或本次 `--out` 内。

### 各动作的冻结行为

`model acquire` 只接受 `https://huggingface.co/OWNER/REPO/blob/REVISION/PATH`
或 `https://modelscope.cn/OWNER/REPO/blob/REVISION/PATH`，revision 必须是 40 位十六进制。
PATH 可包含子目录，但只能是一个 `.gguf` 文件；输出使用其文件基名。
可变 main/master、凭据、query、fragment、非 ASCII 和路径穿越均拒绝。
`--token-env` 只接受环境变量名；token 值放在初始请求的 `Authorization` 头中，
跟随重定向时由 httpx 在跨 origin 跳转自动剥离，不转发给 CDN；
token、请求头和响应正文不输出或持久化。
下载为进程内流式 GET：正文写入 `文件名.part`，flush/fsync 后 rename 发布，
`source.json` 同样先写 `.part` 再发布；失败清理 `.part` 并保留目录，不能重用。
`--timeout-seconds` 默认 1800，作用于各网络操作；必须为正有限数。
没有总期限、自动重试或续传；慢链路应在开始前显式增加该值。
`source.json` 保存 `remote_model_source.v3`、parser、bytes、sha256
及平台、仓库、固定 revision、仓库内路径和获取时间；bytes/sha256 为实测值。
v1/v2 记录仍严格只读，不补写、不转换；字段见[模型获取契约](contracts/model-acquisition.md)。
成功为 `prepared` / `complete` / 0，details 含 `ready_to_run=false`。
失败原因固定为 `invalid_model_source`、`model_acquire_incomplete`、
`output_exists`（退出 2）、`io_error`（退出 4）、`cancelled`（退出 130）。
stderr 只输出固定原因，不输出原始异常或签名 URL。
模型文件下载完成不证明可加载、服务就绪或测评完成。`run` 仍不接收模型 URL。

获取客户端使用 `trust_env=False`，不使用 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`
或系统代理。依赖本机代理的网络需要能接管直接连接的 TUN/全局路由，
仅开启系统代理不足以让获取经过代理。操作者须确认仓库和 CDN 均可达。

`engine acquire` 接受任何 `https://HOST/PATH` 链接（无 query、fragment、
userinfo 或端口，路径分段无穿越），基名即输出文件名；不限 host 与扩展名，
没有 token 选项。下载语义与 `model acquire` 相同：进程内流式 GET、
`Accept-Encoding: identity`、`.part` 后 rename 发布、失败保留目录。
产物为文件基名与 `engine-source.json`（`remote_engine_source.v1`：
parser、source_url、bytes、sha256、retrieved_at，bytes/sha256 为实测值）。
命令不解压、不装配、不启动服务、不核验 profile；引擎身份仍由
`config create` 的 `binary_sha256` 与 `runtime_library_manifest` 冻结，
Windows 归档继续经 `runtime prepare --archive` 核验。失败原因固定为
`invalid_engine_source`、`engine_acquire_incomplete`、`output_exists`
（退出 2）、`io_error`（退出 4）、`cancelled`（退出 130）；规则细节见
[引擎获取契约](contracts/engine-acquisition.md)。

```bash
inferyard model acquire --source 'https://huggingface.co/OWNER/REPO/blob/COMMIT40/sub/model.gguf' --out bench-ops/models/new
inferyard device-check --model bench-ops/models/new/model.gguf --out bench-ops/preflight/device-1 --json
inferyard config create --preflight bench-ops/preflight/device-1/device-preflight.json --engine /path/to/llama-server --bundle bench-ops/bundles/zh-smoke.json --results bench-work/results --out bench-ops/candidate --model-source bench-ops/models/new/source.json
```

`COMMIT40` 是占位符，必须替换为实际固定 revision；示例候选命令适用于 macOS，
Windows 改用 `--runtime-receipt FILE`，Linux 继续手工配置。服务由操作者另外启动。
慢链路可在获取命令后追加 `--timeout-seconds 14400`，它同时是连接和读取的期限。

1. `init` 导出三个平台的去设备化占位示例。省略 `--bundle` 时导出 `zh-core` 和
   `zh-svg-pelican`；显式 `--bundle` 只导出一个固定题包。示例明确未绑定且不能直接运行；
   不带真实 PID、设备标识、历史绝对路径、凭据或预算资格。题包来自
   `data/community/bundles/`，逐字节复制根 `bundles/` 的权威已审版本，保留内嵌审核信息。
   不更改题目或审核哈希，也不自动选修订题包。`assets/` 和 `results/` 初始为空。
   导出两个题包不触发执行；`run --config` 每次只执行配置中的一个题包，不自动连续运行两包。
2. `config assets` 只读现有单 GGUF、引擎及引擎同目录的 `*.so` / `*.so.*`，
   使用现有 GGUF 解析器与 SHA256 能力，不执行引擎、`ldd` 或版本命令。
   模板按 `tokenizer.chat_template`、再按 `tokenizer.chat_template.default` 选取；
   应是非空字符串。写入恰好 `template.encode("utf-8")`，不改换行、不加 BOM 或末尾换行。
   写后文件 SHA256 应等于该字节串 SHA256；模型和引擎不复制。
   清单为 `{文件基名: SHA256}`，包含引擎和所发现的同目录共享库；库链接只允许解析到
   同引擎目录内的普通文件，按原基名记录，拒绝悬空/越界链接和名称冲突。
   清单是本地资产清点，不证明服务实际加载集合；后续 run 的文件/进程核验保持原规则。
   不生成自动预算或完整 Linux 候选，不借用历史模板。
3. `config create` 接受 `device-check --out` 产生的 `device-preflight.json`，
   或 `device-check --json` 的完整 CommandResult（只从其 `details` 取同一预检对象）。
   应是当前本机平台、`recommendation.status = recommended` 且 selected 非空；
   所选模型、mode、线程、上下文、GPU 索引及内存/磁盘阈值由该推荐冻结，不临时降低预算。
   macOS 保留现有实时复查：重新检查模型、资源、mode 和 AC 电源，变化则要求重新 device-check；
   Windows 保留既有推荐生成规则并严格匹配 CPU/CUDA profile，不把估算称为实跑验证。
   Windows 先按可信 profile 重验回执、清单及实际文件，再执行版本查询；macOS 查询所选引擎版本。
   两平台均只接受现有 `prism-b10743-adfffbe` 版本匹配规则；模板按上一项原字节规则写出。
   `--bundle` 加载并核验原人工审核，配置使用该文件的 version；显式 `--results` 替代仓库路径。
   生成参数沿用对应平台原生成器，不借此统一或改变测量配方。PID/启动时间为未绑定占位，
   `ready_to_run=false`；调用 load_config 及 TOML round-trip 校验后才完成准备。
4. `config bind` 复用原绑定脚本：加载候选、校验本机无凭据 endpoint，读 PID 启动时间及实际 argv，
   再读启动时间以拒绝进程更替；Linux 为 `/proc`、macOS 为原生身份、Windows 为完整 FILETIME。
   只把候选中已有 `--host` / `--port` 值替换为显式 endpoint，分离值与 `--flag=VALUE`
   形式均保留原写法；随后将期望 argv 与实际 argv
   按原脱敏规则严格比较；不能用实际 argv 覆盖其余期望参数。写入 endpoint/PID/start ticks，
   保留其余配置和资产声明，递归 TOML 序列化后复核；不触发服务请求或宣称实时预检已通过。
   后续直接 `run`，由 run 完成当次身份、资产和普通/流式探测；独立 `probe` 仍是可选排障。

### JSON 与共享类型

stdout 恰好一个 UTF-8 JSON 对象，使用现有 CommandResult 外层。`command` 分别为
`init`、`runtime prepare`、`config assets`、`config create`、`config bind`（包含空格）。
成功退出 0，`status="prepared"`、`completeness="complete"`、`run_id=null`、
`evidence_dir=null`、`limitations=[]`。complete 只表示准备产物完整，不是测评完成。
`details` 必含 `out`（绝对目录）、`model_requests_sent=0`、`ready_to_run=false`，
并按命令包含下表字段；文件路径为字符串，SHA256 为 64 位小写十六进制，bytes 为非负整数。

| command | details 的其余必填字段 |
| --- | --- |
| `init` | `bundles`（按导出顺序，每项含 `bundle`、`bundle_path`、`bundle_sha256`）；顶层 `bundle`、`bundle_path`、`bundle_sha256` 仍指向第一个题包；`readme`、`configs`（键 linux/macos/windows 对应绝对路径）、`preparation` |
| `runtime prepare` | `profile`、`mode`（cpu/cuda）、`engine`、`engine_sha256`、`runtime_library_manifest`、`receipt` |
| `config assets` | `model`、`engine`、`template`（各为 `{path, bytes, sha256}`）、`runtime_library_manifest`、`libraries`（基名到 SHA256）、`assets` |
| `config create` | `candidate`、`template`、`runtime_library_manifest`、`device_preflight`、`bundle`、`results`、`model`（路径）、`model_sha256`、`engine_sha256`、`mode`（cpu/cuda/metal）、`preparation` |
| `config bind` | `config`、`candidate`、`pid`、`start_ticks`、`endpoint`、`preparation` |

`init`、`config assets/create/bind` 的 JSON 侧文件使用
`kind=community_init.v1/config_assets.v1/config_candidate.v1/config_binding.v1` 和
`schema_version=3`；内容为 `{kind, schema_version, ...details}`。Windows 回执有独立字段契约。
准备侧文件不注册为 run/报告，不由 `verify --path` 猜测为封存证据。

示例（路径仅示意）：

```json
{
  "command": "config bind",
  "status": "prepared",
  "completeness": "complete",
  "run_id": null,
  "evidence_dir": null,
  "limitations": [],
  "details": {
    "out": "/work/bound-new",
    "model_requests_sent": 0,
    "ready_to_run": false,
    "config": "/work/bound-new/config.toml",
    "candidate": "/work/candidate-new/candidate.toml",
    "pid": 1234,
    "start_ticks": 5678,
    "endpoint": "http://127.0.0.1:48857",
    "preparation": "/work/bound-new/preparation.json"
  }
}
```

请求仍用 `application/types.py` 的 CommandRequest，结果仍为 CommandResult，不另建 CLI DTO。
新增可空字段含 `workspace: Path`，其余字段冻结为：`community_bundle: str`、`runtime_profile: str`、`archive: Path`、
`runtime_archive: Path`、`engine_path: Path`、`runtime_receipt: Path`、`preflight: Path`、
`bundle_path: Path`、`candidate: Path`、`model_repo: str`、`model_revision: str`、`port: int`。
复用 `out`、`model_path`、`output_root`（--results）、`server_pid`（--pid）、
`endpoint_url`（--endpoint）；默认值在新命令构造请求时填入，不影响其他命令。
CLI/application 只负责参数、分派、结果和错误映射；资源、资产、候选、绑定放在 config/platforms。

### 新目录、失败与退出码

输入校验通过后排他创建新目录；已存在的文件、空目录、符号链接（包括悬空链接）一律拒绝。
父目录应已存在且可写，不悄悄创建多级父目录；输入不能位于本次新输出内部。
写文件使用排他创建，拒绝输出树中的链接/Windows reparse point，不覆盖并发创建的对象。
检查失败不动输入或旧输出；创建目录之后的 IO 失败/取消可保留本次不完整目录，
不写成功回执，重试应换新目录。元数据最后写出；完成前复核文件、配置和模板字节。

| 退出码 / status | limitations 固定原因与边界 |
| --- | --- |
| 2 / blocked | `invalid_input`：缺参数、未知参数、互斥参数、非法 JSON/TOML/结构/字段类型、非法 PID/port/endpoint；`unsupported_platform`：动作不支持当前平台/架构；`output_exists`：输出已存在或发生排他创建冲突 |
| 2 / blocked | `unsupported_runtime_profile`、`runtime_archive_required`、`runtime_archive_forbidden`；`archive_identity_mismatch`、`unsafe_archive_entry`、`runtime_library_collision`；`runtime_receipt_invalid`、`runtime_profile_mismatch`、`runtime_asset_mismatch`：输入回执/清单不合法或与包内可信 profile/实际资产不符 |
| 2 / blocked | `device_preflight_blocked`、`device_selection_changed_repeat_device_check`、`ac_power_required`；`model_chat_template_required`、`invalid_library_manifest`、`unsupported_engine_build_requires_adapter_validation`；`service_identity_changed`、`startup_arguments_mismatch`；其他已有本地配置/身份阻断沿原安全原因映射，不退化为成功 |
| 4 / error | `io_error`：文件缺失、权限、磁盘/读写错误；`engine_version_query_failed`：版本查询非零或超时；`package_resource_invalid`：包内模板/profile/题包副本缺失或自检失败；`serialization_mismatch`：工具写出的 TOML/模板复核不符；其余未预期故障为 `internal_error` |
| 130 / interrupted | `cancelled`；只结束本命令自有版本查询子进程，不操作操作者的服务 |

失败外层仍有 `command`，`completeness="incomplete"`、`run_id=null`、`evidence_dir=null`、
`details=null`，limitations 为对应原因数组；stderr 只输出脱敏诊断，不泄露凭据或原始进程命令行。
`config bind` 读取进程身份/argv 时，保留代码定义的 `service_process_unavailable` 和
`service_identity_unreadable` 两个固定原因；仍退出 2 / blocked，不创建输出目录。
未知预检异常或包含额外文本的原因继续走原安全映射，不回显异常原文。
JSON 重复键、NaN/Infinity、bool 冒充整数均拒绝。新准备动作不使用退出 3；
已有执行及核验入口的退出语义不改变。

## KVMem / NInfer 正式文本入口

配置 `engine.adapter = "kvmem"` 绑定 GGUF，`"ninfer"` 绑定原生 NInfer v3 容器。
`probe --config`、离线 `plan --experiment`、`run --config` / `run --plan`、
`report --runs`、`verify --path REPORT` 使用正式 Journal 和评分通路。
Windows 首版为单文件、串行、fixed quality、无缓存文本；原始 run 可直接 verify，
也可通过报告产物核验。跨平台可离线冻结 Windows 计划、生成报告和核验。

示例：[KVMem](../configs/windows-kvmem.example.toml)、[NInfer](../configs/windows-ninfer.example.toml)。
示例身份均为占位，须绑定真实文件/模板/库、argv/cwd、PID/FILETIME 和外部已启动服务。
默认 `engine.observation_mode = "auto"`：生成使用 `/v1/chat/completions`，流式请求启用
`include_usage`。probe 输出端点能力矩阵；`/lab/v1` 缺失时使用实际探测成功的原生信号，
缺失项记 `null` 和原因，不阻断普通测评。显式设置 `"lab_required"` 才要求完整 lab 能力。
manifest 与离线报告披露观测路径；原生路径不授予引擎内部全服务排空证明，进程级证据单列。
精确模板预算、实际生效参数与引擎排空无法观测时保持缺测。异常请求保留 dirty 并要求绑定恢复。
本次直接实现 M2，engine-fit 现有诊断范围不变。
字段和真实二进制差距见[适配契约](contracts/windows-ninfer-adaptation-contract.md)，
当前验证进度见[backlog](backlog.md)。

## 规范语法与兼容入口

| 规范语法                                              | 已删除的兼容语法                                 | 语义                               |
| ----------------------------------------------------- | ------------------------------------------------ | ---------------------------------- |
| `verify --path DIR`                                   | 八个 `*-check --run DIR`                         | 自动识别产物，调用专用验证器       |
| `verify --path PUBLIC --config CFG`                   | `public-config-check --run PUBLIC --config CFG`  | 核验公开包与声明配置               |
| `verify --path PUBLIC --source-run RUN`               | `public-check --run PUBLIC --from-run RUN`       | 核验公开源投影                     |
| `verify --path OVERHEAD --target-run RUN`             | `overhead-check --run OVERHEAD --target-run RUN` | 核验开销目标绑定与阈值             |
| `probe --config CFG`                                  | `check --config CFG`                             | 发送普通/流式探测，保存诊断证据    |
| `plan --experiment FILE`                              | `plan --config FILE`                             | 输入为实验定义                     |
| `report --runs RUN [RUN ...]`                         | `report --run RUN`                               | 一至多个运行目录                   |
| `run --rerun-from RUN`                                | `run --from-run RUN`                             | 整体新建一次重跑，要求真实服务绑定 |
| `public package --run RUN --out DIR`                  | `public-package` 的相同参数                      | 生成脱敏公开包                     |
| `public plan --run PUBLIC --config CFG --out DIR`     | `public-plan` 的相同参数                         | 冻结复现计划                       |
| `extension freeze --spec FILE --config CFG --out DIR` | `extension-freeze`                               | 输入原始扩展规范                   |
| `extension run --plan FILE --config CFG --out DIR`    | `extension-run --spec FILE`                      | 输入冻结扩展计划                   |
| `extension replay --packet FILE --out DIR`            | `extension-replay --spec FILE`                   | 离线导入合成 packet                |

表中的旧命令名和参数别名已删除，调用返回输入阻断；只接受规范语法。扩展 run/replay 分别使用 `--plan` / `--packet`；freeze 继续使用 `--spec`。`resume --from-run` 只续测未执行部分，与整体重跑不同。

报告、评分、导出、公开包及冻结计划的 `--out` 要求新产物目录；新批量运行使用不存在或为空的 `--output-root`，续测绑定同一批次根目录。扩展 run/replay 的 `--out` 是输出根目录，其下创建新的封存子目录，stdout 的 `evidence_dir` 才是可核验的具体目录。

已删除的命令也不再提供帮助。八个 `*-check` 为 `overhead-check`、`repeat-check`、`rescore-check`、`export-check`、`public-check`、`report-check`、`compare-check`、`extension-check`；`public-config-check` 同样由表中的 `verify` 语法取代。

## verify 的核验范围

`engine-fit verify` 专用于[引擎适配诊断](engine-fit-common.md)的 run/比较产物；
普通 `verify` 支持下面八类派生产物，以及原始 run/batch、冻结 plan 和 engine-fit 产物。
规划、执行、比较的全部新选项见 `engine-fit <动作> --help`。

八类产物为开销对照（overhead）、重复分析（repeat）、重评分（rescore）、指标导出（export）、公开包（public）、报告（report）、比较（comparison）和扩展包（extension）。

- 使用特有文件及必要元数据识别；损坏、无法识别或类型冲突均拒绝，不回退到只核验 manifest。
- comparison 与 report 共存时应核验两者；extension 内嵌报告由 packet 验证器负责。
- 原始 run/batch、冻结 plan 由原读取器核验；单独 analysis 仍不属于此入口。
- `--config` 和 `--source-run` 仅用于 public，可同时提供；`--target-run` 仅用于 overhead。不适用选项返回输入阻断。
- 配置匹配只核对本地声明，不要求当前端点/PID，也不证明就绪；`ready_to_run` 仍为 false。配置不匹配、开销阈值或绑定未通过时保持退出码 3。
- 结果保留专用验证器的状态、完整性、限制和退出码，details 附 `artifact_type`；组合比较包含 comparison 与 report 两项结果。

核验只读，不发送模型请求，不授予当前测量或性能比较资格。报告、比较、重评分、导出和两类开销协议有不同输入与来源规则，不能因共享入口而合并语义。

`report` 的[离线 HTML](reports.md)包含模型 / 机器档案、生成参数核验、分类成绩、独立来源资源曲线与逐题检索。新产物使用报告格式 v8，核心 schema 仍为 3；仅接受 v8；v1–v7 返回 unsupported_format/2，旧数据回原项目处理。

### 社区准备 details 的字段类型补充

上述冻结字段名不变。所有绝对文件/目录路径（`out`、`bundle_path`、`readme`、`preparation`、
`runtime_library_manifest`、`receipt`、`assets`、`candidate`、`config`、`device_preflight`、
`results`，以及 create 的 `model`/`template`、runtime prepare 的 `engine`）均为非空 string。
`bundles` 在 init 为非空 array；省略 `--bundle` 时按 `zh-core`、`zh-svg-pelican` 顺序含两项，
显式选择时含一项。每项为含 `bundle`、`bundle_path`、`bundle_sha256` 的 object，
字段类型与顶层同名字段一致。顶层 `bundle` 在 init 为第一项的题包名称 string，
在 create 为题包绝对路径 string；`profile` 为固定名称 string。
`mode` 为表内枚举 string。`configs` 为恰含 linux/macos/windows 三键的 object，值为绝对路径 string。
`libraries` 为非空 object，键为文件基名 string、值为 SHA256 string；所有 `*_sha256`
均为 64 位小写十六进制 string。assets 的 `model`/`engine`/`template` 为恰含
`path: string`、`bytes: integer >= 0`、`sha256: string` 的 object。
`pid`、`start_ticks` 为正 integer，`model_requests_sent` 为 integer 0，`ready_to_run` 为 boolean false；
整数不接受 boolean。`endpoint` 为按现有规则验证的本机无凭据 origin string。

Windows candidate 引用 runtime prepare 目录中的实际引擎和清单，不复制这些资产。
移动或删除该目录会使既有候选路径失效；应在确定的新资产位置重新准备候选并绑定服务，
不能只移动候选文件或修改回执来继承原服务身份。单独移动完整 runtime 目录后生成**新候选**时，
回执相对路径仍须通过包内 profile 和实际文件复核。

可识别的不支持格式返回 unsupported_format/2，details 含 artifact、saved_version、supported_versions。
坏 JSON、布尔版本、坏 seal/hash 或当前格式语义冲突为证据错误/4；配置输入错误为 2。
支持矩阵与主机状态规则见[当前格式契约](contracts/inferyard-current-format.md)。

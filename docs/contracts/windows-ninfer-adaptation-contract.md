# Windows KVMem / NInfer 适配契约

正式 CLI 的 `kvmem` / `ninfer` 使用活动 v3；engine-fit 尚未接入。
字段级解析与原生身份接口见[lab 观测接口](windows-ninfer-adaptation-t0.md)，来源解释遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)。

仅显式 `engine.observation_mode = "lab_required"` 时，要求引擎提供完整
`lab_observation.v1` 和下述分词/参数接口；缺接口时 `probe` / `run` 返回契约错误。
已发布的 KVMem v0.16.0-rc3 / NInfer v2026.09.27b 在当前侦察中 lab 接口为 404；
默认 `auto` 使用 OpenAI 生成并降级披露原生信号，详见本文末的发行版能力探测。

## 职责与共享接口

| 层                        | 责任与边界                                                                 |
| ------------------------- | -------------------------------------------------------------------------- |
| application/types.py、CLI | 唯一请求/结果类型，参数检查和退出码；帮助/能力查询不加载实时后端           |
| config/contracts          | 引擎与资产组合、版本、冻结参数和来源校验；拒绝未知或歧义字段               |
| platforms                 | 同账户 PID、完整 FILETIME、exe/DLL、argv/cwd、模型文件及唯一监听；缺测不伪造 |
| adapters                  | 发行专属 HTTP、普通 JSON/SSE、模板计数、有效参数、取消和 idle 观察          |
| runtime                   | HostLock、Journal、dirty、五终态、固定截止时间、排空与恢复；不管理服务     |
| evidence/reporting        | 原始字节封存、来源关联、离线校验、评分及报告；不在读取时访问端点           |

核心适配器沿用 `close`、`verify_properties`、`token_budget`、`verify_effective`、
`generate`、`wait_idle` 的职责，通过共享接口抽离 Prism 请求构造、模板计数和终态类型。
发行特殊路径只在适配器内部映射，不在公共 Runtime 猜测 `/props`、`/slots` 或模板语法。
`generate` 应在 `before_send` 持久化成功后发请求，并提供已开始但未完成时的 `last_state`。
engine-fit 诊断有独立契约，不通过本适配器扩大平台或引擎范围。

## 版本、身份与参数

核心配置 `engine.adapter` 新增 `kvmem|ninfer`；旧 Prism / llama.cpp 的 v3 配置
和证据仍可读，缺少新字段不补造。新增字段仅在 lab 引擎上必需：

| 字段 | 规则 |
| --- | --- |
| `model.kind` / `model.bytes` | `kvmem→gguf`、`ninfer→ninfer`；正整数大小与本机完整哈希一致 |
| `engine.working_directory` | 绝对 Windows 目录，绑定 cwd SHA；argv0 为绝对引擎路径，startup_args 精确匹配实际参数 |
| `engine.asset_manifest` | lab 观测接口 的 path/role/bytes/sha256 清单，1–256 项、合计≤64 GiB、绝对路径、大小写去重 |
| `model.component_ledger_path` / `component_ledger_sha256` | 原生 NInfer 必需，绑定容器 artifact ID、模型 SHA 和组件目录 SHA |

清单中恰一个 model、engine、template；原生还恰一个 component_ledger，其他均为 library。
库清单应与 `runtime_library_manifest` 的文件名→SHA 精确一致，声明库须在进程中实际加载，
引擎目录里未绑定的已加载 DLL 拒绝。进程使用 lab 观测接口 同句柄 FILETIME、argv/cwd/exe、
同账户和唯一 loopback listener 核验；每次请求前重验 native file stamp。未绑定的
CUDA 环境覆盖与声明不符时拒绝；身份面的白名单只含 `CUDA_VISIBLE_DEVICES`、
`CUDA_CACHE_DISABLE`。其他变量不冻结、不据此失败，也不能声称已核验其影响。
清单哈希准备预算为 `180 + ceil(总字节数 / 32 MiB)` 秒，最大 64 GiB 对应 2228 秒；
这是有界准备预算。整个清单共用一个固定单调截止时间，不按文件或读块重置。
大小/哈希、同句柄身份、读后变化与超时拒绝规则保持不变。

`library` 声明的是本运行使用并实际加载的库，不是发行包目录的全部 DLL 清单。
声明库未映射报 `lab_library_not_loaded`；引擎目录内映射的 DLL 未声明报
`lab_unbound_engine_library`。两项均在预检与逐请求身份重验时检查。
例如 `cudart64_13.dll` 在磁盘上存在但未映射，不能仅据此判定静态链接或不需要该库。
操作者须核对当前构建的链接/依赖材料和实际运行映射；若能证明该文件是未使用的发行附件，
在新配置中同步调整 `asset_manifest` 的 library 项与 `runtime_library_manifest`，
重新核验保留库的完整 SHA、实际加载与配置/计划绑定，保留旧声明及拒绝记录。
若是实际依赖的延迟加载，当前契约仍要求首请求前可证明已加载；使用满足该条件的构建或
启动方式，不能在核验中把未映射库视为已加载。此次映射缺失本身不证明以上任一种原因。

首版只接受 Windows CPU/CUDA、单文件 GGUF v2/v3 或单文件 NInfer v3、
文本串行、reasoning off、seed supported、cache disabled。冻结计划限定 fixed quality；
Prism prefix reuse、strict fixed output、分片和持续/性能专项仍拒绝。

`execution.warmup_count` 接受整数 0..3，默认仍为 3；0 表示不预热。
冻结配置如实记录所选值，plan 的配置引用与 manifest 的文件哈希继续绑定实际字节。
两次协议 probe、基线采集、身份和停止规则保持原条件；历史值 3 的 v3 plan/run 保持可读。

原生 framing 按固定 [JGamboa 源码 c6adc56 的容器定义](https://github.com/JGamboa/ninfer-4090-windows/blob/c6adc56de0a43aa5f3c2a78ccb2073d2398016f4/docs/maintainer/artifact-container.md)
核对完整 magic、≤16 MiB JSON 目录、文件长度、对象范围/顺序/ID 和组件账本。
架构特有的 tensor/binding 解释由引擎 loader 负责。账本恰含
`artifact_id`（32 hex）、`model_sha256`、`components_sha256`；后者是 `components`
使用本产品 `json_bytes`（排序键、紧凑逗号/冒号、UTF-8、末尾 LF）的 SHA256。

模型容器检查包括 magic/version、目录边界、分片关联、artifact ID 和组件账本；文件哈希
不证明 GPU 驻留。只接受固定本机单模型、文本串行、显式关闭 vision/spec/ngram 等附加路由。
模型、模板、分词器、EXE/DLL、完整源码/补丁/构建配方 SHA 独立保存。
不能从 alias 或服务自报版本推断这些哈希，也不能声称补丁包等于原发布包。

每项参数保存 `requested`、`effective`、证据来源及缺测原因。模板渲染后的精确输入 token 数与输出上限之和应不超出模型上下文窗口；
字符数保守上界不替代正式 `token_budget` 接口。
模板、采样器、seed/stop、thinking、缓存、KV/Host KV/Host State 以及并发均需实际核验。
不支持的参数在发送前阻断，不能静默丢弃；环境覆盖只核验，敏感值不落盘。

## 候选引擎端观测协议

完整 lab 能力要求 `lab_observation.v1`；只读路由
`GET /lab/v1/identity`、`GET /lab/v1/lifecycle`、`GET /lab/v1/requests/ID`，
以及 `POST /lab/v1/requests/ID/cancel`。精确字段见 lab 观测接口。
这些路由当前未获发布二进制证实；仅显式 `lab_required` 时缺失即阻断。
默认降级仅记录实际探测的原生信号，不猜测引擎内部生命周期。

身份快照应给出 `server_instance_id`、协议/构建标识、已加载资产与模板身份、分词
能力和有效参数。声明与本机文件、完整进程身份共同核验；引擎声明不是独立来源证明。
客户端生成 `request_id`，服务校验唯一性并关联 HTTP、SSE 和 JSONL；`server_instance_id`
变化使旧请求查询失效。客户端提供的 ID 不能被当作认证或绕过路径校验。

生命周期快照定义互斥阶段：`accepted_waiting`、`preparing`、`prefilling`、`decoding`、
`output_draining`、`releasing`；`active_total` 为各阶段之和。请求在任何排队/准备工作之前
纳入账本，在消费者/响应结束及上下文/GPU 释放之后退出。快照在同一同步边界读取，
含递增 `snapshot_seq`、`poisoned` 及 request 列表，不能用跨锁求和拼成全局 idle。
观察路由不进入生成账本；所有普通/流式生成入口及内部排队都应覆盖。

idle 成立需 `active_total=0`、`poisoned=false`、服务身份稳定，并在冻结排空期限内确认。
单次零快照不证明未来无请求；正式执行继续依赖独占本机服务、HostLock 及每次重验。
任一生命周期阶段或入口未覆盖即不提供 idle 能力。

取消是幂等的请求专属操作：accepted 不等于释放完成；应持续查询到已释放终态或
等待全服务 idle。不存在的 ID、已结束的 ID 和旧实例 ID 有固定不同结果，不能取消其他请求。
断连也驱动同一 cancel 标志；prepare、等待、prefill、decode、输出尾部逐阶段验证。
取消期限与生成总期限分别冻结；超时/未知保持 dirty、停止余项，不清除状态掩盖问题。
观察接口使用与服务一致的认证、loopback 限制、响应上限和固定 deadline。

## 传输、采集与保存

流式应完整 finish 和 DONE；普通 JSON 应完整合法终态。严格 UTF-8、重复键、非有限数、
布尔冒充计数、截断帧、超大响应和终态后事件均有拒绝测试。
合法嵌套 usage 对象不当成计数；content 和 reasoning 分开，空白/心跳不触发首答案。
cached/reasoning token 缺测为 null+原因，不将无 delta 推断为内部零 token。

逐请求引擎 timings 只有 ID 关联和单位/范围核验后才能归属；全局 throughput 差值不拆给
单请求。GPU 主机温度、聚合显存、进程 RSS、主机可用 RAM 分别标来源；CPU 温度和 WDDM
归因缺测保留。客户端 HTTP 时间不作为 decode 吞吐。

正式核心配置/冻结计划的资源阈值、AC、磁盘、身份、HostLock、
dirty 和期限保持既有规则；温度/内存暂停只属于旧诊断实验，不能自动继承。
lab 通路首发送前 mark dirty；完整响应和取消 accepted 均不能清理。只在实例稳定、
请求状态 released、全服务 active_total=0 且非 poisoned 后清理。

原始输出、计划、日志、manifest 与报告都写新目录；离线核验不访问网络。


## 决策、替代与风险

采用引擎端账本和请求关联，代价是构建与维护新二进制。纯代理计数不能完整覆盖后台
释放，原 monitor 快照也不能提供同一同步边界，均不作为正式 idle 来源。
仅显式 `lab_required` 时，参数和输入 token 接口不足记录明确阻断；
默认 auto 降级时保留缺测与原因，不授予未观测的参数或性能资格。支持范围按已验收能力公布。

## lab 分词、参数与生成映射

lab 观测接口 identity 的五项 capability 均须为 true；不能据布尔声明推断接口已可用。
以下 lab 管理操作同样认证、不重定向、响应累计≤2 MiB，各次操作共用其 5 秒 deadline。
启动能力探测则逐端点各给 5 秒：7 个端点的串行探测预算合计最长约 35 秒，
不是全轮 5 秒；解析、记录及本机调度另有少量开销。显式严格模式可能提前拒绝。
分词/参数观察不进入生成账本。所有响应恰含列出的字段，整数拒绝 bool/非有限值。

- `POST /lab/v1/token-budget`：发送冻结 chat 请求体（stream=false、不含请求 ID），
  返回 `protocol, server_instance_id, input_tokens, context_size, template_sha256,
  template_prompt_sha256`。协议为 lab_observation.v1；实例/模板/上下文须匹配冻结身份，
  input_tokens 为模板渲染并处理特殊 token 后的精确非负整数计数，两个 SHA 为小写 64 hex。
  渲染 SHA 对应实际送入分词器的 UTF-8 模板文本。input+max_tokens 超上下文即发送前拒绝。
- `GET /lab/v1/requests/{id}/parameters`：完成后的保留记录，返回
  `protocol, server_instance_id, request_id, generation, conditions, auxiliary_routes`。
  generation 恰为配置 generation 的全部字段（含 seed_support/reasoning_mode/stop）；
  conditions 恰为 context_size/threads/threads_batch/slots/cache_policy。
  auxiliary_routes 恰为 vision/speculative/ngram 三个 false。值、类型及 ID 绑定需核验；
  普通与流式 probe 各自保存有效参数。
- `POST /v1/chat/completions`：messages、model 与 generation 参数直接发送，去掉仅配置用的
  seed_support，保留 reasoning_mode；附 n=1、cache_policy=disabled、stream，流式附
  stream_options.include_usage=true。不发送 Prism 的 chat_template_kwargs/cache_prompt。
  生成体与头均绑定 32 hex 请求 ID，体字段为 lab_request_id/lab_server_instance_id，
  头为 X-Lab-Request-ID/X-Lab-Instance-ID。实例和 ID 在发送前冻结到 request snapshot。
- 普通响应和每个 SSE JSON 帧使用 lab 观测接口 GenerationDecoder；完整 finish+DONE 后仍读取至 EOF，
  拒绝终态后事件。解析/HTTP/超时异常发送一次请求专属 cancel，不隐式重试生成；
  取消失败保留 pending ID，排空仍应读取请求状态和全服务生命周期。正式请求失败后停止余项。

`lab_wire` 事件保存经凭据脱敏的 UTF-8 原响应文本及引擎请求/实例 ID；有效 UTF-8 的字节
可由文本精确重建，原 chunk 边界不作为 token 事件。`lab_usage` 保存四项 usage 和缺测原因。
`idle_observed.lab_snapshot` 保存 lifecycle、关联 request 与 cancellation；source 明示
`/lab/v1/lifecycle`。离线 ledger 按请求快照重放响应、复核用量、实例、状态递进与释放证据，
并复核 `token-budgets.v2.json` 每个 entry 的 `budget.lab_token_budget` 原响应及两份有效参数快照。
旧 `token-budgets.json` 位置数组返回 `unsupported_format` / 2，与 v2 并存时也拒绝。
v2 的 `budget.lab_token_budget` 仍严格核验六键记录形状。
预算选择和 definition 见[活动数据契约](../data-contract.md#命令内复用与预算快照)。
report/verify 不访问服务。Prism timings 定义不套给新引擎。

合法/非法配置见 tests/fixtures/contracts/config.{kvmem,ninfer}.{valid,wrong-asset}.json；
占位示例见 configs/windows-{kvmem,ninfer}.example.toml，需替换本机路径、字节数、SHA、
构建 ID、实际 argv、PID/FILETIME，不能直接用于真机。

## 原生发行版能力探测

本节适用于 lab 能力不可用时的默认 auto 模式，上述完整 lab 条件只用于 lab 通路。
`engine.observation_mode` 可选 `auto`（默认）或 `lab_required`；只有显式要求 lab
而缺少协议能力时才以契约错误阻断。身份、文件绑定和冻结参数声明保持原校验。

生成使用 OpenAI chat 和 include_usage，不发送 lab ID 或 Prism 专属模板字段；
lab 可用时才增加协议 ID 绑定。只读探测保存逐端点 HTTP 状态、可用值、缺失原因和脱敏正文。
流式 `usage:null` 接受并跳过，收到 usage 对象才校验并记录用量；全流缺少对象时，
完整 finish + DONE + EOF 仍允许 completed，用量记录 null + `not_reported`。
非 null 的非法 usage 类型仍拒绝；内容、finish reason 与取消/dirty 规则不变。
health、models、slots、metrics、props 仅在成功探测后成为原生信号，不能推断全服务 idle。
未发现请求专属取消接口时只中断 HTTP，native_cancel 为 null+原因；不盲发全局取消命令。

原生模式记录串行客户端接纳状态，引擎内部排空为 unknown/null。完整响应且连接收尾成功
只结束当前客户端请求标记，不授予引擎内排空资格；取消、超时、解析和工具异常保留 dirty。
旧 dirty 仍须既有身份/恢复规则处理，不能凭弱信号清除。正常请求的状态清理明确标注
client_http 范围，与 lab 模式的引擎释放证据分开。能力矩阵同时写入封存快照、manifest、
probe 输出和离线报告；进程 PID/FILETIME/文件身份与引擎内证据分列。
缺少模板预算/有效参数时为 null+原因，不能伪造已核验。新运行不继承旧实测资格。

`engine.environment` 可选，只允许冻结 `CUDA_VISIBLE_DEVICES` / `CUDA_CACHE_DISABLE`；
实际进程的这两个白名单键须与声明逐项完全相等；其余环境变量在身份面之外，不冻结也不拒绝。
启动前在同一 PowerShell 窗口核对这两个键：无须覆盖时移除，再启动外部服务；
确需覆盖时把相同值写入 `engine.environment` 后启动。遇到 `lab_unbound_engine_environment`，
检查已启动进程继承的白名单值与配置；环境变化后由操作者重启并重新绑定 PID/FILETIME，
或按进程实际值更新配置，重新冻结计划并写新运行目录。CLI 不修改或重启服务。
不允许用此字段保存凭据；其他环境对生成的影响仍按有效参数缺测披露。
NInfer 发行版的第一个位置参数绑定模型；同句柄文件身份与完整 argv 核验不变。
`engine_internal_drain.scope = "capability_only"` 表示能力披露，不代表当前排空状态；
lab 的实际排空仍逐请求读取生命周期与释放证据。合法配置更新为 auto，新增
`config.{kvmem,ninfer}.wrong-observation.json` 拒绝未知观测模式，旧 v3 配置省略字段仍可读。

进程 `startup_args` 封存实际读取后的脱敏参数副本，完整 argv SHA 仍按原绑定核验。
每次 native 正常响应完成后，先在运行目录写 `client-http-clean-<请求键SHA>.json` 并封存，
再清理主机锁标记。收据关联 run/request/client ID、dirty token、finish reason 与客户端依据；
内部排空仍为 null + 原因。写收据失败保留 dirty；离线读取复核存在的收据，旧 v3 原件缺此
可选收据时保持原读取语义。收据证明清理依据，不宣称主机锁写入已成功。
native 失败的 stop reason 为 `native_generation_failed:<原始error_category>`；
终态与 dirty 均保留，不以 `service_stop_unconfirmed` 或锁状态错误掩盖原始失败。

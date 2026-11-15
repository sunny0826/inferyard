# Windows KVMem / NInfer 适配契约

正式 CLI 的 `kvmem` / `ninfer` 使用活动 v3；engine-fit 尚未接入。
字段级解析与原生身份接口见[lab 观测接口](windows-ninfer-adaptation-t0.md)，来源解释遵循[证据血缘规则](../data-contract.md#证据血缘与比较结论)。

仅显式 `engine.observation_mode = "lab_required"` 时，要求引擎提供完整
`lab_observation.v1` 和[生成与观测契约](windows-ninfer-generation-observation-contract.md#lab-分词参数与生成映射)中的分词/参数接口；缺接口时 `probe` / `run` 返回契约错误。
已发布的 KVMem v0.16.0-rc3 / NInfer v2026.09.27b 在当前侦察中 lab 接口为 404；
默认 `auto` 使用 OpenAI 生成并降级披露原生信号，详见[生成与观测契约](windows-ninfer-generation-observation-contract.md#原生发行版能力探测)。

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

## 生成与观测

lab 分词/参数映射、原生发行版能力探测与生成收尾规则见
[生成与观测契约](windows-ninfer-generation-observation-contract.md)。


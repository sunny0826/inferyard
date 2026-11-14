# Windows KVMem / NInfer 生成与观测契约

本文是 [Windows KVMem / NInfer 适配契约](windows-ninfer-adaptation-contract.md) 的续篇，承接
lab 分词/参数映射、原生发行版能力探测与生成收尾规则；职责、身份、配置字段与失败边界仍在主契约。

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

本节适用于 lab 能力不可用时的默认 auto 模式，[主契约](windows-ninfer-adaptation-contract.md)所述完整 lab 条件只用于 lab 通路。
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
client_http 范围，与 lab 模式的引擎释放证据分开。能力矩阵写入封存快照的
`engine-capabilities.json`、随 manifest 一并哈希，并由 `probe` 输出展示；它不进入
`studio.json` 报告。进程 PID/FILETIME/文件身份与引擎内证据分列。
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

# ADR 056：同一 GGUF 的跨引擎受控比较

状态：Accepted。日期：2026-11-06。设计已采纳，实现、能力实测与真机比较尚未完成。

## 问题

用户需要回答「换引擎后速度是否改善」。现有 `compare` 只支持 model、config、
side-by-side 三种模式；[ADR 055](055-local-engine-formal.md) 明确本地引擎
正式运行不获得跨引擎性能比较资格，
[比较器](../../src/inferyard/analysis/comparison.py) 对本地 profile 直接阻断。
本地 profile 还拒绝严格定长与缓存协议，无法保证两侧做了相同工作量。

## 决定

新增比较模式 `engine`，只允许推理引擎及其冻结的调优参数不同。
首个支持的组合是 llama-cpp 对 lmstudio，两侧使用同一个单 GGUF 文件。
其他家族组合返回 `unsupported_engine_pair`，逐对实测后再开放。
MLX 与 GGUF 不是同一份资产；llama-cpp 对 mlx-lm 的组合见文末修订。

### 必须相同的条件

- 模型字节摘要、题包与 case 顺序、逐请求的请求体摘要、冻结生成参数、
  context 大小、槽位与并发 1、缓存协议、输出模式、安全策略和设备身份。
- 逐请求的实际输入 token 数。两个服务各自渲染聊天模板，相同 messages
  不保证相同 token；任一配对请求不相等时阻断性能结论，并保留该请求的差异记录。
- 测量定义版本与开销核算口径。

### 允许不同并须观察的条件

引擎家族、安装摘要和声明的调优参数：flash attention、batch、ubatch、
KV 类型、GPU 层数。两侧调优参数在计划中冻结，并由服务侧观察核对。
声明了但无法观察的调优参数在配置阶段拒绝，不记为 unknown 后继续。
任一参数未观察或两侧取值不一致时，比较范围标为 `product_configuration`，
结论只适用于「引擎 + 默认配置」，不归因于引擎实现。
LM Studio 没有经核验的加载参数来源，因此首组总是 `product_configuration`。

### 输出与缓存

- 吞吐与 E2E 比较要求两侧输出相同 token 数。引擎能证明严格定长时启用
  `strict_fixed_length`；不能证明时，只比较输出 token 数相等的配对请求，
  并报告排除数量，不用 E2E 对不同输出长度排名。
- 首版只比较冷缓存口径。每个请求须有缓存命中数为 0 的证据；
  缺证据的一侧首内容延迟记为 `cache_unverified`，不进入胜负判定。
- 本模式对上述两项放开 [local_profile](../../src/inferyard/runtime/local_profile.py)
  的拒绝，仅在冻结计划声明 `comparison.mode = engine` 时生效；
  普通本地运行的限制不变。

### 指标

只用客户端测得的指标判定：L01 首内容延迟（流式）、L03 E2E、L04 端点输出吞吐，
以及新增的客户端解码间隔：`(E2E − L01) / (输出 token 数 − 1)`，
只在配对输出 token 数相等时计算，指标 ID 由目录生成脚本分配。
L06、L07 由各引擎自行定义，只展示，不参与判定。

### 执行顺序与判定

两侧按 A、B、A、B 交错执行，每侧至少 3 个块；每块是一次独立冻结运行，
块间按 ADR 055 核验旧进程退出、替换服务并重新绑定。
块间等待温度回落到计划冻结的起始阈值以下，等待时间写入证据。
胜者只在全部门槛通过、且每个配对块的块内中位数差值方向一致时输出；
否则结论为 `no_clear_difference`。不从单块结果推导加速比。

### LM Studio 后台

操作者已授权测评流程停止并重启现有 LM Studio 后台。
CLI 仍不停止或启动服务：块间替换由操作者执行，比较器以各块绑定的
服务 PID/start 互不相同作为替换证据，主机状态继续核验旧进程退出。
Studio 侧的接管需另行设计；ADR 055 中「Studio 不能停掉用户原有 LM Studio
后台进程」的规则不变。

## 不做

- 多轮会话回放与真实编程任务：本轮不做。
- 32K 实际输入分档与显存溢出判定：另行设计，本模式先使用默认双题包。
- 通用的强制可比开关。

## 修订（2026-11-06，能力实测后）

能力实测原件见 `.tools/engine-compare-probe-20261106/`（诊断，不是比较结果）。

- LM Studio 的 usage 不含缓存命中数，`cache_prompt=false` 被忽略：同一 675 token
  输入重复请求从 362ms 降到 80–85ms。因此 llama-cpp 对 lmstudio 在本模式下
  恒为 `cold_cache_unverified`，保留支持但不会产生结论。
- 新增组合 llama-cpp 对 mlx-lm（`CROSS_FORMAT`），使用同一上游模型的不同格式
  （GGUF Q8_0 与 MLX 8bit）。模型资产字段允许不同；结论范围固定为
  `engine_and_model_format`，附 `model_format_differs_not_attributable_to_engine`，
  只回答「这台机器上哪套引擎与格式更快」。逐请求输入 token 相等仍为硬门槛，
  它同时核对两侧分词与模板。
- MLX 冷缓存由结构保证：项目自带的正式 MLX 服务调用 `mlx_lm.stream_generate`
  不传 `prompt_cache`，每个请求新建缓存；服务源码在服务核验时与 CLI 包内源码
  比对摘要。该侧缓存证据记为 `formal_server_no_prompt_cache_reuse`，
  与 llama-cpp 侧的 `observed_zero_cached_tokens` 分别列出，证据强度不同。
- 温度停止：操作者显式要求忽略温度。本地候选可冻结
  `safety.temperature_stop_override_reason`；冻结计划的停止阈值随之为 null，
  温度读数仍必需并记录。没有该声明时 null 阈值继续拒绝。
  使用该声明的比较附 `temperature_stop_overridden`；
  热降频可能影响两侧，A/B 交错只能部分抵消。

## 兼容性

比较模式枚举新增取值，比较结果新增 `engine-comparison.v1` 定义；
既有 run、比较与报告证据按原定义读取，不获得新资格。
字段与拒绝路径以后续契约为准，进度见[实施计划](../plans/engine-comparison.md)。
证据解释遵循[血缘规则](../data-contract.md#证据血缘与比较结论)。

# 本地引擎正式串行契约

本契约对应 [ADR 055](../decisions/055-local-engine-formal.md)。
配置 definition 为 `local-engine-config.v1`，adapter 为 `local_openai_v1`。
当前实现限定 macOS Metal；普通串行文本运行不自动获得持续负载、严格长度、
缓存受控实验或跨引擎性能比较资格。

## 资产与绑定

`engine.family` 为 llama-cpp、mlx-lm、vllm、sglang、lmstudio。
前者和 LM Studio 使用单 GGUF；三个 Python 家族使用目录模型。
`model.content_sha256` 是排序文件清单的摘要；`assets_sha256` 是清单文件字节摘要。
不能把任一个写成旧单权重文件 `model.sha256`。
模板保存独立文件及字节摘要，不能据此声称服务实际使用了相同模板。

安装声明 `local-engine-installation.v1` 包含 executable、working_directory、
runtime_roots 和白名单环境。候选冻结为 `local-engine-runtime.v1`，
包括 Python 实际 import 路径、解释器根、源码、扩展和动态库库存。
Python 正式入口用 `-I -S -B` 启动包内 bootstrap，仅加入冻结的 venv site-packages；
不执行 `.pth` 或 site customization，也不把 runtime_roots 加入导入路径。
依赖 editable 路径的入口、未声明外部来源及库存变化拒绝准入。
Python worker 同样要求隔离启动和可识别的入口。
安装可另行声明完整 `worker_environment`，只用于 Python 家族的后代核验，
不合并到主进程启动环境；缺省时严格沿用 `environment`。
worker 声明必须保留主进程全部声明值，额外变量按家族封闭列举并精确匹配，
不从当前进程学习白名单。安装摘要同时绑定该声明，离线使用同一语义检查。
`OMP_`、`PROMETHEUS_` 与既有受控前缀的未声明项均拒绝；
`LD_LIBRARY_PATH`、`PYTHONPATH` 等搜索路径覆盖不接受。
SGLang 的运行 ID 和独立 metrics 目录可显式冻结，metrics 目录须位于工作目录内，
每个服务实例使用新目录，不能复用旧进程留下的计数文件。
系统进程接口核验可观察的 exec 环境，不将其表述为 Python 内部所有运行时变量的快照。
`setproctitle` 丢失原 argv 时仍拒绝；需要保持身份的外部补丁必须进入独立安装的新库存。
库存保留未解析动态库依赖及原因；未使用的可选库缺项不伪造为存在，
实际映射的非系统库必须全部在冻结清单中且依赖可解析。
`lsof txt` 同时包含数据映射；未知映射以 device/inode 和稳定文件状态绑定读取
Mach-O 文件头，不以扩展名判断。未知原生代码映射拒绝；GGUF、系统 plist、
ICU 和 Metal 缓存等非 Mach-O 数据不作为动态库库存。模型仍独立核验完整清单；
系统及驱动生成的缓存字节不在本契约的可复现安装范围内。
原文件已删除或因系统权限不可读的数据映射还须由本次 `vmmap` 观察证明：同路径全部区域均为
`mapped file`，当前及最大权限都不可执行。缺失、格式不识别或任一区域可执行时拒绝；
该观察不为未知原生库豁免库存核验。

冻结的 runtime、模型清单和 binding JSON 必须采用 `storage.json_bytes` 的规范字节。
离线从封存对象重建字节摘要，与配置及 FileIdentity 库存核对，
不重新打开当前模型、Python 安装或服务。

绑定 `local-engine-binding.v1` 包含 runtime 摘要及实际服务身份：
PID/start、唯一 listener、可执行文件、argv、cwd、模型位置及 LM observer。
完整子进程树保存每个 worker 的 PID/start、parent PID、executable/argv/cwd 摘要；
绑定前后重复观察，不把瞬时不完整进程树当作稳定身份。
每轮后代观察也复查环境未在观察窗口内改变。
LM Studio 的 argv 是实际后台进程命令，不能以 `lms server start` 参数冒充。
每次发题前核验身份；队列观察也以身份前后核对包围。
资源指标继续表示绑定 PID，未观测 worker 内存不补入旧字段。

## 请求与安全

请求只发送 model、messages、temperature、max_tokens、stream；
流式额外发送 `stream_options.include_usage=true`。
temperature 固定为 0，max_tokens 与 context 显式冻结；
不发送 seed、top-p、reasoning 开关或假定各引擎共享默认值。
实际参数、模板与精确输入预算没有可靠观察时为 unknown/null 加原因。
缺 usage 不补零；上下文拒绝、错误 JSON、提前 EOF 按正式失败处理。

新 MLX 入口为 `mlx-lm-formal.v1`，支持普通与流式请求。
服务版本观察绑定入口源码、GenerationQueue 源码、非 synthetic 状态和 context 上限。
旧诊断入口不接受这个新身份。

新配置必须接通电源，温度来源必需，停止阈值最高 85°C；
安全检查间隔 0.1–5 秒，周期检查环境、温度、内存和磁盘。
冻结实验不得省略或放宽配置要求；周期检查不反复全扫 Python 库存。
请求边界与异步身份观察的成本必须纳入测量开销。

## 队列、清理与转换

`local-engine-observation.v1` 保持 mode=native、request_id_scope=client_only。
HTTP capability 保留真实响应与状态码；LM 的 `lms:ps` 为本地 CLI 观察，
status_code 为 null，仅保存经核验的实例状态和计数，不保存私有完整 CLI 输出。

每次 `native_observed.signals.queue_snapshot` 使用 `local-engine-queue.v1`，
保存家族、served_model、绑定身份、观察来源、原始 metrics 或 LM 规范计数、
重建结果、不确定状态和缺测原因。
metrics 的全部 label series、成对计数、重复项和有限非负整数均需核验。
队列观察不产生 `service_drain`，也不证明 GPU 同步或内部请求释放。

发题要求合法的请求前空闲观察。正常 HTTP 终态关闭 context 后，
还需成功的请求后队列观察，随后保存 `client_http` 回执再清 dirty。
离线重建校验前后观察、请求顺序、客户端 ID 和回执归属。
请求失败、取消、超时、身份变化或观察失败后停止继续发题，保留原件与 dirty；
后来 queue=0 不能消除不确定性。

主机状态额外保存 `local-host-service.v1` 的 scope 摘要和已绑定进程树，
clean 后仍保留，下一条独立命令也不能同进程跨 scope。
dirty 恢复与正常替换都核验主进程及 worker 已退出。
同 workload 正常串行继续沿用现有 native 规则。
跨题包需要强排空证明或经核验的进程替换和重绑；
重跑的 `--from-run` 仍要求新服务，不扩大既有边界。
CLI 操作者执行替换；Studio 只能管理自己拥有的进程，
不能停掉用户原有 LM Studio 后台进程。

旧 v3 配置、native/lab 证据与诊断 definition 按原定义读取。
本地新 profile 的公开脱敏包尚不受旧单文件身份格式支持，明确拒绝。
离线 HTML 报告按资产清单和运行环境快照展示身份，不伪造旧字段。
解释与比较结论见[血缘规则](../data-contract.md#证据血缘与比较结论)。

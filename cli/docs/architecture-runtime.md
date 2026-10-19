# 架构运行路径

本文属于 [当前架构](architecture.md)。

## 请求与运行

[engine-fit](engine-fit-common.md) 使用独立诊断定义和引擎观察接口，复用主机锁、dirty 恢复与原生资源采集。vLLM / SGLang 绑定模型目录，llama.cpp 绑定单 GGUF；macOS 的
MLX-LM 绑定随包受控服务脚本及目录，LM Studio 绑定唯一已加载本机实例、CLI 观察器与
实际打开的 GGUF。原生监听和进程身份仍逐次重验。MLX 队列覆盖生成器关闭和 GPU 同步；
LM Studio 的只读观察器固定连接 `127.0.0.1` 和绑定端口；`lms` 仅传显式 `--port`，
使用本机 CLI 身份，不传 `--host` 或自动发现、启动服务。Ollama 缺少全服务
空闲来源，执行阻断。产物独立封存并离线重建，不进入正式评分或性能比较资格。
边界见 [ADR 014](decisions/014-macos-common-engines.md)、[ADR 015](decisions/015-lmstudio-local-cli-identity.md)
与[常用引擎契约](contracts/engine-fit-contract-common.md#常用引擎扩展)，继承原身份、取消和同资产比较条件。
[ADR 016](decisions/016-engine-fit-temperature-override.md) 允许操作者在新 plan.v3 中
显式冻结临时跳过温度停止的原因，对应 run.v4。默认和旧计划仍为 85°C；温度照常采集，
其他停止条件和恢复规则继续执行。覆盖不能在运行时切换，也不能与旧计划混合比较。
[ADR 019](decisions/019-engine-fit-memory-override.md) 追加独立的内存停止覆盖，
只在新 plan.v4/run.v5 中显式冻结原因与 null 内存下限，可与温度覆盖组合。
Guard 继续采集两类读数及缺测，报告标记各项覆盖；磁盘、身份、预算与 dirty 继续执行。
[ADR 020](decisions/020-windows-engine-fit-native.md) 为 Windows 单 GGUF llama.cpp
增加独立 run.v6；原生 FILETIME/监听、稳定进程树 working-set/CPU 秒及 NVIDIA 温度
分别注明来源。旧定义继续拒绝 Windows，其他 Windows 引擎仍明确阻断。
软件/模拟验证不授予真机或正式测评资格，见[Windows 契约](contracts/engine-fit-contract-platforms.md#windows-原生来源)。

发送模型请求前应持有主机锁，完成配置、服务身份、dirty 恢复和空闲检查；正式任务的输入、顺序、预算与停止条件应预先冻结。各执行路径的本地准备顺序可以不同。探测及实际输入预算验证后，按冻结协议执行预热、基线和正式请求，随后排空、环境终检与封存。具体次数、间隔与截止时间来自协议；诊断、预热和正式请求各有 phase，不混入正式质量分母。

服务身份联合核验模型/引擎文件哈希、模板、有效参数、PID、启动时间与端口归属。`requested` 不自动等于 `effective`；无法核验的参数为 unknown。模板后的实际输入预算不静默截断。

macOS 使用原生进程身份和 `lsof` 核验二进制及监听端点；模型绑定核验启动参数指向的文件设备号/inode，模型映射仍标为未观察。Metal 还须验证 Apple Silicon/Metal 能力、正数 offload 参数，以及已绑定且实际加载的 `libggml-metal.dylib`。这些检查不证明模型的 GPU 驻留，也不替代真实模型测评。

所有模型请求前取得主机共享锁。发送前持久化请求身份和 dirty 状态；连接关闭、低 CPU 或 `/health` 成功都不能替代可核验空闲证据。超时或取消后停止发送并按冻结期限排空；无法确认空闲时保留 dirty。恢复声明绑定当前 token，还须核验旧进程消失、新服务身份及空闲。

每个正式题目没有隐式重试。固定计划满足：

```text
planned = completed + failed + cancelled + invalid + not_executed
executed = completed + failed + cancelled + invalid
valid_executed = completed + failed
```

| 状态         | 含义                                                       |
| ------------ | ---------------------------------------------------------- |
| completed    | 协议正常终止；答案仍可判错，评分异常单独为 unscorable      |
| failed       | 模型服务、传输、协议或请求预算失败，按协议保留质量分母     |
| cancelled    | 操作者取消，不当作模型答错                                 |
| invalid      | 工具或证据故障；中断后只有 started、没有终态的请求归入此项 |
| not_executed | 未开始，保留计划身份和原因                                 |

生命周期结束不代表结果完整。评分故障不撤销执行终态或缩小分母；分母为零、无法评分或不满足整包条件时不给完整通过率。持续负载的 `planned = null`、窗口及排空语义见[实验设计](experiments/design.md)，不套用固定题序数量。

## 计量与比较

客户端计时使用单调时钟。首内容与首答案分开；空内容、心跳和元数据不触发首内容。完整协议结束与提前 EOF 分开，总截止时间不因新事件续期。SSE event、网络 chunk 与 token 不等价；token 用量注明端点来源与计数范围。

采集按绝对调度点记录读取起止、实际间隔和丢样。读取跨越 phase 或请求边界的样本不能简单归入当前请求；短请求无样本不外插。RSS、系统可用内存、显存和能量分别标明层次、单位、窗口及来源，不能互相替代或相加。PID 复用、计数回退、缺段和传感器不可得分别留原因。

macOS 原生采集 CPU、RSS、可用内存、主机换页和只读 AppleSMC 温度；频率、能耗和独立显存保留缺测。统一内存不能与 RSS 相加。来源及单位随证据保存，离线读取按保存的来源归约；CPU 启动身份和计数口径见[平台契约](contracts/macos-contract.md)。性能环境按保存平台核验原生电源策略，Linux governor/EPP 不填造；温度、原生策略或完整开销证据缺失仍阻断。正式通路总开销及目标绑定见[ADR 006](decisions/006-macos-performance-prerequisites.md)。

性能比较绑定设备、OS/驱动、引擎、任务、参数、缓存/预热、环境、测量源码和定义。unknown 不能与 unknown 视作相同；质量、耗时、资源分别判定资格。量化比较须有同源 revision，缺少基线不给收益结论。描述性差值与受控结论资格分开；只满足并列条件时不生成因果结论或赢家。

总采集开销包括采集、校验、队列、写盘、flush/fsync 和封存的完整路径。轮询开销或短预跑通过不足以授予正式性能比较资格；源码变化后重新核验实际测量来源。


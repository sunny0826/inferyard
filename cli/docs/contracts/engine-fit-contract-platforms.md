# 同机引擎平台来源

本文是 [engine-fit 契约](engine-fit-contract.md) 的 macOS、Windows 与 MLX 来源规则。
基线见 [基线契约](engine-fit-contract-base.md)，常用引擎见 [扩展契约](engine-fit-contract-common.md)。

## macOS 原生来源

依据 [ADR 013](../decisions/013-engine-fit-macos.md)，在公共诊断规则上增加原生来源。
CLI、计划 v1、比较 v1 与封存目录格式保持不变。

### 运行与身份

macOS 输出 `schema_version=3, definition=engine_fit_run.v2`；Linux 仍输出 v1。
v2 要求冻结计划 host.platform 为 Darwin。
离线校验明确分派 v1/v2，不猜测或改写旧数据。v2 只接受以下 macOS binding：
保留 v1 的 pid/start_ticks/origin/address/port/executable_sha256/argv_sha256/model_binding，
`listener_inode` 应为 null，新增 `listener_identity`、`listener_source`、`cwd_sha256`。
cwd_sha256 为规范化绝对 cwd 路径的 `os.fsencode` 字节 SHA256（不含尾 NUL），不暴露路径。
source 应是 `lsof:TCP:LISTEN:pid`，identity 应匹配
`macos:tcp:<address>:<port>:pid:<pid>`。v1 继续只接受原来 numeric inode 形状。
启动身份是原始内核单调启动时间整数微秒。模型目录按 device/inode 核验。
同用户 PID、可执行文件当前映射、原始 argv 哈希、cwd、模型入口、全局端口唯一监听归属
在绑定与后续检查中核验；任何变化或权限不明阻断，不持久化 argv 和凭据。

### 平台接口

`platforms/engine_fit.py` 的 bind_service/check_service/resource_snapshot 保持签名。
Darwin 且 proc_root 为默认 `/proc` 时延迟分派 `engine_fit_macos.py`；自定义 proc_root
继续走 Linux fixture。macOS 模块可复用既有 identity/macOS 原生 helper。
resource_snapshot 返回原来的字段集：host 可用内存 bytes、进程树 RSS bytes、
CPU user+system seconds、process_count、scope、missing_reasons。
scope 应明确 psutil 来源、observed_live_descendants、排除退出进程与 RSS 共享页重复。
启动身份、父子关系和子集合读取前后校验，任一成员变化/权限失败整棵树缺测；
主机可用内存单独保留。不能将主 PID 指标冒充进程树。

### 温度与证据

Linux 保留 linux-sensors.v2 / thermal_zone_reported，整数 raw 毫摄氏度乘 0.001。
macOS 使用 macos-smc.v1 / smc_key_reported，AppleSMC: 来源，raw=value 摄氏度，
有限且范围 -273.15 到 200；两种来源均保持缺测原因与单调读取时间。
v1 仅接受 Linux 温度来源，v2 仅接受 macOS 温度来源，不混淆单位。
运行器按平台选择传感器，85°C、内存/磁盘阈值与 dirty 恢复规则不变。
新记录不能声称已独立核验目标进程架构、Metal/MLX 后端、GPU 内存或已退出子进程资源。

## Windows 原生来源

依据 [ADR 020](../decisions/020-windows-engine-fit-native.md)，继承
[常用引擎契约](engine-fit-contract.md#常用引擎扩展)。核心 schema_version 仍为 3。

### 运行定义

新增 run.v6；字段集合沿用 run.v5，platform 应为 Windows 并等于 plan.host.platform。
只允许 engine=llama-cpp、model.kind=gguf、plan.v2/v3/v4。
停止覆盖标记、禁止原因及参数分别遵循对应计划；默认计划仍保持原默认停止。
run.v1–v5 平台/形状/来源规则不变，不能把 Windows 产物重新标为旧版本。
service 继续使用 /v1/models、/props 与 /metrics 的现有严格协议，版本缺测不能猜测。

Windows binding 为既有 pid/start_ticks/origin/address/port/executable_sha256/
argv_sha256/listener_inode/model_binding，加 cwd_sha256、listener_identity、
listener_source、process_start_source。
listener_inode 应 null；listener_source=GetExtendedTcpTable:owner_pid；
listener_identity 为 windows:tcp:ADDRESS:PORT:pid:PID；
process_start_source=GetProcessTimes:creation_FILETIME_100ns_since_1601。
model_binding 保留 engine/path/device/inode/source；source=verified_startup_file_identity。
device 是 0 至 2^64−1 的无符号卷标识，inode 是 0 至 2^128−1 的文件标识；应为真正整数，
不得经浮点数或有符号截断。旧运行定义继续保持其既有整数范围。
argv/cwd 只持久化哈希，账户及环境只用于当次核验。

### 身份与资源

platforms/engine_fit_windows.py 提供 bind_service 与 resource_snapshot；
现有平台分派仅在原生 Windows 且默认 proc_root 时进入它。
bind_service 只接受明确 `llama-server.exe -m|--model FILE`，执行文件与 argv[0]
均应匹配；参数白名单及单资产规则沿用 llama.cpp。Windows 参数和环境名的大小写
语义分别处理：环境配置名按不区分大小写拒绝，参数保持引擎实际的大小写。
exe 路径、账户、创建 FILETIME、argv、cwd、模型文件和唯一监听在绑定末尾重新核验。
服务管理仍由外部操作者执行。

稳定文件哈希在 Windows 统一使用 GetFileInformationByHandleEx，分别打开路径和
待哈希句柄，前后比较相同来源的卷号、完整 128 位文件 ID、原生属性、大小、创建时间、
写入时间及真实 ChangeTime（FILETIME 100ns）。只排除会因读取变化的访问时间。
Python 路径 stat/lstat 前后另作原始 stamp 对比，不跨 API 比较 mode/ctime。
目录、重解析点、待删除文件及任何原生查询失败均拒绝，不回退到截断 ID 或创建时间。
来源依据 [原生查询与信息类](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-getfileinformationbyhandleex)、
[文件基本信息和 ChangeTime](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_basic_info)及
[完整文件标识](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)。

进程树从 psutil.Process.children 获得，逐成员读取准确创建 FILETIME、父 PID、CPU
user+system 秒及 working-set RSS bytes；二次读取创建时间/父子关系，CPU 不得回退。
树最多 256 个活进程，身份/资源观察检查共享 10 秒预算；超限与读取失败保留 null 和原因。
memory_available_bytes 来自 GlobalMemoryStatusEx 的主机可用物理内存。
scope 应写入这些 Windows 来源及 FILETIME 单位，不能套用 `/proc` 或 Darwin 来源。

### 温度与执行

Windows 温度 collector=windows-nvidia-temperature.v1，semantics=gpu_temperature_reported，
unit=celsius，raw_value/value 为相等的有限数值，范围 -273.15 至 200。
source 为 nvidia-smi:gpu:INDEX:temperature.gpu，明确只归属主机 GPU；不声称模型温度。
缺少可核验工具、设备或读数时 raw_value/value=null，保存明确 missing_reason；
没有可确认设备时 source=nvidia-smi:unavailable，不能猜测设备序号。
读数有单调 ns 读取起止，server_pid/process_start_ticks/request_id 保持 null。
采集进程只运行只读查询，有 2 秒期限及 64 KiB 输出界限；CPU 温度不使用 ACPI 替代。

Guard 在 Windows 选择该采集器，默认/显式覆盖的停止比较仍使用计划值。
主机锁、dirty、请求冻结顺序、时限、连接关闭和排空不变；失败连接不自动 clean。
run.v6 仍 diagnostic=true、performance_comparison_qualified=false，限制明确当前磁盘
exe 哈希不证明加载字节、Windows CPU 温度缺测及 GPU 驻留未独立核验。
比较仍要求完全相同计划和测量源码；未接入的其他 Windows 引擎在发送前明确拒绝。

## MLX 总序列与线程

依据 [ADR 017](../decisions/017-macos-hf-three-engines.md)，活动核心 schema 仍为 3。

### 接口与边界

- 受控 `mlx_server.py` 接受可选 `--max-model-len N`，N 为 1 至 32768 的整数；
  未指定保留原输入上限。指定时 `prompt_tokens + max_tokens > N` 在生成前拒绝。
- MLX `/version` 保持 `mlx-lm.v1`、版本和脚本 SHA 绑定，可附上下文上限诊断字段。
  进程入口解析只增加此明确参数；重复、非法、未知参数继续拒绝。
- 不修改 plan/run 类型、CLI 生命周期或 idle/poisoned 语义。所有请求仍只有 temperature=0。
- MLX 显式使用公开线程本地 stream；主线程加载完成应同步，worker 的生成和同步
  使用其本线程 stream，不把普通线程所属 stream 传到另一线程。失败仍保留 poisoned。
- 第三方依赖、权重和服务由已获安装授权的操作者外部准备；环境与资产在忽略目录，
  测评器 Python 3.15 和根锁文件不加入引擎依赖。引擎逐一启动、运行、排空、关闭。

### 拒绝与证据

MLX 超总序列上限是客户端输入错误，不能启动生成器。准备阶段计入 waiting；
拒绝后撤回该请求的 waiting 预约，不改变其他活动或排队计数。输入拒绝本身不置
poisoned。客户端超时期间不能把仍在准备的请求观察为 idle。
第三方缺指标、启动失败或身份不匹配为软件/环境问题，保留日志与失败目录；
不放宽验证器。真实运行完成不自动授予质量评分、全平台支持或正式性能资格。

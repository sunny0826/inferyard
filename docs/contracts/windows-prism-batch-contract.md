# Windows Prism 批量执行契约

依据 [ADR 032](../decisions/032-windows-prism-batch-run.md)。活动契约仍为 v3，来源与结论遵循
[证据血缘总规则](../data-contract.md#证据血缘与比较结论)。

## 分派与职责

核心批调度与其单轮执行在 Windows 允许 `prism_llama_server_v1`、`kvmem`、`ninfer`；
其他适配器在真实依赖路径抛 `windows_phase2_live_not_supported`。注入测试依赖仍按原规则。
`application/overhead.py` 的 live 开销测量、`extensions/workflow.py` 的 live 扩展、
`application/dispatch.py` 的 prepare-length 继续拒绝 Windows，不属于本次能力放行。

`resource_collector_id(proc_root=Path("/proc"))` 在 win32 返回 `windows-resource.v1`；
显式非默认 proc_root 优先保留 Linux 夹具行为；默认 Darwin/Linux 返回既有 ID。
`collector_factory("windows-resource.v1")` 与资源分派工厂返回新平台 ResourceSampler。
Prism 单次/probe 继续使用既有内存采集器；KVMem/NInfer 批量继续使用 `windows-memory.v1`。

## 采集接口与证据

`platforms/resources_windows.py.ResourceSampler(store, config, *, proc_root, clock)` 复用
Sampler 的 phase、request_id、stopped、failure、environment、schedule 与 run 接口。
独立运行资源周期和每秒环境观察，沿用同步写盘、flush 与错过周期不补造规则。

`collect(pid, ticks)` 返回两个现有 MEMORY_SAMPLE 结构的样本：

| 指标                 | 来源                            | 单位/归属                        |
| -------------------- | ------------------------------- | -------------------------------- |
| system_mem_available | psutil:virtual_memory:available | bytes，主机；PID/创建时间为 null |
| service_rss          | psutil:Process.memory_info:rss  | bytes，绑定 PID，排除子进程      |

读取起止用 monotonic ns；RSS 前后由原生 process_start_ticks 核对完整 FILETIME。
身份变化后该实例的 RSS 持续缺测 `source_changed`。权限/读取失败与非整数、负值分别记
null + `permission_denied` / `source_unavailable` / `invalid_memory_value`。
不接受 bool、NaN 或 Infinity 为内存值。既有运行安全门继续独立检查真实可用内存。

`collector.json` 保存 schema_version、collector、interval_ms、sources、rss_scope、
process_start_source、sensors 及 unavailable_metrics。未实现指标条目为
`{value: null, missing_reason: "windows_resource_metric_not_collected"}`：服务 CPU、主机换页、
温度、频率、GPU 内存/利用率、功率和能耗。sensors.sources 为空；报告按既有缺测归约保存
null + 原因。不生成伪装为 Linux/Darwin 的累计计数或外部 CPU/边界观察证据。

collector 身份在组件目录源 `registry.components()` 注册；现有 MEMORY_SAMPLE 没有 collector
枚举，来源是非空 label，因此无需增加资源计数 Schema 枚举，也不改变旧样本接受范围。
目录指标/方法 ID 和定义不变；C01/C02/C03 的目录来源引用新增 Windows 实现与回归，
由 export_catalogue.py 重新生成 metrics.json，导出检查核验一致性。

## 选择、风险与拒绝边界

选择独立内存采集而非伪造通用 CPU/温度来源；代价是这些指标保持缺测及严格性能资格受限。
缺测不改变冻结温度/内存策略；require_temperature 仍拒绝缺源，max_external_cpu_percent
若无法核验仍 fail-closed。身份、哈希、服务空闲、取消、排空、HostLock 与 dirty 不放宽。
正式固定题包执行的验收不扩大到持续负载、扩展、长上下文或严格性能比较。

## 社区安装的离线运行时准备

本节只增加准备入口，不改变上述执行契约。参数和 CommandResult 见
[CLI 准备接口](../cli-surface.md#社区安装准备接口)。`runtime prepare` 仅支持原生 Windows x64，
由操作者提供已下载归档；不联网、不下载模型、不启动服务。普通用户可显式选择新目录，
新入口没有 D 盘或仓库根限制；旧脚本的路径和下载策略留在兼容层。

### 包内可信 profile

profile 放在 `src/inferyard/data/community/profile/`，随源码提交并随 wheel 交付。
NAME 与文件名一一对应，不能通过用户回执增加 profile 或更改受信值。
固定 release 为 `prism-b10743-adfffbe`，来源基址为
`https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/`；
每个归档的完整来源 URL 为该基址加下表文件名，仅记录来源，不在准备命令中请求该 URL。
固定数值来自现有准备脚本中的常量，不能从下载响应或待验证回执取得期望值。

| profile / 角色 | 固定归档文件名 | bytes | SHA256 |
| --- | --- | ---: | --- |
| `prism-b10743-adfffbe-win-cpu-x64` / engine | `llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip` | 19441568 | `d0b3016c9cc4bc1385de68be034adee570277ba952dd94292ba3888b7f18cc44` |
| `prism-b10743-adfffbe-win-cuda-12.4-x64` / engine | `llama-prism-b10743-adfffbe-bin-win-cuda-12.4-x64.zip` | 257322810 | `1b849f713bee42fda258de83770cd422e8f48dd631ce370eb0641f6458c69d87` |
| 同一 CUDA profile / runtime | `cudart-llama-bin-win-cuda-12.4-x64.zip` | 391443627 | `8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6` |

profile JSON 的必填字段冻结如下；它是新增准备元数据，活动证据 schema 仍为 3。

| 字段 | 定义 |
| --- | --- |
| `kind` / `schema_version` | `community_runtime_profile.v1` / 3 |
| `profile` / `platform` / `architecture` / `mode` | 上述 NAME / `Windows` / `x64` / `cpu` 或 `cuda` |
| `engine_release` | 固定 release |
| `archives` | 按 engine、runtime 顺序的数组，CPU 仅 engine；每项 `{role, name, source_url, bytes, sha256}` |
| `members` | 每个归档的全部普通文件数组，按 role/path 排序；每项 `{role, path, bytes, sha256}`，path 为归档内规范相对 POSIX 路径 |
| `engine_binary` | 最终输出树内的相对路径，如 `engine/llama-server.exe`；必须唯一且来自 engine 归档 |
| `runtime_library_manifest` | 固定为引擎二进制同目录的 `engine-sha256.json` |
| `libraries` | 最终引擎目录中 `llama-server.exe` 及全部 DLL 的 `{基名: SHA256}`，含 CUDA 合并后的 DLL；不接受回执自报的替代集合 |
| `version_pattern` / `version_timeout_seconds` | 与现有 macOS 生成器一致的 `\b10743\b.*\badfffbe(?:41)?\b` / 10 |

`members` 和 `libraries` 的值由**先通过上表大小/整包 SHA256 核验的固定归档字节**离线派生，
生成器读取归档但不执行二进制。完整派生清单纳入包内资源及 `--check`；
缺少归档或不能核对派生清单时不得以历史回执、合成文件或任意目录补齐并激活 profile。
归档身份固定，逐文件数值按确定规则派生并核对，不从待验证输入学习期望值。历史 `validation/` 仅可只读交叉核对，不作为发行资源来源。
profile 原文件 SHA256 供回执引用；config create 从已安装的同名 profile 重新算出期望摘要。

### 解包和 CUDA 合并

1. 先核验输入归档长度、SHA256、角色数量，再检查 ZIP 全部成员；任何阶段不执行未核验引擎。
   拒绝绝对路径、盘符、UNC、`..`、反斜杠、冒号、NUL、链接/特殊文件、Windows 保留名、
   尾随点/空格、重复或大小写折叠冲突、文件/目录冲突；解包目标必须留在新目录内。
   实际成员集合、展开大小和逐文件摘要必须与包内 members 完全一致，不能接纳额外文件。
2. engine 归档保留内部结构解到 `NEW/engine/`，唯一 `llama-server.exe` 的位置由 profile 指定。
   runtime 归档解到 `NEW/runtime/`；其全部 DLL 以基名复制到 server 同目录。
   同名同哈希只保留一份，同名不同哈希或大小写歧义拒绝，不按复制顺序覆盖。
   CPU 禁止 runtime 归档；CUDA 必须包含固定 runtime，不能借用系统目录 DLL 补缺。
3. 比较最终 server/DLL 集合及哈希与 profile.libraries；安全核验通过后排他写出
   `engine-sha256.json`，内容保持既有 `{基名: SHA256}` 格式。它是分发资产清单，
   不意味着其中所有 DLL 均已加载；运行期按原 Prism 身份规则检查，不改变其他适配器的库规则。
4. 只运行已验证 server 的 `--version`，cwd 为 server 所在目录；stdout/stderr 捕获且限 10 秒，
   不继承 shell 命令拼接、不传模型。版本匹配后再检查资产未变化，最后写成功回执。
   无服务生命周期行为；CLI 错误映射沿用冻结表。

### 新回执与 config create 验证

`NEW/runtime-receipt.json` 使用严格 JSON。必填且不接受额外字段：

| 字段 | 定义 |
| --- | --- |
| `kind` / `schema_version` | `community_runtime_receipt.v1` / 3 |
| `profile` / `profile_sha256` | 包内 NAME / 该 profile 文件原字节 SHA256 |
| `platform` / `architecture` / `mode` / `engine_release` | 与包内 profile 完全相同 |
| `archives` | 与 profile 相同的 `{role, name, source_url, bytes, sha256}` 数组；不保存操作者下载路径 |
| `engine_binary` / `runtime_library_manifest` | 相对回执父目录的 profile 指定路径；禁止绝对路径、越界和重解析点 |
| `engine_sha256` / `libraries` | 实际核验结果，必须与包内固定值相等 |
| `version` | `{argv: ["--version"], returncode: 0, stdout: string, stderr: string}`；输出为观察值，不作为资产信任根 |
| `model_requests_sent` / `ready_to_run` | 0 / false；不含模型已验证声明 |

`config create --runtime-receipt FILE` 的验证顺序固定为：

1. 严格读取回执，检查 kind/schema/字段类型、重复键、非有限数，拒绝 bool 冒充整数；
   从安装资源找 profile，核验 profile 本身及回执的 profile_sha256、release、mode、平台/架构。
2. 回执 archives 和 libraries 必须与 profile 完全相等，不能只核验回执内部自洽。
   回执相对路径按其父目录解析，必须匹配 profile 并留在该目录内；移动整目录可用，
   不通过根映射、磁盘搜索或读取回执中的外部 URL 找资产。
3. 复核全部解包成员、合并 DLL 和实际 server 字节；单独读取 engine-sha256.json，
   同时与 profile、回执和实际文件比对，拒绝缺失/额外 DLL、任意二进制及清单删项。
   原始 ZIP 可已移走，不要求保留在下载位置；可信值来自包内 profile。
4. profile.mode 必须等于预检 selected.mode，CUDA 还须有预检选中的 NVIDIA GPU 索引。
   再执行受限版本查询并复核资产，然后生成候选。不把回执的 version 字符串当成本次版本查询。

新 `config create` 不接受旧 `windows_runtime_assets.v1` / `windows_cuda_runtime_assets.v1`；
提示按固定归档生成新目录回执。旧脚本仍读取原回执形状及路径，不重写历史回执或证据。
这种新入口拒绝不改变旧证据读取器，也不赋予任何新增测量资格。

拒绝覆盖必须包括：伪造 profile 摘要、同步篡改回执和清单、替换 server/DLL、删库/增库、
CPU/CUDA 错配、未批准 release、归档逃逸/重名、已有输出、版本超时/非零、输出 IO 失败。
模拟夹具只能验证拒绝逻辑；固定资产验证及 Windows 原生门各自记录，不能互相代替。

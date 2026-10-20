# 平台采集状态与失败语义

[返回主参考](../platform-sampling.md)

## 采集阶段与状态闩锁

- **phase 集合**：采样器只记录当前 `phase` 与 `request_id`，阶段切换由运行时请求路径驱动（probe、warmup、baseline、formal、residual、scoring、finalizing 及 batch 路径的 diagnostic 阶段）。
   读取跨越 phase 或请求边界的样本按保存的 phase 与读取区间归属，不事后重分配（口径见[架构「计量与比较」](../../../cli/docs/architecture.md)）。
- **prepare 观测**：采集器、observer 与 boundary observer 的契约（来源清单、口径、限制、boot 身份、间隔）在采样器构造时以 `stage="prepare"` 一次性写入证据，离线读取按保存的来源归约，不按报告生成机器选择平台语义。
- **`process_lost` 闩锁**：macOS/Linux/Windows 采集器在绑定 PID 的来源变化（启动身份对不上）后置位，该 PID 的 CPU/RSS 后续读数一律缺测，本轮内不再恢复；RSS 读数在读前后各核对一次启动身份。
- **macOS 计数器尺度**：`resources_macos.ResourceSampler` 构造时要求 `ticks_per_second` 为 `None` 或 1000000，其他尺度直接 `ValueError`，防止把 Linux jiffy 口径误接到微秒计数器。
- **传感器 `lost` 闩锁**：SMC 键或 sysfs 路径的来源身份变化后加入 `lost` 集合，后续样本持续缺测，避免把已漂移的来源拼成连续序列。
- **本轮常量**：已写 `environment.start.json` 的轮次，周期行抄写开跑快照的常量字段（含当时的空值）；电源、换页、可用内存每秒新读。
   周期行因此看不到中途变化又在结束前恢复的身份变化，保持到结束的变化由结束快照的新读发现（规则见 [ADR 042](../../../cli/docs/decisions/042-environment-collection-slim.md)）。
- **停止与异常**：`Sampler.stopped` 经 stop event 唤醒等待，不中断已在进行的读取；调度循环任何异常记入 `sampler.failure` 并重抛，请求执行把它转为证据错误（`sampler_failed`/`sampler_stopped_early`），不继续产生无证据结果。
- **开销对照的 off 臂**：总开销 ABBA 的 off 臂不换掉环境观察者——`EnvironmentOnlySampler` 只关资源指标、`BoundaryOnlySampler` 只留请求外守卫，observer 扰动因此不被增量对照计入（切换由 `overhead_runner` 按模式选择）。

## 约束与失败行为

- **缺测语义**：任何读不到的测量保存为 `value: null` 加 `missing_reason`，绝不补 0；字段级失败只影响该行，共同身份失败（如 boot 变化）使整组缺测。
   缺测原因枚举与样本结构见[数据契约](../../../cli/docs/data-contract.md)与[指标定义](../../../cli/docs/experiments/metrics.md)。
- **统一内存不与 RSS 相加**：Apple Silicon 的 GPU 与主机共享内存，`device_macos.displays` 把 GPU 内存标为 `shared_host` 并保留缺测原因；性能解释不得把统一内存与 RSS 相加，独立显存、频率、能耗在 macOS 保持缺测（约束同时见 [MACOS.md](../../../cli/MACOS.md)）。
- **平台工厂规则**：`sys.platform == "darwin"` 且 `proc_root` 为默认 `/proc` 时选 `macos-resource.v1`；`win32` 选 `windows-resource.v1`；显式非默认 `proc_root`（测试根）一律落到 `linux-resource.v2`，Linux `/proc` 采集不进入其他平台的原生通路；不支持的平台抛 `PreflightError("resource_collector_platform_unsupported")`。
   身份与环境的跨平台分派遵循同一 `proc_root` 规则（依据 [ADR 005](../../../cli/docs/decisions/005-macos-runtime.md)）。
- **失败关闭**：所有原生查询失败关闭而非伪造值；对外只抛固定类别 `PreflightError`，调用方不得持久化原始 OS 异常文本。
   macOS 的原生查询直接 `exec` 或走 ctypes，不经 shell（`lsof_records` 显式无 shell）。
   权限失败是不确定性，永不当作"进程已消失"的证据（Windows `ERROR_INVALID_PARAMETER` 才映射为 `service_process_unavailable`）。
- **文件身份一致性**：`hash_file` 在读取前后各 `fstat` 一次，期间 size/mtime/inode 变化或哈希后立即复核不符都拒绝（`identity_file_changed_during_hash`/`identity_file_changed`）；非普通文件与预期摘要不符分别拒绝。
   绑定文件被替换或改动永远不能用缓存身份蒙混。
- **身份核验差异**：macOS 每次完整检查生成一份独立 lsof 视图（`lsof:TCP:LISTEN:pid` 与 `lsof:txt:file_identity`），`mapped_file` 只在同次视图内判断，Metal 还要求正数 offload 参数与已加载的 `libggml-metal.dylib`，模型 GPU 驻留保持 `not_observed`。
   来源字符串描述实际核对的字段，新鲜来源是每次新读，但不证明文件表在整个核验窗口内原子不变，窗口风险保留。
   Linux 用 `/proc/<pid>` 的 exe/cmdline/socket inode 核对；Windows 单次用 `GetExtendedTcpTable` 归属与 `process.memory_maps` 核验 CUDA 库，lab 适配器用双句柄创建 FILETIME 复核防 PID 复用。
- **计数器身份**：每轮计数器读取前后核对 `boot_id`，macOS 还要求页大小与冻结 scale 一致；CPU 启动身份不一致、计数回退、缺段在离线归约（`reduce_external_cpu`）中分别留 `counter_identity_changed`、`counter_regression`、`sampling_gap` 等原因并拒绝资格，不取负、不外插。
   macOS CPU 恒为整数微秒（`clock_ticks_per_second = 1000000`），不与 Linux jiffy 或 Windows FILETIME 比大小。
- **原生电源策略**：性能环境要求冻结并观察稳定的 `macos_power_policy`（新来源 `macos.iokit.power-policy.v1`，旧证据接受 `macos.pmset.power-policy.v1`）；`assess` 对未冻结、不可用、与冻结不匹配或中途变化分别留原因，缺失仍阻断。
   Linux governor/EPP 只读 `/sys` 真实值，不填造；Windows 声明热与电源策略缺测。
- **传感器身份绑定**：SMC 温度绑定注册表 entry id、键与编码，读值须有限且在物理范围内（-273.15 至 200 °C），否则缺测；来源变化后该键永久 `lost`。
   Linux 传感器绑定 sysfs 路径 inode，身份变化同样进入 `lost`；`scaling_cur_freq` 只标注为 requested/driver-reported，不断言是有效执行频率。
   `source_changed` 一经触发不复原，避免把已漂移的来源当作连续序列。
- **SMC 只读边界**：`SMC` 只暴露键枚举（命令 8）、元数据（9）与读值（5）三个 RPC，永不写键或请求特权；80 字节 ABI 尺寸不符直接拒绝；连接经 `weakref.finalize` 回收，读前读后核对注册表身份。
- **高开销探测只在预检**：`system_profiler` 等高开销设备查询只由 `metal_capability` 与设备检测在预检阶段执行，不进入每个采样周期；周期采集只读本机原生计数器与传感器。
- **Windows 批量边界**：`windows-resource.v1` 只采集 RSS 与可用内存，CPU、换页、温度、频率、GPU 与能耗在 `collector` 观测里显式声明缺测，不借用其他平台计数；其声明包含 `strict_performance_comparison_not_qualified`，见 [Windows 批量资源来源](../../../cli/docs/data-contract.md#windows-批量资源来源)。
   KVMem/NInfer 批量不走路该平台采集器，由 `registry.adapter_collector_id` 绑定既有内存采集定义 `windows-memory.v1`。
- **换页口径**：macOS 与 Linux 的换页都是主机级 pages（随证据保存页大小），不归因于模型；macOS 的 `host_statistics64` Swapins/Swapouts 不含压缩内存，pageins/pageouts 不算 swap，Linux 的 `/proc/vmstat` 口径含 swap 与 zram。
   压缩内存不冒充 swap。
- **外部 CPU 语义**：`reduce_external_cpu` 给出的是 host 容量百分比（非单核百分比），"host 忙碌 − 绑定 PID"包含采集器自身、内核与其他工作，子进程不减除；顺序读取不是原子快照，不产生负值截断。
   请求边界的守卫观测（`BoundaryGuard`）是冻结宽度的括号区间均值，不构成干扰的连续缺席证明，比较授权与否按既有资格字段判定。
   Windows lab 适配器用 `assess_windows` 把残余区间绑定到每个请求区间：未冻结容差、请求区间不可得、区间过宽、覆盖率不完整或超过冻结阈值分别留原因，全部请求 eligible 才放行。
- **调度与边界记录的校验**：`schedule.jsonl` 的 `scheduled_ns`/`actual_ns`/`late_ns`/`collector_work_ns` 与 `kind=resource_boundary` 的起止区间都要求非负整数并拒绝 bool，结束不得早于开始；校验规则见[周期环境与调度记录](../../../cli/docs/data-contract.md#周期环境与调度记录)。
- **设备检测与正式采集分离**：`device-check` 保留自己的读取器（macOS 用 `pmset -g batt` 与 `vm_stat` 估计可用内存并标 `memory_available_is_estimate`），正式路径的 AC/电源策略走 IOKit、换页走 `host_statistics64`；两套读取不互相替代，检测结论也不授予运行资格。
- **检测建议的非承诺性**：`recommend` 的 blocked/recommended 只是带原因的下一步指引（如 `insufficient_host_memory`、`output_disk_unavailable`），不下载、不准备、不启动任何模型或引擎；加速器模式还要求后续绑定核验通过才成立。
- **GPU 环境字段的语义**：`environment_snapshot` 在未绑定服务的阶段如实标注——Linux 默认快照为 `not_used`/`cpu_only_scope`（CUDA 后端核验通过后由 `nvidia_snapshot` 结果替换），macOS/Windows 为 `not_observed`/`backend_requires_service_binding`；GPU 证据只在服务绑定核验后由 `metal_capability` 或 CUDA 检查填入，环境声明不预设 GPU 驻留。
- **进程清单诊断的保守性**：`memory-check` 的 RSS 总和不是可释放内存，应用归属可能不完整，快照非原子；截断、权限缺测逐行留原因，权限缺测时结论降级为 partial 而非补值。
- **证据发布的平台差异**：`platform_io` 保持独占创建与不跟随打开；Windows 长路径加 `\\?\` 前缀，无 POSIX 目录 fsync，快照用 `MoveFileExW(WRITE_THROUGH)` 发布，不声称与 Linux 目录 fsync 相同的元数据崩溃耐久（`durability()` 按平台如实声明）。
- **证据资格**：实现、模拟验证与真机证据分开记录，软件/模拟验证不授予真机或正式性能比较资格；历史覆盖与当前候选的验证范围见[平台状态](../../../cli/docs/platforms.md)，资格结论遵循[证据血缘规则](../../../cli/docs/data-contract.md#证据血缘与比较结论)。

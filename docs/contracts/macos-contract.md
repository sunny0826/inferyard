# macOS 平台契约

依据 [ADR 005](../decisions/005-macos-runtime.md)。

## 接口与职责

| 接口                           | 责任与不变量                                                                                                                                                                                         |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `platforms/macos_identity.py`  | `process_start_ticks(pid)`、`process_arguments(pid)`、`verify_listener(pid, address, port)`、`verify_process(...)`、`memory_available()`、`environment_snapshot()`；公开签名与现有 identity 分派兼容 |
| `platforms/identity.py`        | 仅默认原生根目录且 Darwin 时分派；显式测试 `/proc` 根保留 Linux 行为；hash、端点、本机锁和进程前后身份规则不变                                                                                       |
| `platforms/telemetry.py`       | Darwin RSS/可用内存使用原生来源，PID 变化则缺测；继续写既有 memory envelope                                                                                                                          |
| `platforms/resources.py`       | 平台采集工厂 `ResourceSampler(...)` 与 `resource_collector_id()`；Linux 保留原类和定义                                                                                                               |
| `platforms/resources_macos.py` | 与 Linux ResourceSampler 同一 `collect`、`boundary`、`idle_cycle_rss` 接口，采集定义 `macos-resource.v1`                                                                                             |
| `platforms/external_cpu.py`    | 按执行平台采集、按保存的 source 离线归约；禁止按读报告机器选择证据语义                                                                                                                               |
| runtime / extensions           | 单次、实验、开销与条件扩展使用工厂；不复制请求生命周期；缺少强制传感器仍拒绝                                                                                                                         |
| contracts / adapter            | `metal` 应有正数 GPU offload、原生 Apple/Metal 能力与服务绑定，未知平台组合拒绝；不因硬件有 Metal 宣称模型已在 GPU 驻留                                                                              |

共享应用类型仍仅在 `application/types.py`。不改变用户命令或退出码。psutil 仅在原生路径延迟加载，help/version/schema 不加载实时后端。

## 证据与计量

- macOS 启动身份为锁定的 psutil 7.2.2 原生原始创建时间转换的整数微秒，使用其内部 `create_time(monotonic=True)` 避免公共 API 的校时修正，持久化明确来源；只作同平台 PID 复用检查，不与 Linux jiffy 或 Windows FILETIME 比大小。内部 API 不可用时失败关闭。
- CPU 的 user/system 秒转换为整数微秒，`clock_ticks_per_second = 1000000`；RSS 和系统内存为 bytes；换页为 host pages，保存实际页大小，压缩内存不冒充 swap。
- 主机启动身份使用原生启动时间/boot session；读前后变化、计数回退、缺段、权限错误分别拒绝或缺测。
- 温度为只读 AppleSMC 原始摄氏浮点值，绑定注册表 ID、键与编码；未知编码、来源变化和读取失败为缺测。频率、能耗和独立显存仍缺测；统一内存不能与 RSS 相加。详见[性能契约](macos-contract.md#原生温度与完整开销)。
- 环境保留真实 AC、平台、内核、CPU、内存和换页；Linux governor/EPP 为 null。性能资格要求冻结并观察稳定的 `macos_power_policy`，按保存的平台核验；现有显式 `allow_unknown_environment` 仍只允许有限质量运行，实际 AC/政策不匹配仍阻断。
- 采集 on/off 的来源、定义和目标绑定应一致；不继承 Linux 或旧源码的开销资格。独立总观察开销 guardian 的温度要求不降低。

## 兼容、替代与风险

v3 现有样本原样可读，新 collector 使用同一计量结构但独立来源。不得为避免更新离线路径把 macOS 样本贴成 Linux。原生查询有权限和版本差异，失败关闭而非伪造值。高开销设备 profiler 只用于预检，不能在每个采样周期执行。

## 原生温度与完整开销

文中的“一轮测试”表示完整执行一次冻结任务；四轮对照依次关闭、开启、开启、关闭被比较的采集。ON / OFF 分别表示开启 / 关闭采集，原始协议字段 `arm` 保留。

依据：[ADR 006](../decisions/006-macos-performance-prerequisites.md)。活动契约保持 v3；新增可选结构与独立来源，旧原件不改。

| 责任     | 接口与证据                                                                                                                                                 |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 原生 SMC | `SMC.keys()` / `info(key)` / `read(key)`；只读 AppleSMC，80 字节 ABI，完整返回状态检查，注册表身份绑定，句柄回收                                           |
| 温度     | `MacSensors.metadata()` / `collect(phase, request_id)`；`macos-smc.v1`、`smc_key_reported`、摄氏度、原始 float；未知键类型、非有限值、源变化为缺测         |
| 电源     | `macos_power_policy` 字段不变。新采集 `source=macos.iokit.power-policy.v1`；旧证据和已冻结配置仍接受 `macos.pmset.power-policy.v1`。同一次运行的冻结条件与观测必须是同一个 source。读失败为缺测，不回退 `pmset`，不沿用上一笔 |
| 环境资格 | 按保存的平台选择身份字段和电源验证；Darwin 应有完整且稳定的原生策略及冻结绑定，Linux CPUFreq 验证不变；缺源不豁免                                          |
| 正式通路 | 固定单槽直接复用 `run_trial` 的模板/token/有效参数/空闲验证与实际适配器；不扩大并发或原生工具支持                                                          |
| 性能证据 | 旧比较的总观察 live 包须通过离线封存重算，绑定同源码、配置、题包、case 顺序与同 boot/早于目标；当前总开销分级见职责身份契约                                        |

共同 guardian 继续运行在独立进程，覆盖四轮测试 setup 到 flush/fsync/seal/close，温度要求不降。温度值与频率/能耗/GPU 驻留不互推；统一内存与 RSS 不相加。

`total_observer_control.v2` 增加 `trial_plan_path`、`trial_plan_sha256`、`trial_id`，冻结并核验正式 plan、精确配置/题包哈希和固定题序。四轮测试直接调用同一个 `run_trial`；on 产物保留普通 trial 原件，off 原始快照/事件保存在独立 baseline 证据。源身份、warmup/probe/基线等待/生成配方不变，观察采集是唯一切换因素。on 计时包含最终封存、关闭和离线重建；共同 guardian 与 HostLock 成本不宣称为零。

比较读取增量对照根目录的 `total-control-binding.json`（独立版本 `total_observer_binding.v1`，封存包绝对路径与 manifest 哈希），重算 v2 live 包。核验绑定三项：on 子运行及 off 原始缓冲、同 boot 且四轮测试早于目标、正式 plan/workload/生成输出精确绑定。v1、fixture、缺测、过期源码、缺少测试轮次、不同目标通路均拒绝。Linux 原生策略和各指标独立门继续有效。

决策：平台语义由来源明确声明，不能借用 Linux 标识。替代方案：在 macOS 填造 governor/EPP 或放宽门槛，拒绝。风险：私人 SMC ABI、电源字段可用性及采集成本；每种失败须保留原因，真实 ABBA 未通过时资格为 false。

## 同次身份视图

依据 [ADR 007](../decisions/007-macos-process-view.md)。

### 接口与职责

| 接口                                                  | 冻结语义                                                                                                                                                                         |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `macos_process.lsof_records(pid, selectors)`          | 保持命令、NUL 解析、所有者字段、两秒超时、无 shell、错误失败关闭；空 selectors 仅按 PID 读取全文件视图                                                                           |
| `macos_process.mapped_file(pid, identity, rows=None)` | 保留两参数独立调用；未提供 rows 时按 `-d txt` 新读。提供 rows 时只在同次 `verify_process` 的局部视图中判断，不触发第二次查询；严格要求 PID、`f = txt`、REG、device 与 inode 相符 |
| `macos_identity.verify_listener(pid, address, port)`  | 公开签名与独立新读不变，前后启动身份检查和既有错误语义不变                                                                                                                       |
| 私有监听筛选                                          | 同一视图应仅有一个匹配目标地址族 / 地址 / 端口的 TCP LISTEN，且属于绑定 PID；重复匹配、错误所有者与缺失均拒绝                                                                  |
| `macos_identity.verify_process(...)`                  | 公开签名不变；初始启动身份、实际 exe、单次视图读后身份、命令参数独立身份、模型参数文件、slots_debug、监听、Metal 和最终启动 / 文件身份逐项核对                                   |
| 私有 Metal 核验                                       | 接收同次视图；仍核验正数 offload、manifest、库内容哈希、库 `txt` 映射与文件未变                                                                                                  |

每次完整身份检查生成独立的局部 rows，不引入实例、模块或全局缓存。独立 `verify_listener` / `mapped_file` 不借用上次完整检查结果。实际返回字段、`listener_source = lsof:TCP:LISTEN:pid`、Metal `source = lsof:txt:file_identity` 保持，源字符串描述实际核对字段，不保证查询是原子快照。

### 兼容、替代与风险

不修改 v3、Schema、采集定义、共享应用类型、CLI、锁、dirty、取消 / 排空、评分、题包、采样间隔或 flush/fsync。Linux / Windows 分派不动，旧证据按保存来源读取；整体测量源码身份改变，旧实测资格不迁移。

替代三个独立查询只消除同次调用的重复启动与读取；环境、Metal 文件哈希及观测持久化成本仍在。全视图的无关文件不会提供映射证明，解析异常拒绝且不泄露原始内容。启动身份检查不能证明文件表在整个窗口内原子不变，窗口风险须保留；新鲜来源是每次新读，而不是宣称完成时所有状态同时读取。

接口变动或新增缓存须更新本契约，不可在测试失败后静默放宽核验；数据与资格继续遵循上述计量与完整开销规则。

## 环境与换页读取

依据 [ADR 008](../decisions/008-macos-environment-reads.md) 与 [ADR 042](../decisions/042-environment-collection-slim.md)。跨平台的抄写字段和落盘投影见[数据契约](../data-contract.md#周期环境与调度记录)。

### 接口与观测权威

| 接口                                                | 冻结语义                                                                                                                 |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `macos_native.sysctl_string(key)`                   | 每次只读原生字符串键；有界 4096 字节，成功长度及尾 NUL / UTF-8 校验，返回裁剪后的字符串或 `None`；不缓存值，不用于整数键 |
| `macos_identity.environment_snapshot()`             | 可选的本轮常量默认不传。未传时整份新读。传入后按下方「本轮常量」抄写。电源与换页每次新读，失败为缺测                     |
| `macos_native.sysctl_text(key)` / `boot_id()`       | 保留现有独立读取和 UUID 校验；其他设备能力查询不切换。周期行已有 `boot_id` 时，该行不再读 boot                           |
| `ResourceSampler.counters(pid, ticks, capture=...)` | 签名与 CPU / 换入 / 换出三行顺序不变；CPU 自身读出保留。两个 swap 值同次共用一次 `host_statistics64` 及同一读取起止时间，每次调用重新读取 |
| 换页共同身份                                        | boot 在原生读取前 / 后核对；页大小与冻结 scale 相符，系统换页不归因于模型、不把 pageins/pageouts 当 swap                 |
| 换页单字段                                          | 来源精确匹配，值为非负整数且拒绝 bool；某字段失败为对应行 `null` 与原因，共同身份失败则两行均缺测                        |
| 保存与读取                                          | 保留 `macos-resource.v1`、pages、页大小、主机范围、读取区间 / 中点口径。新采集来源是 `host_statistics64:swapins` / `swapouts`。旧证据里的 `vm_stat:Swapins` / `Swapouts` 按原字节读取，不重新解释 |

### 本轮常量

只在 `environment.start.json` 已经写入、并且这次调用拿到了那份开跑快照时，周期行才抄常量。结束快照、预检、`device-check`、请求边界，以及没写开跑快照的采样，整份新读。

macOS 上，开跑快照已有 `cpu_model` 时周期行不读 brand string；已有 `boot_id` 时不读 boot；已有 `os_release` 时不读系统版本。可用内存仍每秒读。`memory_total_bytes` 已抄到时，快照总量用抄到的值。

电源每次从 IOKit 新读，来源 `macos.iokit.power-policy.v1`，`ac_sources` 的键是 `iokit:providing-power`。供电源只认 `AC Power` 与 `Battery Power`。活动区的 `LowPowerMode` 只接受 0 或 1；电源模式先看 `powermode`，没有再看 `PowerMode`，都没有则为 `unsupported`。换页每次从 `host_statistics64` 新读 Swapins / Swapouts。两处失败都是缺测，不回退 `pmset` 或 `vm_stat`，不沿用上一笔。

### 边界与风险

动态电源、可用内存和换页不跨请求、周期或进程缓存。本轮常量只在同一轮的周期采样里复用开跑那一次，不跨进程。CPU 计数与 swap 两行可来自不同原生区间，swap 两行共享区间；不能把这种组织误写为整个资源包是原子快照。字节上限不足或格式异常不使用截断字符串，不启动备用命令，也不沿用旧值。

不改 CLI / 应用共享类型、v3、题包 / 评分、采样间隔、环境边界、SafetyGuard、独立 guardian、身份视图、锁 / dirty、取消 / 排空、flush/fsync 或严格默认阈值。`device-check` 的读取不在这次更换内。Schema 同时接受新旧电源来源和换页来源；变更用生成器同步并保留 ID，不手改生成 JSON。

同一次运行里来源必须相同。已经冻成 `macos.pmset.power-policy.v1` 的条件与新观测对不上，需要重新冻结。不承诺完整开销一定达到冻结阈值。旧证据保持原字节，读取不调用本机后端重新解释它；完整开销、输出工作量和资格另冻验收。

当前 comparison v3/v4 的总开销与可选增量诊断、跨目标复用条件见[职责身份契约](scoped-measurement-contract.md)。

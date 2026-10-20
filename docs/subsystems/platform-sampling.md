# 平台采集与设备身份

本子系统证明一次测量发生在哪台设备、哪个进程、哪组计数器上，并按绝对调度点采集资源样本与周期环境快照。
它只读不改：不启动、停止或信号任何被测服务。
`engine_fit_*` 模块的引擎诊断定义不属于这里，见 [engine-fit 引擎诊断](engine-fit.md)；采样任务由运行时创建与停止，见 [运行时执行与引擎适配](runtime-execution.md)；日志落盘与封存见 [证据、封存与离线读取](evidence-storage.md)；设备检测命令的 CLI 分派见 [CLI 与应用分发](cli-dispatch.md)。
覆盖范围为 `cli/src/inferyard/platforms/` 中除 `engine_fit*` 以外的模块，加上运行时侧的两个调度协作者（`environment_schedule.py`、`environment_observer.py`）。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 平台工厂 | 按 `sys.platform` 与 `proc_root` 选择采集器定义，延迟导入原生后端 | `resource_collector_id`、`ResourceSampler` 在 [`cli/src/inferyard/platforms/resources.py`](../../cli/src/inferyard/platforms/resources.py) |
| 跨平台身份分派 | 统一的进程/监听/环境入口，按平台转发到原生实现；显式 `proc_root` 保留 Linux 语义 | `process_start_ticks`、`verify_listener`、`verify_process`、`environment_snapshot`、`memory_available`、`static_preflight`、`hash_file`/`FileIdentity` 在 [`cli/src/inferyard/platforms/identity.py`](../../cli/src/inferyard/platforms/identity.py) |
| macOS 进程身份 | 启动身份、exe/模型文件 inode 核验、lsof 监听与 Metal 库映射核验 | `verify_process`、`verify_listener`、`metal_capability` 在 [`cli/src/inferyard/platforms/macos_identity.py`](../../cli/src/inferyard/platforms/macos_identity.py)；`lsof_records`、`matching_listener`、`mapped_file` 在 [`cli/src/inferyard/platforms/macos_process.py`](../../cli/src/inferyard/platforms/macos_process.py) |
| macOS 原生小查询 | 有界 sysctl/boot session/换页快照，无 shell | `sysctl_string`、`sysctl_text`、`boot_id`、`swap_snapshot`、`query` 在 [`cli/src/inferyard/platforms/macos_native.py`](../../cli/src/inferyard/platforms/macos_native.py) |
| Windows 进程身份 | FILETIME 启动身份、exe 路径、监听归属与 CUDA 库映射核验 | `verify_process`、`verify_listener` 在 [`cli/src/inferyard/platforms/windows_identity.py`](../../cli/src/inferyard/platforms/windows_identity.py)；`process_start`（GetProcessTimes FILETIME）在 [`cli/src/inferyard/platforms/windows_api.py`](../../cli/src/inferyard/platforms/windows_api.py) |
| Windows lab 进程树 | KVMem/NInfer 的句柄级创建 FILETIME 复核与进程退出检查 | `ProcessHandle`、`verify_process_binding` 在 [`cli/src/inferyard/platforms/windows_lab_process.py`](../../cli/src/inferyard/platforms/windows_lab_process.py)；`bind_files`、`verify_process` 在 [`cli/src/inferyard/platforms/lab_identity.py`](../../cli/src/inferyard/platforms/lab_identity.py) |
| 采样基类与调度 | phase 标记、停止唤醒、绝对调度点与丢样记录 | `Sampler`、`read_rss`、`sample_memory` 在 [`cli/src/inferyard/platforms/telemetry.py`](../../cli/src/inferyard/platforms/telemetry.py)；`run_sampler`、`advance` 在 [`cli/src/inferyard/runtime/environment_schedule.py`](../../cli/src/inferyard/runtime/environment_schedule.py) |
| 环境观察者 | 外部 CPU 观测、请求边界守卫、off 臂共用的 observer 契约 | `EnvironmentObserver`、`EnvironmentOnlySampler`、`observer_contract` 在 [`cli/src/inferyard/runtime/environment_observer.py`](../../cli/src/inferyard/runtime/environment_observer.py) |
| Linux 采集 | `/proc` 计数器、vmstat 换页、thermal/CPUFreq 传感器 | `ResourceSampler`、`read_cpu` 在 [`cli/src/inferyard/platforms/resources_linux.py`](../../cli/src/inferyard/platforms/resources_linux.py)；`LinuxSensors` 在 [`cli/src/inferyard/platforms/sensors_linux.py`](../../cli/src/inferyard/platforms/sensors_linux.py) |
| macOS 采集 | CPU/RSS/可用内存/换页计数器与只读 AppleSMC 温度 | `ResourceSampler` 在 [`cli/src/inferyard/platforms/resources_macos.py`](../../cli/src/inferyard/platforms/resources_macos.py)；`read_cpu`、`capture`、`ExternalCpu` 在 [`cli/src/inferyard/platforms/external_cpu_macos.py`](../../cli/src/inferyard/platforms/external_cpu_macos.py)；`SMC` 在 [`cli/src/inferyard/platforms/smc_macos.py`](../../cli/src/inferyard/platforms/smc_macos.py)；`MacSensors` 在 [`cli/src/inferyard/platforms/sensors_macos.py`](../../cli/src/inferyard/platforms/sensors_macos.py) |
| 外部 CPU 归约 | host 减绑定 PID 的残余忙碌区间计算与离线校验 | `capture`、`reduce_external_cpu` 在 [`cli/src/inferyard/platforms/external_cpu.py`](../../cli/src/inferyard/platforms/external_cpu.py)；Windows 请求绑定评估在 [`cli/src/inferyard/platforms/external_cpu_windows.py`](../../cli/src/inferyard/platforms/external_cpu_windows.py) |
| Windows 批量采集 | RSS 与可用内存，其余指标显式缺测 | `ResourceSampler`、`UNAVAILABLE` 在 [`cli/src/inferyard/platforms/resources_windows.py`](../../cli/src/inferyard/platforms/resources_windows.py) |
| 原生电源策略 | IOKit AC/电源模式读取、冻结比对与稳定性评估 | `read_native`、`native_policy`、`valid`、`assess` 在 [`cli/src/inferyard/platforms/power_macos.py`](../../cli/src/inferyard/platforms/power_macos.py)；Linux CPUFreq 清单与评估在 [`cli/src/inferyard/platforms/cpu_policy.py`](../../cli/src/inferyard/platforms/cpu_policy.py) |
| 本轮常量 | 开跑快照常量抄写进周期行 | `capture_constants`、`apply_constants`、`CONSTANT_FIELDS` 在 [`cli/src/inferyard/platforms/environment_constants.py`](../../cli/src/inferyard/platforms/environment_constants.py) |
| 设备检测 | 硬件快照、GGUF 发现、执行模式建议（保守估计） | `inspect_device`、`hardware_snapshot`、`recommend`、`discover_models`、`nvidia_snapshot` 在 [`cli/src/inferyard/platforms/device_preflight.py`](../../cli/src/inferyard/platforms/device_preflight.py)；`snapshot` 在 [`cli/src/inferyard/platforms/device_host.py`](../../cli/src/inferyard/platforms/device_host.py) 与 [`cli/src/inferyard/platforms/device_macos.py`](../../cli/src/inferyard/platforms/device_macos.py)；`read_metadata` 在 [`cli/src/inferyard/platforms/gguf_metadata.py`](../../cli/src/inferyard/platforms/gguf_metadata.py) |
| 应用内存诊断 | 独立只读进程清单（macOS/Windows），带预算与截断记录 | `collect`、`observe` 在 [`cli/src/inferyard/platforms/memory_macos.py`](../../cli/src/inferyard/platforms/memory_macos.py) 与 [`cli/src/inferyard/platforms/memory_windows.py`](../../cli/src/inferyard/platforms/memory_windows.py)；`owner_session` 在 [`cli/src/inferyard/platforms/memory_windows_api.py`](../../cli/src/inferyard/platforms/memory_windows_api.py) |
| 证据文件发布 | 独占创建、不跟随打开、目录 fsync 与快照发布 | `open_nofollow`、`publish`、`sync_directory`、`durability` 在 [`cli/src/inferyard/platforms/platform_io.py`](../../cli/src/inferyard/platforms/platform_io.py) |

关键运行状态的事实源：采集器定义由 `resource_collector_id` 在构造时确定并写入 `collector` 观测；
普通 macOS/Linux single 与 batch 由 `registry.adapter_collector_id` 选择同一资源采集器，
不再只记录内存；Windows 及 lab 例外见[数据契约](../../cli/docs/data-contract.md#macos-来源与后端)。
`EnvironmentObserver.__init__` 锁定本轮 `boot_id`、`tick_rate` 与 `page_size`；
`process_lost` 是进程来源变化后的永久闩锁，触发后该 PID 的后续计数器读数一律缺测；
本轮常量由 `run_trial` 调 `use_start_environment` 注入采样器，周期行据此抄写常量。
运行时的安全守卫（`runtime/safety.py` 的 `SafetyGuard`）复用本子系统的温度传感器（只取 temperature 指标）与外部 CPU 来源做低 CPU/温控检查，守卫策略本身归 [运行时执行与引擎适配](runtime-execution.md)。
状态与配置的字段语义见[周期环境与调度记录](../../cli/docs/data-contract.md#周期环境与调度记录)，macOS 冻结接口见 [macOS 平台契约](../../cli/docs/contracts/macos-contract.md)。

## 运行路径

1. **设备检测**：`device-check` 命令经 `inspect_device` 汇总 `device_host.snapshot`/`device_macos.snapshot` 的硬件探测（CPU、内存、AC、GPU，各带来源与缺测字段）、`discover_models` 的 GGUF 元数据读取和 `recommend` 的执行模式建议。
   建议只是保守筛选估计（`unified_memory_fit_estimate` 等），不承诺运行时占用，活体预检仍以 `config bind` 后的核验为准。结果被拒或无模型时退出码为 2。
2. **预检身份核验**：`static_preflight` 先 `hash_file` 计算模型、引擎二进制、模板与 manifest 内动态库的文件身份（`sha256` + device/inode/mtime），再按平台分派 `verify_process` 逐项核对启动身份、exe、启动参数、模型参数文件与监听归属。
   启动参数比对前经 `sanitized_arguments` 剔除 `--api-key` 等凭据类选项，证据只保存脱敏后的参数。
   端点 URL 在预检经 `service_origin` 解析为数值地址与端口以定位监听；适配器发送前再用 `resolve_loopback_origin` 固定数值 origin 并要求 `localhost` 解析不越出 loopback 段，防 DNS 重绑定。
   KVMem/NInfer 适配器在 Windows 上改走 `lab_identity.verify_process` + `windows_lab_process.verify_process_binding`；CUDA 后端额外要求 `nvidia_snapshot` 观察到设备。
   随后 `environment_snapshot` 采集环境快照、`check_resources` 核对冻结的内存/磁盘门槛，环境准入由 `config.environment_binding.admission` 判定，阻断即 `frozen_environment_mismatch`。
   命令入口见 [CLI 与应用分发](cli-dispatch.md)。
3. **采样器装配**：`run_trial` 通过工厂 `ResourceSampler(store, config)` 创建采样器（定义见 `resource_collector_id`），构造时写入 `collector` 观测（来源清单、口径、限制、boot 身份、页大小、调度间隔；`EnvironmentObserver` 子类另写 `observer` 契约），随后注入开跑环境常量并以 `asyncio.create_task(sampler.run())` 启动调度循环。
   采样任务异常先行结束时请求执行以证据错误中止，不继续产生无证据结果。
4. **调度循环**：`run_sampler` 用事件循环单调时钟维护绝对调度点 `due`；每轮记录 `scheduled_ns`/`actual_ns`/`late_ns`/`collector_work_ns` 到 `schedule.jsonl`。
   落后超过一个周期时 `advance` 直接跳到下一个未来调度点，不为错过的调度点补造样本（丢样本身由间隔缺口在离线归约中留原因）。
   基类 `Sampler.run` 保留既有 memory envelope 循环，`registry.collector_factory` 的 `linux-memory.v1`/`macos-memory.v1`/`windows-memory.v1` 走它；新的资源采集器统一走 `run_sampler`。
5. **周期采集**：每个资源调度点调用 `collect(pid, ticks)`，一行样本只含一个指标并带自己的读取区间与来源：
   - `sample_memory` 读系统可用内存与服务 RSS，RSS 读前后各核对一次启动身份，身份变化即缺测；
   - `counters` 读 CPU 与主机换页计数器（macOS/Linux）。macOS 的 CPU 行来自 psutil user/system 秒转整数微秒，换页两行共用一次 `host_statistics64` 读取与同一读取区间；Linux 的 CPU 行来自 `/proc/<pid>/stat`，换页两行共用一次 `/proc/vmstat` 读取；
   - 传感器类（`LinuxSensors`/`MacSensors`）读温度与频率，每个来源独立绑定身份；
   - `external` 追加 host−PID 外部 CPU 观测到 `external-cpu.jsonl`，供离线归约与安全守卫共用同一校验语义。
   Windows 批量采集器只读 RSS 与可用内存，其余指标保持声明式缺测。
6. **请求边界**：请求发送前 `before_request` 触发 `BoundaryGuard` 记录 `request-environment.jsonl`（`before_send` 边界的完整环境快照加 host−PID CPU）；到达捕获与终态时 `boundary("request_start"|"request_end"|"idle")` 追加边界计数器样本与 `kind=resource_boundary` 调度记录，`request_end` 还额外记一次 `after_terminal` 边界观测；确认空闲后 `idle_cycle_rss` 以 `phase=residual` 记录一轮 RSS。
   phase 与 request_id 由请求执行路径在 probe/warmup/baseline/formal/residual/scoring 之间切换，样本按保存的 phase 归属，不跨边界外插。
7. **周期环境**：环境调度点每秒整读或按本轮常量抄写生成 `environment.jsonl` 行；macOS 电源与换页每次新读，不回退旧来源（规则见 [macOS 平台契约](../../cli/docs/contracts/macos-contract.md) 与 [ADR 042](../../cli/docs/decisions/042-environment-collection-slim.md)）。
8. **收尾**：`finish_trial` 停止采样任务（批量路径先把 phase 切到 `finalizing`，单次路径取消任务并把采样器异常映射为退出码 4）；样本与观测的 flush/fsync、封存与离线读取归 [证据、封存与离线读取](evidence-storage.md)。
9. **应用内存诊断**：`memory-check` 命令先过平台门（非 Darwin/Windows 直接退出 2），再经 `memory_macos.collect`/`memory_windows.collect` 在有界预算（`deadline_seconds=5`、`max_processes=4096`）内枚举进程清单。
   macOS 绑定原始内核启动身份与父进程身份，Windows 另取 owner/session 与 private commit。
   清单在隔离子进程中运行，截断与缺测逐行留原因，保护请求由决策层保守评估，诊断从不进入正式执行路径。
   产物结构见 [memory-check 契约](../../cli/docs/contracts/memory-check-contract.md)。

## 采集阶段与状态闩锁

见[采集阶段与状态闩锁](platform-sampling/semantics.md#采集阶段与状态闩锁)。

## 约束与失败行为

见[约束与失败行为](platform-sampling/semantics.md#约束与失败行为)。

## 验证

```sh
cd cli
mise exec -- uv run --frozen pytest -q \
  tests/unit/test_resources.py \
  tests/unit/test_macos_resources.py \
  tests/unit/test_macos_identity.py \
  tests/unit/test_macos_process_view.py \
  tests/unit/test_macos_native_policy.py \
  tests/unit/test_macos_power_qualification.py \
  tests/unit/test_macos_sysctl.py \
  tests/unit/test_identity.py \
  tests/unit/test_telemetry.py \
  tests/unit/test_cpu_policy.py \
  tests/unit/test_sensors_linux.py \
  tests/unit/test_sensors_macos.py \
  tests/unit/test_external_cpu.py \
  tests/unit/test_windows_resources.py \
  tests/unit/test_device_preflight.py \
  tests/unit/test_device_platforms.py \
  tests/unit/test_memory_check_macos.py \
  tests/unit/test_memory_check_windows.py \
  tests/unit/test_windows_lab_process.py
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen ruff format --check src tests scripts
```

这些回归覆盖模拟与 fixture 路径（含显式 `proc_root` 测试根）；macOS/Windows 原生读取的真机证据范围另按[平台状态](../../cli/docs/platforms.md)登记，不由单测通过推定。
命令形式对照 [CONTRIBUTING](../../cli/CONTRIBUTING.md) 的「Changes and verification」一节与根级 mise tasks（`test`/`lint`）。

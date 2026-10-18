# 运行时执行与引擎适配

`runtime/` 与 `adapters/` 在操作者已启动并声明身份的本地模型服务上执行冻结的请求序列：主机共享锁与 crash 持久的 dirty 恢复、服务身份预检驱动的执行准入、请求身份先落盘再发送、取消与按冻结期限排空、引擎传输与响应/用量解析。它不编译配置、不写证据日志本体、不采集平台指标，也不启动或停止外部服务。

边界：命令参数与分派见 [CLI 与应用分发](cli-dispatch.md)；单次/批量配置编译为 workload 与 trial 见 [配置、契约与冻结计划](config-plans.md)；日志追加、封存与账本见 [证据、封存与离线读取](evidence-storage.md)；进程身份核验、空闲来源与平台采集见 [平台采集与设备身份](platform-sampling.md)；engine-fit 诊断路径复用本参考的主机锁与恢复机制，其余见 [engine-fit 引擎诊断](engine-fit.md)。通用层职责见 [架构](../../cli/docs/architecture.md) 的「请求与运行」节。

本参考覆盖 `runtime/` 与 `adapters/` 内的正式与 lab 通路。`runtime/engine_fit.py` 与 `adapters/engine_fit.py` 是 engine-fit 独立诊断通路：同样经过 `HostLock` 与恢复核验，但产物独立封存、不进入正式评分，细节归 [engine-fit 引擎诊断](engine-fit.md)。

「职责与事实源」「运行路径」「约束与失败行为」三节正文已移入 `runtime-execution/` 目录；其余各节保留在本页。

## 职责与事实源

正文见 [职责与事实源](runtime-execution/responsibilities.md#职责与事实源)。

## 运行路径

正文见 [运行路径](runtime-execution/trial-lifecycle.md#运行路径)。

## 约束与失败行为

正文见 [约束与失败行为](runtime-execution/constraints.md#约束与失败行为)。

### 主机锁与 dirty 恢复

正文见 [主机锁与 dirty 恢复](runtime-execution/constraints.md#主机锁与-dirty-恢复)。

### dirty 状态转换

正文见 [dirty 状态转换](runtime-execution/constraints.md#dirty-状态转换)。

### 请求准入与执行

正文见 [请求准入与执行](runtime-execution/constraints.md#请求准入与执行)。

### 适配器边界

正文见 [适配器边界](runtime-execution/constraints.md#适配器边界)。

## 长上下文速度接入

专项继续使用原 batch/trial 执行器；Journal 把冻结 experiment.profile 传给输入校验器。
[`runtime/context_speed.py`](../../cli/src/inferyard/runtime/context_speed.py) 提供 profile 准入，
已有专项 batch、resume、单 run 与 rerun 在发送前拒绝；结果分派为 speed-results 提示，
不调用通用 repetition_metrics。预算与额外准备请求见
[CLI 专项接口](../../cli/docs/cli-surface.md#长上下文速度专项)。

## 验证

以下命令在仓库根的 `cli/` 目录内执行（与 [mise 任务](../../mise.toml) `test`/`lint` 同一形式）。单元测试全部使用 fixtures 与模拟服务，属软件级回归，不授予真机资格。`cli/scripts/` 下的 `verify_*` 脚本可能启动服务或发送真实请求，本子系统的常规验证不依赖它们，故不列入。

```sh
# 主机锁、dirty 恢复与状态兼容
uv run --frozen pytest tests/unit/test_lock.py tests/unit/test_host_lock_compatibility.py

# 请求生命周期、trial 编排、duration 窗口与排空、安全停止、服务 drain
uv run --frozen pytest tests/integration/test_trial_requests.py \
  tests/integration/test_trial_runner.py tests/integration/test_trial_profiles.py \
  tests/unit/test_duration_driver.py tests/unit/test_duration_send_admission.py \
  tests/unit/test_safety.py tests/unit/test_safety_trace.py tests/unit/test_service_drain.py

# 适配器解析：Prism SSE、lab 生成/观察/参数、native 观察
uv run --frozen pytest tests/unit/test_prism.py tests/unit/test_lab_generation.py \
  tests/unit/test_lab_generation_plain.py tests/unit/test_lab_observation.py \
  tests/unit/test_lab_observation_tracker.py tests/unit/test_lab_adapters.py \
  tests/unit/test_native_adapters.py

# 长度准备与固定输出
uv run --frozen pytest tests/unit/test_length_builder.py tests/unit/test_fixed_output.py \
  tests/unit/test_input_lengths.py

# 批量执行编排与容量/缓存协议（集成级，含模拟服务）
uv run --frozen pytest tests/integration/test_batch_runner.py \
  tests/integration/test_capacity_stop.py tests/integration/test_cache_protocol.py \
  tests/integration/test_batch_process_interruptions.py

# 样式与全量回归
uv run --frozen ruff check src tests scripts
uv run --frozen pytest
```

## 与既有文档的差异

- [架构](../../cli/docs/architecture.md)「请求与运行」称无法核验的有效参数为 `unknown`；实现中（`NativeObservation.native_parameters`）这些字段记为 `effective: null`、`verification: "not_observed"`、`missing_reason: "effective_parameter_unavailable"`，证据内不存在字面 `unknown` 值。本文按实现描述。

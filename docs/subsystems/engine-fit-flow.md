# engine-fit 运行路径

本文属于 [engine-fit 引擎诊断](engine-fit.md)。

## 运行路径

以下路径均在 [通用层职责](../../cli/docs/architecture.md) 约束下执行；跨子系统的锁、
排空与证据规则见 [运行时执行与引擎适配](runtime-execution.md) 与
[证据、封存与离线读取](evidence-storage.md)。

### `engine-fit plan`（离线冻结）

1. CLI 解析参数后由 `application/engine_fit.py` 懒分派到 `config/engine_fit.py` 的
   `prepare`；先做廉价输入校验（case 数量、prompt 大小、token/超时/重复上限、覆盖原因
   合法性），再哈希可能很大的模型权重。
2. `model_asset_manifest`（新计划）或 `model_manifest`（plan.v1 遗留目录计划）生成资产
   清单；`host_identity` 读取 OS 机器身份与硬件描述并计算主机哈希。
3. 按是否使用停止覆盖、资产形态与引擎集合选择 definition（v1–v4），冻结 cases、
   parameters 与 `plan_id`（去除自身字段的 canonical JSON SHA256），经 `validate_plan`
   复核后原子写入 `plan.json` 到新目录。不发送任何请求，也不启动任何服务。

### `engine-fit run`（绑定一个操作者启动的服务）

1. `runtime/engine_fit.py` 的 `execute` 先做平台/引擎准入：Windows 仅 llama-cpp，
   mlx-lm/lmstudio 仅 Darwin，ollama 固定阻断；LM Studio 强制要求 `--lms-path` 与
   `--models-root`，其他引擎拒绝这两个选项。输出目录必须不存在且不得位于模型目录内。
2. `load_plan` 严格校验并重算计划哈希；核对当前 `host_identity` 与计划主机哈希、
   `runtime_manifest` 与计划模型清单一致。
3. 获取 `HostLock`；调用 `bind_service` 建立 binding（平台分派：Linux 走 `/proc`，
   Darwin 走 [`engine_fit_macos.py`](../../cli/src/inferyard/platforms/engine_fit_macos.py)，
   原生 Windows 走
   [`engine_fit_windows.py`](../../cli/src/inferyard/platforms/engine_fit_windows.py)）。
   锁为 dirty 时经 `verify_manual_recovery` 核验恢复 token、说明与新端点；非恢复状态
   携带恢复参数即拒绝。
4. `FitClient` 的 `inspect` 核验 `/v1/models` 的模型 ID 与引擎版本来源，`idle` 核验
   全服务空闲指标；不 idle 即阻断（连接关闭、低 CPU 或 `/health` 成功都不能替代空闲
   证据）。LM Studio 的 inspect/idle 委托 `LMStudioObserver`，执行与观测通过同一已绑定
   服务，不做独立端口自发现。
5. `Guard.check` 在请求前、请求中、请求后采集身份、资源、磁盘、温度与内存读数并按
   计划阈值停止；请求期间的原生检查在独立线程中每 0.5 秒执行，不阻塞 HTTP 截止或
   取消，原生读结束才允许后续检查或封存触碰最近快照。
6. 请求循环按冻结顺序执行（并发固定 1，无隐式重试）：每行先持久化为
   `invalid/request_in_flight` 并标记 dirty，收到完整响应后才改为 `completed`；
   协议完整终止后 `check_service` 复核身份，`_completed_idle` 最多等待 10 秒确认空闲
   指标归零，然后 `lock.clean`；该排空等待不计入请求时限。
7. 结束时按五终态守恒计算 `counts` 并写入 completeness；`seal_run` 封存四份文件与
   manifest.v2（manifest 最后写入，已封存目录不可重复封存）；dirty 未清时在结果
   details 中返回 `recovery_token`。

### `engine-fit compare` 与 `engine-fit verify`（纯离线）

1. `compare` 对两份已封存运行重新核验 manifest 字节与 plan/行语义，要求同计划、同
   测量源码、不同引擎，把已核验来源字节复制到 `sources/<index>` 后新建比较目录；
   原路径关闭或删除后仍可离线核验。
2. `verify` 优先读取最终 manifest；无 manifest 时经 `read_partial` 读取未封存中断的
   最小文件集。通用 `inferyard verify --path` 经 `application/verification.py` 识别
   engine-fit 产物后复用同一验证器，不复制其逻辑。

## 约束与失败行为

- 请求并发固定 1、无隐式重试、temperature=0、顺序与预算冻结；请求 ID 从 `r000001`
  起，发送前把该行持久化为在途状态。五终态（completed/failed/cancelled/invalid/
  not_executed）守恒与字段语义见 [engine-fit 契约](../../cli/docs/contracts/engine-fit-contract.md)，
  此处不复制。
- 凭据只经 `--api-key-env` 环境变量引用；冻结输入（计划、模型名）内出现凭据即拒绝
  （`credential_in_frozen_input`）；CLI 输出只保存 `lms` 白名单字段，argv 与环境实际值
  不落盘。
- 取消映射为退出码 130 与 `cancelled` 终态；传输/预检失败映射为退出码 3 并保留触发
  读数（`stop:<request_id>` 阶段快照）；证据或 IO 错误映射为退出码 4 并保留 dirty。
- 超时或取消后停止发送并按冻结期限排空；无法确认空闲时保留 dirty，恢复须显式 token
  与说明，旧失败目录不覆盖、不删除锁或 state；资源停止后先处理原因，不反复跑满。
- 所有 run/compare 产物 `diagnostic=true`、`performance_comparison_qualified=false`，
  limitations 明确列出客户端耗时、边界采样而非峰值、进程树 RSS 共享页重复计数、
  GPU 内存/能耗未采集等边界；模拟与软件验证不授予真机或正式性能资格（软件缺口不
  归因于硬件，见 [ADR 014](../../cli/docs/decisions/014-macos-common-engines.md)）。
- 旧 definition 的读取语义保持，离线校验按版本分派，不猜测或改写旧数据；不支持的
  旧格式按 [ADR 038](../../cli/docs/decisions/038-inferyard-current-format.md) 拒绝，
  不转换、不覆盖已有证据。


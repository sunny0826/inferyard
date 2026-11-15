# 扩展协议与受控工作流

`cli/src/inferyard/extensions/` 实现与正式 v3 证据通路隔离的独立实验协议：固定上游构建的并发与工具传输、闭集并发负载、四臂 ABBA 观测开销对照，以及把对照绑定到正式 trial 的完整控制工作流。它与正式测评的边界是：扩展协议自成 `extension_packet.v2` 封存容器、自己的 journal 与离线核验入口，产物不进入正式评分或性能比较资格；正式执行与证据语义见 [运行时执行与引擎适配](runtime-execution.md) 与 [证据、封存与离线读取](evidence-storage.md)，容器布局归属见 [run-evidence-layout 契约](../../cli/docs/contracts/run-evidence-layout.md)。

「职责与事实源」「运行路径」「协议定义一览」「事件、容器与封存布局」「状态转换」「约束与失败行为」各节正文已移入 `extensions-protocol/` 目录；「验证」保留在本页。

## 职责与事实源

正文见 [职责与事实源](extensions-protocol/responsibilities.md#职责与事实源)。

## 运行路径

正文见 [运行路径](extensions-protocol/workflow.md#运行路径)。

### v1 观测接线（total_control_runtime.Observer）

正文见 [v1 观测接线](extensions-protocol/workflow.md#v1-观测接线total_control_runtimeobserver)。

## 协议定义一览

正文见 [协议定义一览](extensions-protocol/definitions.md#协议定义一览)。

### 工具链细节（native_tools）

正文见 [工具链细节](extensions-protocol/definitions.md#工具链细节native_tools)。

## 事件、容器与封存布局

正文见 [事件、容器与封存布局](extensions-protocol/evidence.md#事件容器与封存布局)。

## 状态转换

正文见 [状态转换](extensions-protocol/workflow.md#状态转换)。

### 闭集负载的请求行

正文见 [闭集负载的请求行](extensions-protocol/workflow.md#闭集负载的请求行)。

### 四臂与守护进程

正文见 [四臂与守护进程](extensions-protocol/workflow.md#四臂与守护进程)。

## 约束与失败行为

正文见 [约束与失败行为](extensions-protocol/constraints.md#约束与失败行为)。

### 独立守护进程（independent guard）

正文见 [独立守护进程](extensions-protocol/constraints.md#独立守护进程independent-guard)。

### 四臂对照的硬错误与缺陷

正文见 [四臂对照的硬错误与缺陷](extensions-protocol/constraints.md#四臂对照的硬错误与缺陷)。

### v2 行级核验（verify_control_rows）

正文见 [v2 行级核验](extensions-protocol/constraints.md#v2-行级核验verify_control_rows)。

### 其余约束

正文见 [其余约束](extensions-protocol/constraints.md#其余约束)。

## 验证

以下命令在仓库根执行；live 通路需要操作者已启动并绑定真实服务，模拟验证使用 fixture：

```sh
# 扩展协议全部单元测试（含 fixture 级 freeze/replay/live 编排演练）
cd cli && mise exec -- uv run --frozen pytest -q \
  tests/unit/test_closed_concurrency.py \
  tests/unit/test_extension_evidence.py \
  tests/unit/test_extension_runtime.py \
  tests/unit/test_extension_transport.py \
  tests/unit/test_native_tools.py \
  tests/unit/test_total_observer_control.py

# lint
cd cli && mise exec -- uv run --frozen ruff check src tests scripts

# 扩展 wire schema 的生成物核对（closed_concurrency / native_tools / total_observer_control）
cd cli && mise exec -- uv run --frozen python scripts/export_schemas.py --check

# 全量测试（影响面较大时；仓库根任务 mise run test 的等价命令）
cd cli && mise exec -- uv run --frozen pytest
```

测试与行为的对应：`test_closed_concurrency.py` 演练调度绑定槽位、goodput/窗口归约、坏证据与缺排空拒绝资格；`test_extension_evidence.py` 演练封存重放、篡改拒绝、离线 freeze 绑定与 fixture 不继承 live 资格；`test_extension_runtime.py` 演练取消汇合、独立守护进程边界与四臂 seal；`test_extension_transport.py` 演练预检 fail-closed、有效参数与原生工具流；`test_native_tools.py` 演练四项检查、调用预算与 SSE 组装拒绝；`test_total_observer_control.py` 演练软件验证与真机资格区分、guard 身份环境绑定、臂重叠与部分臂保留。

端到端手工路径（需真实 llama.cpp 固定构建服务，不属于默认验证）：`inferyard extension freeze --spec SPEC.json --config CONFIG.toml --out EXT_PLAN`、`inferyard extension run --plan EXT_PLAN/plan.json --config CONFIG.toml --out EXT_RUNS`，离线核验用 `inferyard verify --path EXT_RUNS/<evidence_dir>`（重放归约并比对 `summary.json`），语法见 [使用指南](../../cli/docs/usage.md)。

核对方式：pytest 测试文件均存在于 `cli/tests/unit/`；ruff 与 pytest 由 `cli/pyproject.toml` 的依赖组提供，`--frozen` 与 `uv` 版本由 [mise.toml](../../mise.toml) 固定；`export_schemas.py --check` 覆盖 `schemas.py` 惰性引入的 `schemas_extensions.py` 三个 wire schema；仓库根的 `mise run test`/`lint`/`schemas` 任务分别是上述三条命令的封装。macOS 上 `test_extension_runtime.py` 有 2 条 Linux fork 守护进程用例按平台跳过，属预期；本次核对该测试子集为 53 passed、2 skipped，ruff 与 schema 核对均通过。live 通路（步骤 3-9 的真实服务执行）在本机未经真机演练，真机资格以独立证据标记为准。

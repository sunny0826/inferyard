# CLI 与应用分发

CLI 与应用分发层负责把命令行参数转成应用请求、把应用结果呈现到 stdout，并在入口统一映射错误与退出码；它不实现测量、评分或证据逻辑，只构造请求、选择处理器、呈现结果。上界是参数与呈现（`cli/`），下界是共享类型与显式分派（`application/`）。执行与取消见[运行时执行与引擎适配](runtime-execution.md)，配置解析与冻结计划见[配置、契约与冻结计划](config-plans.md)，产物核验的证据规则见[证据、封存与离线读取](evidence-storage.md)，报告与比较的离线生成见[评分、比较与报告](scoring-reporting.md)，device-check 的平台实现见[平台采集与设备身份](platform-sampling.md)，engine-fit 与扩展分派的业务规则见 [engine-fit 引擎诊断](engine-fit.md) 与 [扩展协议与受控工作流](extensions-protocol.md)。

「职责与事实源」「运行路径」「约束与失败行为」三节正文已移入 `cli-dispatch/` 目录；其余各节保留在本页。

## 职责与事实源

正文见 [职责与事实源](cli-dispatch/responsibilities.md#职责与事实源)。

## 运行路径

正文见 [运行路径](cli-dispatch/request-lifecycle.md#运行路径)。

## 约束与失败行为

正文见 [约束与失败行为](cli-dispatch/request-lifecycle.md#约束与失败行为)。

### 命令级验证与审核缓存

正文见 [命令级验证与审核缓存](cli-dispatch/request-lifecycle.md#命令级验证与审核缓存)。

### 元数据查询

正文见 [元数据查询](cli-dispatch/request-lifecycle.md#元数据查询)。

### 分派内联命令

正文见 [分派内联命令](cli-dispatch/request-lifecycle.md#分派内联命令)。

### 单次与批量分派条件

正文见 [单次与批量分派条件](cli-dispatch/request-lifecycle.md#单次与批量分派条件)。

### 输出形态

正文见 [输出形态](cli-dispatch/request-lifecycle.md#输出形态)。

## 长上下文速度接入

专项 `plan --preset context-speed` 与 `speed-results` 延迟分派到
[`application/context_speed.py`](../../cli/src/inferyard/application/context_speed.py)。
请求新增 benchmark_profile、context_size、preset、batch；应用层复用完整 raw verifier 并返回专项
CommandResult。progress 的专项建议由 config 层分派，仍保持裸 progress 输出。
语法与退出码见 [CLI 专项接口](../../cli/docs/cli-surface.md#长上下文速度专项)。

## 验证

以下命令在仓库根的 `cli/` 目录内执行（对应 mise task `test`/`lint`/`schemas`）；pytest 的 `testpaths` 在 `pyproject.toml` 中设为 `tests`，下面给出显式文件路径以便只跑相关回归。

```sh
# 全量单元与集成回归（等价 mise task：mise run test）
uv run --frozen pytest

# 本分派面相关回归：冷启动不加载执行后端、规范命令映射、参数拒绝、progress 输出
uv run --frozen pytest \
  tests/unit/test_cli_architecture.py \
  tests/unit/test_cli_surface.py \
  tests/unit/test_cli_dispatch_surface.py \
  tests/unit/test_workspace_progress.py

# 产物识别与核验路由回归
uv run --frozen pytest \
  tests/unit/test_verification.py \
  tests/unit/test_offline_reading.py

# Ruff（Python 3.15，100 列；等价 mise task：mise run lint）
uv run --frozen ruff check src tests scripts

# JSON Schema 生成物与契约源无漂移（等价 mise task：mise run schemas）
uv run --frozen python scripts/export_schemas.py --check

# 元数据冷启动冒烟（不加载实时后端）
uv run --frozen inferyard --versions
```

`test_cli_architecture.py` 用 import hook 断言 `--help`/`--versions`/`--schema` 不导入执行后端；`test_cli_surface.py` 断言已删除命令名与参数别名的显式拒绝；`test_cli_dispatch_surface.py` 断言规范命令分组不改变后端结果与退出码传递；`test_verification.py` 与 `test_offline_reading.py` 覆盖产物识别、专用验证器权威保留与核验选项约束。

本文是纯文档变更：写作时未执行上述命令，只核对了命令、脚本与测试文件的存在性及参数来源（mise task、`pyproject.toml` 的 pytest/ruff 配置、`scripts/export_schemas.py --check` 的 argparse 定义）。合入前应按项目流程实际运行 `test` 与 `lint`。

## 与既有文档的差异

- [CLI 接口](../../cli/docs/cli-surface.md) 的规范语法表把 `check --config CFG` 列为已删除语法，而分派别名表把规范命令 `probe` 映射到内部模块命令 `check`（[`cli/src/inferyard/application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py)）；二者不矛盾——`check` 只是 `runtime.runner` 的内部命令名，无法从命令行直接产生，但相邻表述易被误读为 `check` 仍可从 CLI 调用，阅读时需区分“CLI 语法”与“内部命令名”。
- [CLI 接口](../../cli/docs/cli-surface.md) 声明八个 `*-check` 命令与 `public-config-check` 已删除、调用返回输入阻断；[`cli/src/inferyard/application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py) 仍保留 `rescore-check`/`export-check`/`report-check`/`compare-check`/`repeat-check`/`overhead-check`/`extension-check`/`public-check`/`public-config-check` 的分派分支，且 `public-config-check` 还被核验路由作为内部命令名调用（[`cli/src/inferyard/application/verification.py`](../../cli/src/inferyard/application/verification.py) 的 `_HANDLERS` 同样以 `*-check` 作为传给专用验证器的内部命令名）。这些名字无法由解析器产生（`add_commands` 只注册规范名），故无 CLI 可观察行为，属分派层的内部命名残留；本文按“不可达、无外部语义”记录，不视为对外兼容入口。

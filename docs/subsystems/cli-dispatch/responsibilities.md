# 职责与事实源

本页是[CLI 与应用分发](../cli-dispatch.md) 的子文档，收录「职责与事实源」的正文；内容不变，入口保留原节标题与锚点。

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 控制台/模块入口 | `inferyard` 与 `python -m inferyard` 进入 `main`，可注入 `handlers` 覆盖分派（测试用） | `main` 在 [`cli/src/inferyard/cli/__init__.py`](../../../cli/src/inferyard/cli/__init__.py)，模块入口在 [`cli/src/inferyard/__main__.py`](../../../cli/src/inferyard/__main__.py) |
| 参数解析与命令注册 | 净化解析器（不回显原始参数）、子命令注册；各命令只注册参数，不导入执行器 | `Parser`、`ArgumentError`、`parser` 在 [`cli/src/inferyard/cli/arguments.py`](../../../cli/src/inferyard/cli/arguments.py)，注册函数 `add_commands` 分布在 `cli/src/inferyard/cli/commands/` 各模块（如 [`execution.py`](../../../cli/src/inferyard/cli/commands/execution.py)、[`discovery.py`](../../../cli/src/inferyard/cli/commands/discovery.py)） |
| 元数据查询 | `--versions` 导出工具/依赖版本，`--schema KIND` 导出当前 JSON Schema | `versions` 在 [`cli/src/inferyard/cli/metadata.py`](../../../cli/src/inferyard/cli/metadata.py)；`export_schema`、`schemas_for` 在 [`cli/src/inferyard/contracts/schemas.py`](../../../cli/src/inferyard/contracts/schemas.py) |
| 请求构造 | 跨参数校验、路径解析、配置加载，产出 `CommandRequest` | `build_request` 在 [`cli/src/inferyard/cli/request.py`](../../../cli/src/inferyard/cli/request.py)，配置加载入口 `load_config`、`validate_endpoint` 在 [`cli/src/inferyard/config/loader.py`](../../../cli/src/inferyard/config/loader.py) |
| 共享类型 | `CommandRequest`/`CommandResult`/`Handler`/`VerificationOptions` 唯一定义处 | [`cli/src/inferyard/application/types.py`](../../../cli/src/inferyard/application/types.py) |
| 命令分派 | 显式命令到执行模块的映射；执行模块只在分派时导入 | `default_backend` 在 [`cli/src/inferyard/application/dispatch.py`](../../../cli/src/inferyard/application/dispatch.py) |
| 产物识别与核验路由 | 按标记文件识别派生产物，保留各专用验证器权威 | `execute`、`_identify`、`_HANDLERS` 在 [`cli/src/inferyard/application/verification.py`](../../../cli/src/inferyard/application/verification.py)；原始 run/batch/plan 核验在 [`cli/src/inferyard/application/raw_verification.py`](../../../cli/src/inferyard/application/raw_verification.py) |
| 应用侧聚合执行器 | 社区准备、engine-fit 五个动作、memory-check、model/engine acquire、overhead、progress、service discover 的应用层入口 | [`cli/src/inferyard/application/community.py`](../../../cli/src/inferyard/application/community.py)、[`engine_fit.py`](../../../cli/src/inferyard/application/engine_fit.py)、[`memory_check.py`](../../../cli/src/inferyard/application/memory_check.py)、[`model_source.py`](../../../cli/src/inferyard/application/model_source.py)、[`engine_source.py`](../../../cli/src/inferyard/application/engine_source.py)、[`overhead.py`](../../../cli/src/inferyard/application/overhead.py)、[`progress.py`](../../../cli/src/inferyard/application/progress.py)、[`service_discovery.py`](../../../cli/src/inferyard/application/service_discovery.py) |
| 输出呈现 | CommandResult 到 stdout 文本的转换；device-check 人类摘要 | `format_result`、`presentation_context`、`_human` 在 [`cli/src/inferyard/cli/device_output.py`](../../../cli/src/inferyard/cli/device_output.py) |
| 错误映射与退出码 | 统一异常到退出码/limitation 的映射，结果完整性校验 | `run` 在 [`cli/src/inferyard/cli/entrypoint.py`](../../../cli/src/inferyard/cli/entrypoint.py) |
| 失败原因措辞 | 异常到固定 limitation 字符串的脱敏转换（白名单回退） | `safe_reason` 在 [`cli/src/inferyard/evidence/error_reasons.py`](../../../cli/src/inferyard/evidence/error_reasons.py)；旧格式拒绝的 `UnsupportedFormat` 在 [`cli/src/inferyard/evidence/formats.py`](../../../cli/src/inferyard/evidence/formats.py) |
| 命令级验证/审核缓存 | 单次命令内的内容哈希验证缓存与人工审核缓存，不持久化 | `command_validation` 在 [`cli/src/inferyard/contracts/validation_cache.py`](../../../cli/src/inferyard/contracts/validation_cache.py)；`command_reviews` 在 [`cli/src/inferyard/config/review_cache.py`](../../../cli/src/inferyard/config/review_cache.py) |

关键状态的事实源：

- `CommandRequest` 与 `CommandResult` 是 `frozen=True, slots=True` 的 dataclass；请求字段全集以 [`cli/src/inferyard/application/types.py`](../../../cli/src/inferyard/application/types.py) 的 `CommandRequest` 为准，新增命令参数必须在此登记，`build_request` 按 `CommandRequest.__dataclass_fields__` 过滤解析结果。`Handler` 是 `Callable[[CommandRequest], tuple[int, CommandResult]]` 的类型别名：处理器吃掉请求、返回退出码与结果，不接触 argv 与 stdout。
- 命令行运行期没有全局可变状态：验证缓存与审核缓存都是 `ContextVar`，只在 `main` 的一次调用内有效，命令结束即丢弃。
- 结果外层的字段语义（`status`/`completeness`/`limitations`/`details` 等）见 [CLI 接口](../../../cli/docs/cli-surface.md) 的 JSON 与共享类型一节，此处不复制。
- 退出码语义（0/2/3/4/130）见 [CLI 接口](../../../cli/docs/cli-surface.md)；stdout JSON 与诊断分流约定见[架构](../../../cli/docs/architecture.md)的层表。
- 活跃契约版本常量为 `SCHEMA_VERSION = 3`（[`cli/src/inferyard/__init__.py`](../../../cli/src/inferyard/__init__.py)），`--versions` 与 schema 导出的版本字段都以此为准。
- `VerificationOptions`（[`cli/src/inferyard/application/types.py`](../../../cli/src/inferyard/application/types.py)）在类型层解析核验专用选项：`--source-root OLD=NEW` 校验非空、解析为绝对路径并拒绝互相包含的重叠映射；它与 `rerender` 一起构成传给报告/比较验证器的选项对象，解析失败抛 `ContractError`。

命令注册与分派目标一一对应（注册模块不导入执行器，目标模块只在分派时导入）：

| 命令/命令组 | 注册模块（`cli/commands/`） | 分派目标 |
| --- | --- | --- |
| `init`、`runtime prepare`、`config assets/create/bind` | `community.py` | `application/community.py` |
| `progress` | `discovery.py` | `application/progress.py` |
| `memory-check` | `discovery.py` | `application/memory_check.py` |
| `device-check` | `discovery.py` | `platforms/device_preflight.py` |
| `catalogue`、`plan` | `discovery.py` | 分派内联：`registry.catalogue`、`config.planning` |
| `verify` | `verification.py` | `application/verification.py` |
| `overhead` | `overhead.py` | `application/overhead.py` |
| `probe`、`run`（单次） | `execution.py` | `runtime/runner.py` |
| `run --plan`、`resume` | `execution.py` | `runtime/batch_runner.py` |
| `prepare-length` | `length.py` | `runtime/length_prepare.py`（Windows 拒绝） |
| `repeat-summary` | `repetition.py` | `reporting/repetition_report.py` |
| `rescore` | `scoring.py` | `reporting/rescore.py` |
| `export` | `exports.py` | `reporting/export.py` |
| `public package/plan` | `public.py` | `reporting/public_package.py`、`config/public_plan.py` |
| `report` | `reporting.py` | `reporting/report.py` |
| `compare` | `comparison.py` | `reporting/comparison_report.py` |
| `filter-candidates` | `candidates.py` | `analysis/candidate_filter.py` |
| `extension freeze/run/replay` | `extensions.py` | `extensions/workflow.py` |
| `engine-fit engines/plan/run/compare/verify` | `engine_fit.py` | `application/engine_fit.py` |
| `model acquire` | `model.py` | `application/model_source.py` |
| `engine acquire` | `engine.py` | `application/engine_source.py` |
| `engine check` | `engine.py` | `application/engine_check.py` |
| `service discover` | `service.py` | `application/service_discovery.py`（枚举与探测在 `platforms/service_discovery.py`） |

注册模块只描述参数形状（类型、必填、互斥组），分派目标模块统一暴露 `execute(request) -> (code, CommandResult)` 约定；`application/` 下的聚合执行器是该约定的薄适配层，业务规则仍在领域包内，`application/types.py` 是两侧共享的唯一类型来源。

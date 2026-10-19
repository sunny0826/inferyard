# 运行路径与约束

本页是[CLI 与应用分发](../cli-dispatch.md) 的子文档，收录「运行路径」「约束与失败行为」「命令级验证与审核缓存」「元数据查询」「分派内联命令」「单次与批量分派条件」「输出形态」的正文；内容不变，入口保留原节标题与锚点。

## 运行路径

1. `python -m inferyard` 或控制台脚本进入 `main`（[`cli/src/inferyard/cli/__init__.py`](../../../cli/src/inferyard/cli/__init__.py)）：打开 `command_reviews` 审核缓存上下文，调用 `run`（[`cli/src/inferyard/cli/entrypoint.py`](../../../cli/src/inferyard/cli/entrypoint.py)），注入 `parser`、`build_request` 包装与 `default_backend`。
2. `presentation_context`（[`cli/src/inferyard/cli/device_output.py`](../../../cli/src/inferyard/cli/device_output.py)）在解析前预扫描 argv：仅当首参数为 `device-check` 时识别显式 `--json`/`--format`（支持 `--opt=value` 与缩写前缀，遇 `--` 终止），使输出格式选择不受后续解析失败影响。
3. `parser` 解析 argv。`--version`/`--versions`/`--schema` 是互斥元数据组；若与命令同现或无命令，抛 `ArgumentError`；元数据分支直接打印 JSON 并返回 0，不进入请求构造与分派。元数据分支在入口内完成：`--versions` 调 `versions()`，`--schema KIND` 调 `export_schema(KIND)`，KIND 取自 `schemas_for()` 的当前注册表。
4. 对 `run`/`resume`/`plan`/`probe` 打开 `command_validation` 内容哈希验证缓存（其他命令不启用），`build_request`（[`cli/src/inferyard/cli/request.py`](../../../cli/src/inferyard/cli/request.py)）执行跨参数校验、把路径参数解析为绝对路径，并加载 `--config` 得到 `LoadedConfig`；`config create` 带 `--workspace` 时，裸名题包（无后缀、无父目录）解析为 `WORKSPACE/bundles/NAME.json`。
5. 处理器选择：显式 `handlers` 覆盖优先（测试注入伪后端），否则 `default_backend`（[`cli/src/inferyard/application/dispatch.py`](../../../cli/src/inferyard/application/dispatch.py)）。`default_backend` 是显式 if 链，每个分支在分派时才 `import` 对应执行模块，未知命令抛 `ContractError("command", "unsupported command")`。
6. 分派要点：`probe` 规范名映射到内部 `check` 后进入 `runtime.runner`，结果外层 `command` 还原为规范名；`catalogue` 与 `plan` 在分派内直接调用 `registry.catalogue` 与 `config.planning`，不经过独立执行模块；`prepare-length` 在 Windows 于分派层拒绝（`PreflightError("windows_phase2_live_not_supported")`）；`init`/`runtime prepare`/`config assets/create/bind` 聚合到 `application/community.py`，`engine-fit` 五个动作聚合到 `application/engine_fit.py`；`verify` 进入产物识别与核验路由（见下）。
7. `verify` 的执行器（[`cli/src/inferyard/application/verification.py`](../../../cli/src/inferyard/application/verification.py)）按标记文件识别产物类型，run/batch/plan/engine-fit 走原始核验（[`cli/src/inferyard/application/raw_verification.py`](../../../cli/src/inferyard/application/raw_verification.py)），八类派生产物按 `_HANDLERS` 延迟调用各专用验证器并保留其状态、完整性、限制与退出码；comparison 与 report 共存时核验两者并合并 details。识别冲突、损坏标记或不支持的产物类型抛 `EvidenceError`，经入口映射为退出 4；核验选项与产物类型不匹配（如 `--config` 用于非 public）抛 `ContractError`，映射为退出 2。
8. 处理器返回 `(code, result)` 后，`run` 校验退出码属于 {0, 2, 3, 4, 130} 且 `result.command == request.command`，否则视为后端结果非法；异常按类型映射（见下节）。
9. `format_result` 转换输出：`progress` 成功时直接输出 `details` 的 `progress.v1`（不套 CommandResult 外层）；`device-check` 默认人类摘要；其余为 `asdict(CommandResult)` 的单行 JSON，`allow_nan=False`。结果打印到 stdout，诊断只走 stderr，`run` 返回退出码。

## 约束与失败行为

- 类型边界：`CommandRequest`/`CommandResult`/`Handler` 只定义在 [`cli/src/inferyard/application/types.py`](../../../cli/src/inferyard/application/types.py)；业务模块不依赖 CLI 包，CLI facade 重导出同一对象，不另建 CLI DTO。
- 参数边界：`build_request` 拒绝不满足组合规则的输入——endpoint/PID 覆盖仅允许 rerun、实验执行、engine-fit run 与社区准备命令；`run --rerun-from` 与实验绑定要求 endpoint 与 PID 成对且 PID 为正；recovery 确认与备注必须同时给出；`config create` 无 `--workspace` 时 `--preflight`/`--results` 必填；端口限 1..65535。违规抛 `ArgumentError`/`ContractError`，统一映射为退出 2 / `invalid_input`。
- 路径解析：`build_request` 默认把路径参数 `resolve()` 为绝对路径；社区准备、`model/engine acquire` 与 `engine-fit` 命令改用 `absolute()` 保留链接，不由 CLI 提前解析；`memory-check` 的 `--protect-pid` 必须为正整数。
- 解析器硬化：`Parser` 设 `allow_abbrev=False` 关闭 argparse 前缀缩写，`error` 重写为净化诊断；子命令帮助文本中的历史格式提示（“Historical formats must be handled in the original project”）来自 `parser` 的 epilog。
- 延迟加载：`default_backend` 与各 `add_commands` 注册模块都不在 CLI 启动时导入执行后端；`--help`/`--version`/`--versions`/`--schema` 只注册参数与读取元数据。Schema 注册表只依赖契约包与版本常量（当前 `SCHEMA_VERSION = 3`），语义验证在解析/执行时调用。
- 未知命令：解析器只注册规范命令名，已删除的旧命令名与参数别名在解析层拒绝；分派层对无法识别的 `request.command` 抛 `ContractError`，经入口映射为退出 2，而非静默成功。
- 输出约束：stdout 恰好一个 JSON 对象（或 device-check 人类摘要），`NaN`/`Infinity` 拒绝；格式参数不进入 `CommandRequest`。后端执行器不向 stdout 写诊断。
- 凭据安全：净化解析器 `Parser.error` 不回显 argparse 原始诊断（可能含凭据），统一抛 `invalid CLI arguments; use --help`；入口对未预期异常不打印 traceback/repr；stderr 只输出固定脱敏短语。
- device-check 呈现：人类摘要由 `_human` 生成，status 映射为中文（如 `inspected`/`recommended` → “完成”），容量换算为 GiB，缺测与阻断原因查 `REASONS` 中文表，控制字符经 unicode escape 转义后输出，末尾固定提示“本次未发送模型请求；检测结果不表示服务已就绪”。设备检测本身的规则见[平台采集与设备身份](../platform-sampling.md) 与[设备检测](../../../cli/docs/device-check.md)。显式 `--json`/`--format json` 时保留 CommandResult JSON 外层，便于 Agent 解析；入口用 `getattr(args, "device_format", "json")` 取格式，非 device-check 命令一律回退到 JSON。
- progress 特化：`progress` 永不报告执行结局，任何非 0/130 的结果在入口重映射为 2（[`cli/src/inferyard/application/progress.py`](../../../cli/src/inferyard/application/progress.py) 同时把准备类异常映射为 2），成功时输出裸 `progress.v1`，信封字段与 next_action 排序见 [CLI 接口](../../../cli/docs/cli-surface.md) 的首次上手进度契约。
- 错误映射（[`cli/src/inferyard/cli/entrypoint.py`](../../../cli/src/inferyard/cli/entrypoint.py) 的 `run`）：`UnsupportedFormat` → 2 / `unsupported_format`（details 附 artifact、saved_version、supported_versions，stderr 固定提示指向原项目）；`ContractError` 与 `ArgumentError` → 2 / `invalid_input`；`PreflightError` → 2 / `preflight_blocked`（`windows_phase2_live_not_supported` 单独保留原因为 limitation）；`EvidenceError` → 4；`OSError` → 4 / `io_error`；`KeyboardInterrupt` → 130 / `cancelled`；其余异常 → 4 / `internal_error`。limitation 措辞的来源规则见[数据契约](../../../cli/docs/data-contract.md)。
- 呈现失败兜底：`format_result` 自身抛错时输出 `internal_error` 结果，退出码对 progress 为 2、其余为 4。
- 结果完整性闸门：后端返回的退出码不在 {0, 2, 3, 4, 130} 或 `result.command != request.command` 时，`run` 抛 `RuntimeError`，落入 `internal_error`（退出 4）；执行器不能把未映射的退出码或冒名的结果外层传出入口。
- 处理器注入边界：`handlers` 参数只服务于测试替换伪后端；生产入口（控制台脚本与 `__main__.py`）恒以 `default_backend` 兜底，注入处理器不改变错误映射与输出路径。
- 规范命令适配：分派内的别名表把规范名映射到内部模块命令（如 `probe` → `check`、`public package` → `public-package`），再把结果外层的 `command` 还原为规范名；这只影响分派与呈现，不改变业务执行。已删除语法的完整对照见 [CLI 接口](../../../cli/docs/cli-surface.md) 的规范语法与兼容入口表。
- 兼容规则：分派与入口不改变证据格式语义；旧格式由 `UnsupportedFormat` 在入口统一拒绝并指向原项目，支持矩阵见[当前格式契约](../../../cli/docs/contracts/inferyard-current-format.md)。每条事实的权威位置遵循[数据契约](../../../cli/docs/data-contract.md) 与 [主题契约](../../../cli/docs/contracts/README.md)，本层只链接不复制。

### 命令级验证与审核缓存

- `command_validation(enabled=...)`（[`cli/src/inferyard/contracts/validation_cache.py`](../../../cli/src/inferyard/contracts/validation_cache.py)）只在入口为 `run`/`resume`/`plan`/`probe` 打开，其余命令显式禁用；缓存以文档内容哈希（`validation_key` 对 `config`/`config_input`/`bundle`/`plan`/`experiment` 五类文档计算 SHA-256）为键，同一次命令内重复校验同一文档只成功一次。
- `command_reviews`（[`cli/src/inferyard/config/review_cache.py`](../../../cli/src/inferyard/config/review_cache.py)）贯穿整个 `main` 调用，供题包加载登记人工审核结果；两个缓存都经 `ContextVar` 实现、随命令结束丢弃，不落盘、不跨命令复用。

### 元数据查询

`--version` 是 argparse `version` action，在解析阶段直接打印 `inferyard <版本> source=<checkout|release>` 并退出。`source` 只区分安装方式：PEP 660 editable 为 `checkout`，其余为 `release`，不读取 git。`--versions` 返回 `versions()` 的 JSON，含 `tool_version`、`source`、`schema_version`、`supported_schema_versions`、Python 版本与实现名，以及一组固定依赖的版本（未安装的依赖填 `null`）；`--schema KIND` 返回 `export_schema(KIND)` 的 Draft 2020-12 JSON Schema，KIND 可选值来自 `schemas_for()`，choices 在构造解析器时从注册表排序生成。两者都只读取包内元数据与契约注册表，不触碰执行后端。`--help` 同理：由 argparse 在解析阶段处理并退出，不构造请求。

### 分派内联命令

`catalogue` 与 `plan` 没有独立执行模块，结果在 `default_backend` 内直接构造：`catalogue` 调 `registry.catalogue(kind)` 并把目录放进 `details`；`plan --dry-run` 调 `config.planning.prepare_plan` 预览（`evidence_dir=None`），`plan --out` 调 `write_plan` 冻结并返回计划目录。二者仍走统一的结果校验、呈现与错误映射，只是不经过领域执行器。

### 单次与批量分派条件

`run` 的分派条件不是命令名而是请求形状：`request.frozen_plan is not None` 时进 `runtime.batch_runner` 批量路径，否则进 `runtime.runner` 单次路径；`resume` 无条件进批量路径。`build_request` 相应要求 `--plan` 必须搭配 `--output-root`，保证批量路径拿到批次根目录。

### 输出形态

`format_result` 只有三种形态：普通命令输出 `asdict(CommandResult)` 的单行 JSON；`device-check` 默认输出 `_human` 摘要，显式 JSON 回到 CommandResult 外层；`progress` 成功时输出裸 `progress.v1` 对象，失败沿用 blocked envelope。形态选择只看 `result.command` 与 device-check 的格式参数，后端执行器无法自定义呈现。

本参考遵循[子系统设计参考索引](../README.md)的维护约定：已记录的职责、运行路径、状态转换或失败行为变化时，在同一 PR 更新本文。

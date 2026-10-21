# 配置、契约与冻结计划

本子系统覆盖 `contracts/` 与 `config/` 两个包：前者持有唯一活动数据契约（`schema_version = 3`）的结构与语义验证入口，后者负责配置解析、题包加载与人工审核、冻结计划编译与声明绑定。它处于 CLI 请求构造（见 [CLI 与应用分发](cli-dispatch.md)）与运行时执行（见 [运行时执行与引擎适配](runtime-execution.md)）之间：输出的是已验证、已冻结、哈希绑定的内存内 Document 与 plan，不发送任何模型请求、不执行引擎、不触碰服务生命周期。凭据只以环境变量名引用，落盘脱敏由证据层负责（见 [证据、封存与离线读取](evidence-storage.md)）。schema/catalogue 生成物与导出脚本的归属见 [Catalogue 与 Schema 生成](catalogue-generation.md)。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 契约版本 | 唯一活动 wire 版本常量；kind 注册表与 JSON Schema 导出 | `SCHEMA_VERSION` 在 [`cli/src/inferyard/__init__.py`](../../cli/src/inferyard/__init__.py)；`schemas_for`、`export_schema` 在 [`cli/src/inferyard/contracts/schemas.py`](../../cli/src/inferyard/contracts/schemas.py) |
| 结构验证 | 严格 JSON 解析（拒绝重复键、非有限数）、结构校验、不可变快照 | `validate_document`、`strict_json_loads`、`Document`、`ContractError` 在 [`cli/src/inferyard/contracts/validation.py`](../../cli/src/inferyard/contracts/validation.py) |
| 语义验证 | 按 kind 分派的跨字段/跨记录校验（实验与计划身份、事件时序、样本缺测、汇总守恒） | `validate_semantics` 在 [`cli/src/inferyard/contracts/contracts_experiment.py`](../../cli/src/inferyard/contracts/contracts_experiment.py)；`validate_storage` 在 [`cli/src/inferyard/contracts/contracts_storage.py`](../../cli/src/inferyard/contracts/contracts_storage.py)；`validate_config`、`validate_event` 在 [`cli/src/inferyard/contracts/lab.py`](../../cli/src/inferyard/contracts/lab.py) |
| 命令内复用 | 单命令内按内容哈希复用已成功校验与审核结论，命令结束即丢弃 | `command_validation`、`validation_key` 在 [`cli/src/inferyard/contracts/validation_cache.py`](../../cli/src/inferyard/contracts/validation_cache.py)；`review_cache` 在 [`cli/src/inferyard/config/review_cache.py`](../../cli/src/inferyard/config/review_cache.py) |
| 配置加载 | TOML/JSON 读取、默认值补齐、凭据与容量规则、路径解析、题包加载 | `load_config`、`validate_runtime_config`、`validate_endpoint`、`read_document` 在 [`cli/src/inferyard/config/loader.py`](../../cli/src/inferyard/config/loader.py) |
| 题包与审核 | 统一题包结构/语义校验、内容哈希、人工审核门槛、迁移与逐题证明核验 | `content_hash`、`validate_bundle`、`require_review`、`validate_case` 在 [`cli/src/inferyard/config/bundle.py`](../../cli/src/inferyard/config/bundle.py)；`approved`、`validate_provenance` 在 [`cli/src/inferyard/config/bundle_review.py`](../../cli/src/inferyard/config/bundle_review.py) |
| 冻结计划 | 实验定义展开为 trial、预算核算、计划哈希、运行配置快照、原子发布 | `compile_plan`、`prepare_plan`、`write_plan` 在 [`cli/src/inferyard/config/planning.py`](../../cli/src/inferyard/config/planning.py)；`compile_single_plan` 在 [`cli/src/inferyard/config/single_plan.py`](../../cli/src/inferyard/config/single_plan.py)；`plan_hash`、`workload_budget` 在 [`cli/src/inferyard/config/plan_math.py`](../../cli/src/inferyard/config/plan_math.py)；`normalized_config`、`runtime_snapshot`、`source_bytes`、`validate_workload_inputs`、`read_frozen_plan` 在 [`cli/src/inferyard/config/plan_inputs.py`](../../cli/src/inferyard/config/plan_inputs.py) |
| 环境准入 | 冻结环境条件的声明式准入，诊断模式只记录差异 | `admission`、`mismatches` 在 [`cli/src/inferyard/config/environment_binding.py`](../../cli/src/inferyard/config/environment_binding.py) |
| 服务绑定 | 候选配置与已运行服务进程的启动参数/启动时刻绑定 | `bind`、`prepare` 在 [`cli/src/inferyard/config/service_binding.py`](../../cli/src/inferyard/config/service_binding.py) |
| 声明谱系 | 冻结的模型来源/量化/转换声明只哈希不鉴权 | `validate_lineage` 在 [`cli/src/inferyard/config/model_lineage.py`](../../cli/src/inferyard/config/model_lineage.py)；`parse_record` 在 [`cli/src/inferyard/config/lineage_records.py`](../../cli/src/inferyard/config/lineage_records.py) |
| 社区资源 | 随包交付的已审题包与示例配置的哈希核验读取 | `resource_bytes`、`BUNDLES` 在 [`cli/src/inferyard/config/community_resources.py`](../../cli/src/inferyard/config/community_resources.py)；`initialize` 在 [`cli/src/inferyard/config/community_workspace.py`](../../cli/src/inferyard/config/community_workspace.py) |

关键事实源只有一处：`SCHEMA_VERSION = 3` 是唯一活动契约版本，全部 kind 的结构定义由 `schemas_for()` 注册表给出，`export_schema()` 不接受版本选择；执行终态枚举 `ExecutionState`（`completed`/`failed`/`cancelled`/`invalid`/`not_executed`）定义在 `contracts/validation.py`，是五终态守恒的符号来源。凭据从不以值进入本层：配置中只保存 `endpoint.api_key_env` 环境变量名，字段语义与拒绝规则见 [数据契约](../../cli/docs/data-contract.md)。

`config/` 内其余模块的归属边界：`engine_fit*.py` 是 engine-fit 专用计划与资产声明，属于 [engine-fit 引擎诊断](engine-fit.md)；`fixed_output.py`、`cache_execution.py`、`cache_policy.py` 是固定输出与缓存复用的冻结输入规则，执行侧核验在 `runtime/`，属于 [运行时执行与引擎适配](runtime-execution.md)；`workspace_inputs.py`、`workspace_progress.py`、`community_candidates*.py`、`public_config_check.py` 是操作者准备工作流与离线声明核对，命令面见 [CLI 与应用分发](cli-dispatch.md)；`public_recipe.py`、`public_plan.py` 服务的公开复现产物属于 [评分、比较与报告](scoring-reporting.md)。

## 运行路径

1. **单次运行（`run --config`）**：CLI 构造请求并调用 `load_config`（注入点见 [`cli/src/inferyard/cli/request.py`](../../cli/src/inferyard/cli/request.py) 的 `config_loader`）。`load_config` 以 16 MiB 上限读取 TOML，先按 `config_input` 结构校验，再用 `CONFIG_DEFAULTS` 补默认值，经 `validate_runtime_config` 应用凭据与容量规则，随后把相对路径解析到源文件目录，加载题包并核对 `bundle.version` 与配置声明一致，最后返回冻结的 `config`/`bundle` 两个 `Document`。
2. **单次冻结**：运行时（[`cli/src/inferyard/runtime/runner.py`](../../cli/src/inferyard/runtime/runner.py)）在发送任何请求前调用 `compile_single_plan`，把内存中的 config/bundle 编译为一个 workload、一个 trial 的同构 plan，请求上限与总墙钟预算由题数、超时、预热次数、空闲等待与基线时长推出；执行入口、身份预检与锁在此之后，见 [运行时执行与引擎适配](runtime-execution.md)。
3. **显式计划（`run --plan` / freeze 命令）**：应用分派（[`cli/src/inferyard/application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py)）调用 `prepare_plan` 或 `write_plan`。`compile_plan` 展开 workloads 为 trials（seeded 顺序按 `seed:experiment_id:workload_id:repeat_index` 派生洗牌），逐 trial 用 `workload_budget` 核算请求数与秒数并校验冻结预算上限；`prepare_plan` 再按 workload 用 `source_bytes` 读取 config/bundle 原始字节并核对声明哈希，用 `runtime_snapshot` 生成 `runtime-configs/<sha256>.json` 绑定快照，经 `validate_workload_inputs` 核对 workload 与 config/bundle 一致后重算 `plan_sha256`。
4. **计划发布**：`write_plan` 先把全部工件与 `experiment.json` 原子写入新目录，`plan.json` 最后发布——它是计划完成的标记，目录不完整时计划不可执行。
5. **计划读取（批量/续测）**：`read_frozen_plan` 在不访问原源目录的前提下核验 `plan.json`：结构版本、`plan_sha256` 自哈希、与 `compile_plan` 重展开结果一致、runtime 绑定齐全、快照字节与从冻结源字节重归一化的结果一致（仅接受一个哈希绑定的历史投影例外），最后产出可直接执行的 `LoadedConfig`。
6. **计划再校验**：任何路径进入 `validate_document("plan", ...)` 都重新执行 `_plan` 语义校验：重算 `plan_hash` 比对 `plan_sha256`、trial 题序与冻结 case 集一致、预算与 trial 总和一致、runtime 绑定覆盖每个 workload。
7. **执行时审核**：trial 启动前由运行时调用 `require_review`（[`cli/src/inferyard/runtime/trial_runner.py`](../../cli/src/inferyard/runtime/trial_runner.py)），命中当前内容的人工批准、核验过的迁移/继承证明或逐题审核记录才放行，审核语义见 [数据契约](../../cli/docs/data-contract.md#按用途分派)。
8. **工作区初始化**：`community_workspace.initialize` 用 `resource_bytes` 读取随包题包原字节并立即 `require_review`，连同三平台示例配置一起写入新工作区；题包字节与 `resources.json` 清单哈希不符即拒绝。

## 约束与失败行为

- **唯一契约**：`validate_document` 只接受 `schema_version = 3` 的 dict，版本不符报 `unsupported schema version; explicit migration required`，未知 kind 报 `unknown document kind`；InferYard 不迁移旧格式、不接受 `origin=migrated`，支持矩阵见 [当前格式契约](../../cli/docs/contracts/inferyard-current-format.md) 与 [ADR 038](../../cli/docs/decisions/038-inferyard-current-format.md)。结构合法不替代语义与来源校验，这一分层见 [架构](../../cli/docs/architecture.md)。
- **错误卫生**：`ContractError` 只携带字段路径与固定描述，从不内嵌被拒绝的值（其中可能含秘密）；`strict_json_loads` 拒绝重复键与非有限数，布尔不冒充整数。诊断与拒绝值不落盘，避免凭据经错误消息进入日志。
- **不可变快照与复用**：`Document` 保存完整校验后的不可变 JSON 快照，输入/输出后续变更不影响它；内部编译可经 `_validated_dict` 消费同 kind 快照免于重复校验，可变 dict 始终完整校验。命令内复用只对 `config`/`config_input`/`bundle`/`plan`/`experiment` 五个 kind 生效，理由见 [ADR 034](../../cli/docs/decisions/034-command-local-reuse.md)。
- **凭据边界**：`validate_endpoint` 要求 loopback 源且 URL 内不含用户名/密码、query 与 fragment；`validate_runtime_config` 拒绝 `startup_args` 中的 `--api-key`、`--password`、`--hf-token` 等凭据参数，要求改用 `endpoint.api_key_env` 引用；`api_key_env` 只存变量名（schema 限定为标识符模式），实际取值与脱敏在运行时和证据层完成。
- **容量与路径**：配置与题包读取均限 16 MiB（`MAX_DOCUMENT_BYTES`）；`generation.max_tokens` 不得超过 `conditions.context_size`；相对路径统一解析到配置文件所在目录；`kvmem`/`ninfer` 适配器允许离线冻结 Windows 风格绝对路径。
- **题包规则**：同一题包不得混合质量与性能题（`task_protocol` 一致）；`category = svg` 为免评分生成题，`rules` 只能为 `{}`；classification 标签集按题包冻结；structured 题的参考答案必须自带内联 JSON Schema 校验通过且评分路径不重叠。无当前人工批准且无有效继承证明时 `require_review` 拒绝；旧整包签名永远不会自动变成逐题批准，逐题记录语义见 [数据契约](../../cli/docs/data-contract.md#按用途分派)。
- **计划展开上限**：题包展开总量超过 100 000 case 或 trial 超过 10 000 拒绝；累计请求数不得超过冻结 `budget.max_requests`；`capacity_stop` 只接受固定长度 performance workload。
- **计划工件完整性**：`source_bytes` 要求引用路径不得逃逸输入目录、字节须与声明 `sha256` 一致；同一计划内同名引用内容冲突报 `plan_reference_collision`；`plan.json`、`experiment.json` 与 `runtime-configs/` 是保留名；输出目录不得与输入目录重叠（`plan_output_overlaps_inputs`）。
- **五终态守恒**：固定协议的 summary 在 `contracts_storage._summary` 中强制 `counts.planned == Σ(五种终态计数)`、`executed == planned - not_executed`、`valid_executed == completed + failed`，且 completion_rate 的分子/分母/排除必须与账本一致；评分故障不撤销执行终态、不缩小分母。持续负载协议 `planned = null`，改由 `_duration` 校验窗口、准入与排空守恒，语义见 [实验设计](../../cli/docs/experiments/design.md)。
- **环境准入**：`admission` 返回 `environment-admission.v2` 记录（`policy_source`、`differences`、`blockers`）；显式策略优先级与诊断豁免见 [数据契约](../../cli/docs/data-contract.md#按用途分派)；策略只影响准入，观测不被过滤。
- **失败映射**：本层全部拒绝以 `ContractError` 表达，经 CLI 错误映射为输入/预检阻断（退出 2）；计划工件与快照类失败为 `EvidenceError`（退出 4）。退出码总表见 [数据契约](../../cli/docs/data-contract.md#保持的行为)。

## 长上下文速度接入

context_speed_candidates 在平台推荐刷新后应用固定配方并检查模型 context 上限；
context_speed_plan 复用 prepare_plan/write_plan，保留人工审核。
progress 推荐专项结果读取或新计划/batch。见
[CLI 专项接口](../../cli/docs/cli-surface.md#长上下文速度专项)。

## 验证

以下命令在仓库的 `cli/` 目录内执行（从仓库根先 `cd cli`）：

```sh
# 契约结构/语义、配置解析、题包审核、验证复用、社区资源的相关单测
mise exec -- uv run --frozen pytest -q \
  tests/unit/test_contract_validation.py \
  tests/unit/test_unified_contract.py \
  tests/unit/test_config.py \
  tests/unit/test_validation_reuse.py \
  tests/unit/test_bundle_migration.py \
  tests/unit/test_community_candidates.py \
  tests/unit/test_community_preparation.py

# 静态检查（pyproject.toml 中 ruff 配置：py314、100 列）
mise exec -- uv run --frozen ruff check src tests scripts

# schema 导出物与生成源一致（改 contracts/schemas*.py 后必须运行）
mise exec -- uv run --frozen python scripts/export_schemas.py --check

# 社区资源清单与随包题包字节一致
mise exec -- uv run --frozen python scripts/export_community.py --check
```

命令形式对照 [`CONTRIBUTING.md`](../../cli/CONTRIBUTING.md) 的验证一节；Agent 执行时按仓库根 AGENTS.md 约定以 `rtk proxy` 包裹。

## 本地引擎候选与绑定

`config/local_engines` 提供安装声明候选、结构化启动描述及完整绑定输出，
`contracts/schemas_local_engines.py` 持有显式配置分支。`config/single_plan.py`
把该分支的温度策略冻结进正式单次计划；`config/plan_inputs.py` 绑定资产清单。
公开参数与新目录产物见[本地引擎接口](../../cli/docs/cli-reference/local-engines.md)。

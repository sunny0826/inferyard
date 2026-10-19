# Catalogue 事实源与生成物

本文属于 [Catalogue 与 Schema 生成](catalogue-generation.md)。

## 职责与事实源

| 部分 | 责任 | 事实源 |
| --- | --- | --- |
| 方法 catalogue 生成源 | 20 类、199 个方法条目的唯一文本来源；类号 + 类内序号构成稳定 `method_id` | [`methods-matrix.md`](../../cli/docs/reference/methods-matrix.md)，解析器 `methods` 在 [`export_catalogue.py`](../../cli/scripts/export_catalogue.py) |
| 指标 catalogue 生成源 | 34 组核心 + 9 组条件指标的唯一文本来源；字段语义见 [指标定义](../../cli/docs/experiments/metrics.md) | [`metrics.md`](../../cli/docs/experiments/metrics.md)，解析器 `metrics` 在 [`export_catalogue.py`](../../cli/scripts/export_catalogue.py) |
| catalogue 导出 | 从两个生成源生成 `methods.json` / `metrics.json` 并核对稳定 ID；`--check` 只查不写 | `main` 在 [`export_catalogue.py`](../../cli/scripts/export_catalogue.py)；生成物 [`methods.json`](../../cli/src/inferyard/data/methods.json)、[`metrics.json`](../../cli/src/inferyard/data/metrics.json) |
| JSON Schema 导出 | 从契约包定义导出每个注册 kind 的 `*.schema.json`；`--check` 查漂移，非 check 模式移除注册表外旧文件 | `schemas_for` / `export_schema` 在 [`contracts/schemas.py`](../../cli/src/inferyard/contracts/schemas.py)，驱动 `main` 在 [`export_schemas.py`](../../cli/scripts/export_schemas.py)；生成物目录 [`schemas/`](../../cli/schemas/README.md) |
| 社区资源导出 | 复制已审题包原字节、生成/重验固定归档派生的 runtime profile、重建 `resources.json` 全量清单 | `generated` 在 [`export_community.py`](../../cli/scripts/export_community.py)；题包清单 `BUNDLES` 在 [`community_resources.py`](../../cli/src/inferyard/config/community_resources.py)；profile 定义 `PROFILES`、`profile_header`、`output_files`、`validate_profile` 在 [`runtime_profiles.py`](../../cli/src/inferyard/platforms/runtime_profiles.py)；归档成员解析 `member_records` 在 [`runtime_archive.py`](../../cli/src/inferyard/platforms/runtime_archive.py)；题包审核 `require_review` 在 [`bundle.py`](../../cli/src/inferyard/config/bundle.py) |
| 运行时约束导出 | 核对 `uv export` 运行依赖闭包与 `uv.lock` 在三平台目标下逐一相等，写约束文件与 `.validation.json` 证明 | `main` / `validate` / `closure` 在 [`export_runtime_constraints.py`](../../cli/scripts/export_runtime_constraints.py)，输入 [`uv.lock`](../../cli/uv.lock)、[`pyproject.toml`](../../cli/pyproject.toml) |
| catalogue 运行时读取 | 从安装包资源读 catalogue JSON，逐项校验后返回；`components` 目录为运行时计算，不是生成文件 | `catalogue` / `components` 在 [`registry.py`](../../cli/src/inferyard/registry.py)；CLI 出口在 [`application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py) 的 catalogue 命令 |
| 社区资源运行时读取 | 按 `resources.json` 清单核验 SHA-256 后返回包内资源字节 | `resource_bytes` 在 [`community_resources.py`](../../cli/src/inferyard/config/community_resources.py)，消费方 [`community_workspace.py`](../../cli/src/inferyard/config/community_workspace.py)、[`runtime_profiles.py`](../../cli/src/inferyard/platforms/runtime_profiles.py) |
| 报告模板与内容身份 | 包内 `templates/*.html` 为唯一模板来源，整目录哈希计入报告身份 | `template_name` / `template_hash` 在 [`report_assets.py`](../../cli/src/inferyard/reporting/report_assets.py)，消费 `template_sha256` 在 [`report.py`](../../cli/src/inferyard/reporting/report.py) |
| 实现身份的职责清单 | 按 measurement/scoring/presentation 三角色登记负责文件，作为实现身份计算的输入 | [`implementation-files.json`](../../cli/src/inferyard/data/implementation-files.json)，读取方 `IdentityContext` 在 [`implementation_identity.py`](../../cli/src/inferyard/implementation_identity.py)，源哈希 `tool_source_hash` 在 [`provenance.py`](../../cli/src/inferyard/provenance.py) |
| engine-fit 包内资产 | 随包交付的受控 MLX 服务脚本，从包内资源路径解析 | [`engine_fit_assets.py`](../../cli/src/inferyard/config/engine_fit_assets.py)，资产 [`mlx_server.py`](../../cli/src/inferyard/data/engine_fit/mlx_server.py) |

关键事实源约定：

- catalogue 的生成源只有两个文件——[指标定义](../../cli/docs/experiments/metrics.md) 与 [方法目录](../../cli/docs/reference/methods-matrix.md)；`components` 组件目录由 [`registry.py`](../../cli/src/inferyard/registry.py) 在运行时直接计算，不经过导出脚本。生成源与输出路径是 [`export_catalogue.py`](../../cli/scripts/export_catalogue.py) 顶部的 `METRICS` / `MATRIX` / `OUTPUT` 常量，移动文件必须同步改脚本；运行期消费方只经 `catalogue()` 入口，不与 JSON 文件布局耦合。
- 生成物是事实源的字节固化：导出脚本为生成源记录 `{path, sha256}`（`reference` 在 [`export_catalogue.py`](../../cli/scripts/export_catalogue.py)），catalogue 的 `source` / `evidence_refs` 字段把定义绑定到真实文件字节。
- 运行时代码一律通过 `importlib.resources`（`files("inferyard")`）从安装包内读取资源，不依赖源码检出目录；wheel/sdist 的显式 include 清单见 [`pyproject.toml`](../../cli/pyproject.toml) 的 hatch 构建段，`data/`、`templates/`、`data/community/` 均在包内。

- 工具身份覆盖包内全部 `.py` / `.html` / `.json`（`tool_source_hash` 在 [`provenance.py`](../../cli/src/inferyard/provenance.py)），修改 catalogue、模板或 `implementation-files.json` 都会改变测量实现身份，并与冻结计划的 `definition_versions` 校验联动（`require_execution_support` 在 [`registry.py`](../../cli/src/inferyard/registry.py)）。

打包边界按 [`pyproject.toml`](../../cli/pyproject.toml) 的 include 清单执行：`src/inferyard/**/*.py`、`templates/**/*.html`、`data/*.json`（catalogue、实现清单）与 `data/community/` 的 `resources.json`、README、三平台 `*.example.toml`、`bundles/*.json`、`profile/*.json`；`__pycache__` 与字节码排除。`data/engine_fit/mlx_server.py` 作为包内 `.py` 资源随包交付，由 [`engine_fit_assets.py`](../../cli/src/inferyard/config/engine_fit_assets.py) 解析路径后交给受控服务脚本使用；真实设备配置、证据、缓存与模型资产不在打包范围内。

### 生成物与版本

- `methods.json`：固定 199 个条目，`catalogue_version = 2026-09-29.v1`，每条含 `method_id / category_id / original_rank / title / rationale / procedure / metric_rules / evidence / applicability / source_line`；`original_rank` 保留类内原始排序，使调序可审计、不重分配 ID；目录头部记录生成源的 path 与 SHA-256。目录刻意包含当前工具范围之外的候选方法，列出不等于支持。
- `metrics.json`：固定 43 条（34 core + 9 conditional），`catalogue_version = phase2.v1`，每条带 `schema_version = 3`；`unit`、`layer`、`source`、`required_capabilities` 由脚本按 `metric_id` 前缀派生（`UNITS` / `LAYERS` / `source` 映射在 [`export_catalogue.py`](../../cli/scripts/export_catalogue.py)），`implementation_status` 取 `implemented` / `planned`，`E`、`N`、`U` 前缀标记 `scope = conditional`；已实现条目附加实现与测试文件的 `evidence_refs`（如 `L06` / `L07` 额外绑定 [`engine_timing.py`](../../cli/src/inferyard/analysis/engine_timing.py) 与其测试），未实现条目只绑定指标定义本身——目录列出方法不等于声明该算法已实现。
- `schemas/*.schema.json`：每个注册 kind 一份，由 `export_schema(name)` 现场序列化；目录内不属于当前注册表的旧文件由非 check 导出移除。kind 清单与用途见 [schemas README](../../cli/schemas/README.md)。schema 源除 [`contracts/schemas.py`](../../cli/src/inferyard/contracts/schemas.py) 外，扩展协议与模型来源/转换记录的纯结构定义分别在 [`schemas_extensions.py`](../../cli/src/inferyard/contracts/schemas_extensions.py) 与 [`schemas_lineage.py`](../../cli/src/inferyard/contracts/schemas_lineage.py)。
- `data/community/resources.json`：社区资源树（题包、profile、三平台示例配置、README）全量 `{相对路径: SHA-256}` 清单，由 `generated` 重建；运行时 `resource_bytes` 逐字节核验。
- 社区 profile：两份固定归档派生 profile（`PROFILES` 以 `RELEASE = prism-b10743-adfffbe` 命名，win-cpu-x64 与 win-cuda-12.4-x64）。带归档生成时逐份核对 ZIP 大小与 SHA-256，用 `member_records` 展开成员并按 `(role, path)` 排序，`output_files` 派生 `engine_binary`、`runtime_library_manifest`、`libraries`，最后过 `validate_profile`；无归档模式只重验已提交的 profile 字节，不引入新的信任。
- `templates/report*.html`：离线 HTML 报告的全部页面片与脚本样式（v3 报告固定 `report.html` 入口，`template_name` 校验版本号段）；渲染走 Jinja2 `PackageLoader("inferyard", "templates")`（[`report_common.py`](../../cli/src/inferyard/reporting/report_common.py)），CSP 脚本哈希按模板版本重算，冻结快照跨脚本修改保持字节稳定；整目录哈希作为报告呈现身份。
- `implementation-files.json`：三角色负责文件清单，`IdentityContext` 据此计算每个角色的 `files + dependencies` 身份（角色清单语义见 [架构](../../cli/docs/architecture.md) 与 [职责身份契约](../../cli/docs/contracts/scoped-measurement-contract.md)）。
- 版本关系：`SCHEMA_VERSION = 3`（[`__init__.py`](../../cli/src/inferyard/__init__.py)）是 wire revision；catalogue 的 `catalogue_version` 与指标 `definition_version` 独立管理，schema 文件不随定义版本自动变化，反之亦然。

包内并行三套一致性机制，覆盖不同资源面：`resources.json` 管社区资源树逐字节完整性（`resource_bytes` 核验）；`implementation-files.json` + `tool_source_hash` 管测量/评分/呈现三角色的实现身份（`IdentityContext` 计算）；`template_hash` 管报告呈现身份。三者都不依赖 Git 或源码检出，安装包自证。其中 `implementation-files.json` 是人工维护的已审职责清单，不被任何 `--check` 覆盖，修改它与修改源码同属实现身份变更。

`components()` 组件目录（运行时计算，非生成文件）登记适配器、采集器、扩展协议与评分器共 23 项：适配器与采集器逐项标注能力范围并要求实测核验；评分器条目绑定 `scorer_hash()`（[`scoring.py`](../../cli/src/inferyard/analysis/scoring.py)）且 `live_verification_required = false`，因为确定性评分器本身不需真机。`adapter_factory` / `collector_factory` 只路由到已实现组件，未知标识抛 `ContractError("... not implemented")`。

### 生成源的解析契约

导出脚本对 Markdown 结构有硬性假设，变更生成源时必须保持：

- 方法目录按 `^## (\d+)\. (.+)$` 识别类别标题，类别内表格行必须恰好 7 列（序号、名称、理由、步骤、指标规则、证据、适用前提）；`method_id = method-{类别号}-{序号}`，`source_line` 记录条目在源文件中的行号。
- 指标表按 `| ID |` 起始行识别，`E` / `N` / `U` 前缀为条件指标用 3 列，其余 4 列；ID 前缀决定 `source` 枚举（`Q` 评分器、`L` 流事件、`C` 平台采集、`R` 请求账本、`S` 轮次窗口账本、`X` 长度用例账本、`E` 已核验传感器、`N` 负载事件、`U` 已核验生命周期事件）。
- 生成源的行号、表格列数、条目总数任何漂移都会在导出时抛错，而不是产出错位的 JSON。

### 导出侧与运行侧的环境分离

导出脚本只活在源码检出中：读取 `docs/`、`cli/bundles/`、`uv.lock` 并写回包内资源，安装用户从不运行它们。运行侧只认安装包：wheel 内的 `data/`、`templates/`、`data/community/` 由 `importlib.resources` 读取，editable 安装与 wheel 安装走同一代码路径（[`community_resources.py`](../../cli/src/inferyard/config/community_resources.py) 的模块 docstring 明确此约定）。因此 `--check` 漂移核对是两边唯一的一致性闸门：生成物手改、生成源改动未重导出、归档派生 profile 过期，都会在核对时暴露，而不会等到运行期。


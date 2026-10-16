# Catalogue 与 Schema 生成

本子系统把可审查的 Markdown 生成源与契约包定义固化为随安装包交付的 JSON 资源（方法/指标 catalogue、JSON Schema、社区资源清单与报告模板），并提供 `--check` 漂移核对。它只负责“生成与一致性”：不解释指标语义与比较资格（见 [评分、比较与报告](scoring-reporting.md)），不执行运行时消费（见 [运行时执行与引擎适配](runtime-execution.md) 与 [CLI 与应用分发](cli-dispatch.md)），不发行分发包（见 [脚本手册](../../cli/scripts/README.md) 的发行物一节）；通用层职责见 [架构](../../cli/docs/architecture.md)。

边界另一侧同样清楚：题包创建脚本（`build_*_bundle.py` 系列）产出待审原题与审核材料，属 [配置、契约与冻结计划](config-plans.md) 范畴；`export_community.py` 只搬运**已审核**题包的原字节，不创建或修改题目内容。运行时对 catalogue 与模板的消费不做网络请求，也不据此声明任何实测资格。

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

## 运行路径

### 维护路径：改生成源或契约后重新导出

1. 修改 catalogue 生成源（仅 [metrics.md](../../cli/docs/experiments/metrics.md) 或 [methods-matrix.md](../../cli/docs/reference/methods-matrix.md)）或 schema 定义（[`contracts/schemas.py`](../../cli/src/inferyard/contracts/schemas.py)，按配置、题包/评分、事件/采样、实验、封存/汇总分段）。
2. 在 `cli/` 目录运行导出脚本（不带 `--check`）：`scripts/export_catalogue.py` 重新生成两个 catalogue JSON；`scripts/export_schemas.py` 重新生成 `schemas/*.schema.json` 并删除注册表外旧文件与空目录。
3. 若改动触及 `src/inferyard/data/community/`（题包、profile、示例配置或 README），同一轮必须重生成 `resources.json`：调用 `generated(None)`（无归档，只重验并回写已提交的包内资源），或存在三份固定 Windows ZIP 归档时带 `--archives` 从归档派生 profile。
4. 若运行依赖变化，按目标平台重导出 runtime 约束（`--out` 新路径），`.validation.json` 随约束文件一起提交。
5. 提交生成物与生成源的同一份 diff；本地与 CI 用对应 `--check` 核对漂移。注意 `--check` 会从生成源重新计算期望内容（catalogue 甚至重读 Markdown），所以它同时暴露“源已改未重导出”和“产物被手改”两类漂移。

### 消费路径：安装包内资源到运行结果

1. CLI `catalogue --kind methods|metrics|components` 经 [`application/dispatch.py`](../../cli/src/inferyard/application/dispatch.py) 调用 `catalogue(kind)`（[`registry.py`](../../cli/src/inferyard/registry.py)），后者用 `importlib.resources` 读取包内 JSON；`metrics` 逐项过 `metric_definition` 结构校验，`methods` 查重 `method_id`，结果装入 `CommandResult("catalogue", "listed", "complete", details=...)`（[`types.py`](../../cli/src/inferyard/application/types.py)），details 即 catalogue 文档。
2. 指标定义被评分/汇总路径惰性加载：`Observations`（[`observations.py`](../../cli/src/inferyard/analysis/observations.py)）、`public_metrics`（[`public_metrics.py`](../../cli/src/inferyard/analysis/public_metrics.py)）从 `catalogue("metrics")` 取 `metric_id → definition` 映射。
3. `init` / 准备工作区时 `resource_bytes`（[`community_resources.py`](../../cli/src/inferyard/config/community_resources.py)）按 `resources.json` 的 SHA-256 逐个核验包内题包、三平台示例配置与路线图 README 后写出到操作者目录（[`community_workspace.py`](../../cli/src/inferyard/config/community_workspace.py)）；路线图模板中的 `{{WORKSPACE}}` 占位在写出时替换为真实目录。
4. 生成离线报告时 `template_hash`（[`report_assets.py`](../../cli/src/inferyard/reporting/report_assets.py)）对 `templates/` 下全部 `report*.html` 计算整目录哈希，由 [`report.py`](../../cli/src/inferyard/reporting/report.py) 写入 `template_sha256`，绑定报告呈现身份；报告内容与展示规则见 [报告契约](../../cli/docs/contracts/report-contract.md)。
5. 离线指标核对（`public_metrics_check` 在 [`public_metrics_check.py`](../../cli/src/inferyard/analysis/public_metrics_check.py)）同样从 `catalogue("metrics")` 取定义，核验派生指标的缺测与口径。
6. `config create` 绑定 MLX 随包服务脚本时，`engine_fit_assets`（[`engine_fit_assets.py`](../../cli/src/inferyard/config/engine_fit_assets.py)）从包内 `data/engine_fit/` 解析 `mlx_server.py` 路径，不读源码检出。

以上消费路径全部离线：catalogue 命令、报告渲染与资源写出不访问网络，也不因读取资源而声明任何平台或实测资格。

### 变更触发表

| 变更 | 必须动作 | 核对 |
| --- | --- | --- |
| 改 [`metrics.md`](../../cli/docs/experiments/metrics.md) 或 [`methods-matrix.md`](../../cli/docs/reference/methods-matrix.md) | 重跑 `export_catalogue.py` | `export_catalogue.py --check` |
| 改 [`contracts/`](../../cli/src/inferyard/contracts/schemas.py) 下 schema 定义 | 重跑 `export_schemas.py` | `export_schemas.py --check`（根 `mise run schemas`） |
| 改 `data/community/` 任何文件 | 同轮重生成 `resources.json`（`generated(None)` 或 `--archives`） | `export_community.py --check` |
| 改运行依赖或 [`uv.lock`](../../cli/uv.lock) | 重导出 runtime 约束到新 `--out` 路径 | `.validation.json` 随文件走，`.validation.json` 漂移即失败 |
| 改 `templates/*.html` | 无导出步骤；`template_hash` 在报告时整目录现算 | 报告回归中的 `template_sha256` 与封存核验 |
| 改 [`implementation-files.json`](../../cli/src/inferyard/data/implementation-files.json) | 无导出步骤；身份在运行时计算 | `IdentityContext` 相关回归 |
| 改导出脚本本身的解析或序列化逻辑 | 重跑受影响导出并复核生成物 diff | 对应 `--check` + `ruff check src tests scripts` |

模板没有 `--check` 的原因是其身份没有独立生成物可漂移——`template_hash` 在报告时对包内目录现算，唯一事实是“模板被改”，它直接反映在报告 `template_sha256` 上。

### 导出脚本行为

| 脚本 | 输入 | 输出 | `--check` 语义 | 非 check 副作用 |
| --- | --- | --- | --- | --- |
| `export_catalogue.py` | 两个 Markdown 生成源 | `data/methods.json`、`data/metrics.json` | 比对磁盘内容与重算文本，stale 即 `SystemExit` | 重写两份 JSON；删除过时的 `methods-v1.json` / `metrics-v2.json` |
| `export_schemas.py` | 契约包 `schemas_for()` 注册表 | `schemas/<kind>.schema.json` | 缺失或内容不符记为 stale | 写全部 schema；删除注册表外旧 `*.schema.json` 与空目录 |
| `export_community.py` | `cli/bundles/` 已审题包、固定 ZIP 归档（可选）、包内既有资源 | `data/community/` 全树与 `resources.json` | 逐文件比对字节并核对树内无未登记文件 | 重写社区资源树；结束时打印 `{checked, resources, archives_verified}` 摘要 |
| `export_runtime_constraints.py` | `uv.lock`、`pyproject.toml`、`uv export --frozen --no-dev` 结果 | 约束 txt + `.validation.json` 证明 | 重算证明并与 `.validation.json` 比对 | 仅写新路径（已存在即失败），不改动 lock 或项目定义 |

四个脚本的共同点：都只从活动源定义或锁文件派生，不联网、不启动服务、不执行模型资产；输出序列化固定为 UTF-8、`ensure_ascii=False`、两空格缩进、结尾换行，使 `--check` 的字节比较稳定且 diff 可读。脚本直接 import `inferyard` 契约包，必须在 `uv run` 环境内运行（上节命令已体现）。`export_community.py` 不带 `--archives` 且不带 `--check` 会被 `parser.error` 拒绝，因为无归档的生成没有信任来源。`export_runtime_constraints.py` 的 `--check` 不调用 `uv export`，直接读 `--out` 约束文本并从 `uv.lock` 重算证明，因此核对不需要网络。

导出的 `schemas/*.schema.json` 只服务包外消费者（审查、外部工具），运行时不读它们：CLI 内的结构/语义校验走契约包内的注册表（`Document.parse` / `validate_document`，见 [schemas README](../../cli/schemas/README.md)），两份内容同源但用途不同；catalogue 的 JSON 则相反——运行时就地读取包内文件。`cli/schemas/` 位于项目根而非 `src/inferyard/`，也不在 sdist 的 `only-include` 内，发行包不含导出 JSON。

## 约束与失败行为

- **稳定 ID 不重分配**。`method_id` 由类号与类内序号拼成（`method-{类号}-{序号}`），指标用 `metric_id`（如 `L06`）。调整展示顺序不得重新分配既有 ID；改名或换绑定需走审核过的版本或映射变更。导出脚本在生成物已存在时逐 ID 比对标题，发现身份变化直接 `SystemExit("catalogue identity changed")`，不静默改写。
- **生成物不手改**。`methods.json`、`metrics.json`、`schemas/*.schema.json` 与 `resources.json` 只能由导出脚本生成；手工编辑会被 `--check` 判为 stale / drift，残留的过时 catalogue 文件名（`methods-v1.json`、`metrics-v2.json`）在 `--check` 下同样报错。`export_schemas.py` 非 check 模式还会删除不属于当前注册表的旧 `*.schema.json` 并清理空目录。
- **数量与结构是硬校验**。方法条目必须恰好 199 个唯一 ID，指标恰好 43 个（34 core + 9 conditional），生成源表格列数变化抛 `ValueError`，防止静默错列解析。
- **JSON Schema 只证结构**。`export_schema` 只描述单文档结构；跨字段/跨记录、状态转换与血缘哈希校验由解析器和运行时执行（[`contracts/validation.py`](../../cli/src/inferyard/contracts/validation.py) 等），规则见 [schemas README](../../cli/schemas/README.md) 与 [数据契约](../../cli/docs/data-contract.md)。结构合法不授予测量或比较资格。schema 注册表只依赖契约包与根版本常量，`--schema` 查询不加载配置业务或实时执行器；没有版本选择参数或活动旧版注册表。
- **题包必须已审核**。导出社区题包前对每个 bundle 调 `require_review`（[`bundle.py`](../../cli/src/inferyard/config/bundle.py)），未审核内容拒绝进入资源；改题或换字节使原审核证明失效，需重审。
- **归档身份逐字节核验**。带 `--archives` 时逐份核对三份 ZIP 的大小与 SHA-256（不符抛 `archive_identity_mismatch`）再派生成员与库清单；无归档的 `--check` 只核对已提交资源与题包漂移，不是重新验证归档。脚本不下载、不执行任何模型资产。
- **社区资源树必须完整**。树的构成由 `generated` 固定：三份已审题包原字节（`zh-smoke`、`zh-core`、`zh-svg-pelican`）、两份 profile、三平台 `*.example.toml`、`README.md` 与 `resources.json`；题包集合不符（缺三平台模板抛 `three_platform_templates_required`）、profile 校验不过或树内出现未登记文件（`unexpected community resources`）都会失败。向树内新增资源必须同时改导出脚本与 `resources.json`。
- **运行时资源哈希不符即拒绝**。`resource_bytes` 校验失败抛 `PreparationError("package_resource_invalid", 4)`，安装包资源损坏不会静默产出候选。
- **catalogue 不夸口能力**。`implementation_status` 只有 `implemented` / `planned`（测试断言不存在 `verified`）；`components()` 中各适配器与采集器标 `live_verification_required: true`，软件/模拟验证不授予真机资格，登记状态与实测资格的解释见 [架构](../../cli/docs/architecture.md) 与 [指标定义](../../cli/docs/experiments/metrics.md)。
- **runtime 约束必须精确闭包**。导出调用 `uv export --color never`，即使环境强制彩色输出也只接收纯文本。约束文件要求逐条 `==` 精确版本、无 URL / extras / 通配；`linux-x64`、`windows-x64`、`macos-arm64` 三目标（按 `sys_platform` / `platform_system` / `platform_machine` 模拟求值，解释器固定 `3.14.7`）的导出闭包须与 `uv.lock` 逐一相等，`.validation.json` 漂移同样失败；输出路径已存在即 `FileExistsError`，导出只写新文件。锁内非 registry 来源或歧义匹配（`ambiguous_or_non_registry_runtime_dependency`）、重叠约束与不可达约束分别抛错。
- **定义版本与冻结计划联动**。冻结计划绑定 `definition_versions`（measurement `phase2.v1`、scoring `SCORER_VERSION`、comparison `phase2.v1`）；`require_execution_support`（[`registry.py`](../../cli/src/inferyard/registry.py)）在执行前拒绝版本不符的计划。因此 catalogue 版本号或评分器版本的提升属于契约级变更，旧计划不会被静默接受。
- **模板与 catalogue 不依赖源码检出**。所有读取走 `importlib.resources`，editable 与 wheel 安装行为一致（[`community_resources.py`](../../cli/src/inferyard/config/community_resources.py) 的模块 docstring 即此约定）；`--help`、`--versions`、`--schema` 与 `catalogue` 命令只读包内元数据，不加载实时后端。
- **未知 catalogue kind 拒绝**。`catalogue(kind)` 只接受 `methods` / `metrics` / `components`，其余抛 `ContractError("catalogue.kind", "unsupported catalogue")`（[`registry.py`](../../cli/src/inferyard/registry.py)）。
- **生成物按严格 JSON 读取**。catalogue 与 `resources.json` 都过 `strict_json_loads`（[`contracts/validation.py`](../../cli/src/inferyard/contracts/validation.py)），拒绝重复键与非有限数值；包内资源被篡改会在读取期暴露，而不是静默产出错误定义。

## 验证

以下命令在仓库的 `cli/` 目录内执行（从仓库根先 `cd cli`），环境为 mise 固定的 Python 3.14 + uv：

```sh
# catalogue 与 schema 生成物漂移核对（生成源或契约定义变更后必须先重导出再核对）
mise exec -- uv run --frozen python scripts/export_catalogue.py --check
mise exec -- uv run --frozen python scripts/export_schemas.py --check
mise exec -- uv run --frozen python scripts/export_community.py --check

# catalogue 行为回归：199 方法稳定 ID、43 指标完整性、CLI 输出与包资源解耦
mise exec -- uv run --frozen pytest -q tests/unit/test_registry.py

# 静态检查覆盖导出脚本与包源码
mise exec -- uv run --frozen ruff check src tests scripts
```

本仓库 Agent 约定以前缀 `rtk proxy` 包裹 mise 命令（仅代理 agent 命令，依赖仍由 uv 管理）；按 [`CONTRIBUTING.md`](../../cli/CONTRIBUTING.md) 同一清单，提交前还可运行 `git diff --check` 检查空白问题。

核对方式：前三条与 pytest / ruff 命令逐字对照 [`CONTRIBUTING.md`](../../cli/CONTRIBUTING.md) 的 “Changes and verification” 一节与根 [`mise.toml`](../../mise.toml)（`tasks.schemas` 即第二条，`tasks.test` / `tasks.lint` 对应 pytest / ruff，也可直接 `mise run schemas`）；`export_community.py --check` 对照 [`scripts/README.md`](../../cli/scripts/README.md) 的社区资源一节。三个 `--check` 命令均幂等只读，可重复执行；不带 `--check` 的导出才会写盘。`export_runtime_constraints.py` 为专项发行工具（`--out` 必填、只写新路径、非 check 模式调用 `uv export`），按 [`scripts/README.md`](../../cli/scripts/README.md) 在候选构建序列中使用，不作为常规回归。

相关回归的覆盖面：[`test_registry.py`](../../cli/tests/unit/test_registry.py) 断言 199 方法稳定 ID、43 指标的 core/conditional 分布与 `implemented` 集合（与导出脚本的 `IMPLEMENTED` 常量互相锁定，改任一处必须同步）、CLI catalogue 输出与包资源解耦、组件工厂只路由已实现组件，并在 cp1252 默认编码环境下验证读取显式 `utf-8` 不受平台编码影响；社区资源读写由 `tests/unit/test_community_candidates.py`、`test_community_preparation.py`、`test_community_runtime.py` 覆盖；模板身份由报告回归（含 `template_sha256` 与封存报告核验）间接覆盖。完整回归为 `mise exec -- uv run --frozen pytest`（根 `mise.toml` 的 `tasks.test`），上文给出的是本子系统相关子集。

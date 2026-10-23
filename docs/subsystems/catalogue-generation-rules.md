# Catalogue 约束与验证

本文属于 [Catalogue 与 Schema 生成](catalogue-generation.md)。

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
- **runtime 约束必须精确闭包**。导出调用 `uv export --color never`，即使环境强制彩色输出也只接收纯文本。约束文件要求逐条 `==` 精确版本、无 URL / extras / 通配；`linux-x64`、`windows-x64`、`macos-arm64` 三目标（按 `sys_platform` / `platform_system` / `platform_machine` 模拟求值，解释器固定 `3.15.1`）的导出闭包须与 `uv.lock` 逐一相等，`.validation.json` 漂移同样失败；输出路径已存在即 `FileExistsError`，导出只写新文件。锁内非 registry 来源或歧义匹配（`ambiguous_or_non_registry_runtime_dependency`）、重叠约束与不可达约束分别抛错。
- **定义版本与冻结计划联动**。冻结计划绑定 `definition_versions`（measurement `phase2.v1`、scoring `SCORER_VERSION`、comparison `phase2.v1`）；`require_execution_support`（[`registry.py`](../../cli/src/inferyard/registry.py)）在执行前拒绝版本不符的计划。因此 catalogue 版本号或评分器版本的提升属于契约级变更，旧计划不会被静默接受。
- **模板与 catalogue 不依赖源码检出**。所有读取走 `importlib.resources`，editable 与 wheel 安装行为一致（[`community_resources.py`](../../cli/src/inferyard/config/community_resources.py) 的模块 docstring 即此约定）；`--help`、`--versions`、`--schema` 与 `catalogue` 命令只读包内元数据，不加载实时后端。
- **未知 catalogue kind 拒绝**。`catalogue(kind)` 只接受 `methods` / `metrics` / `components`，其余抛 `ContractError("catalogue.kind", "unsupported catalogue")`（[`registry.py`](../../cli/src/inferyard/registry.py)）。
- **生成物按严格 JSON 读取**。catalogue 与 `resources.json` 都过 `strict_json_loads`（[`contracts/validation.py`](../../cli/src/inferyard/contracts/validation.py)），拒绝重复键与非有限数值；包内资源被篡改会在读取期暴露，而不是静默产出错误定义。

## 验证

以下命令在仓库的 `cli/` 目录内执行（从仓库根先 `cd cli`），环境为 mise 固定的 Python 3.15 + uv：

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

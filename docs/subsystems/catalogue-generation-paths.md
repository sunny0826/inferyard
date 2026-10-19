# Catalogue 运行路径

本文属于 [Catalogue 与 Schema 生成](catalogue-generation.md)。

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


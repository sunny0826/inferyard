# 评分、比较与报告

确定性评分、指标归约、比较资格判定、重评分、离线 HTML 报告与导出的实现子系统，覆盖 `analysis/` 与 `reporting/` 两个包。评分输入是冻结题目、参考答案与 answer policy，输出是带身份哈希的 score 文档与指标观测；本子系统只读取已保存证据，不发送模型请求、不调用模型裁判。运行期评分的调用方在执行子系统（见[运行时执行与引擎适配](runtime-execution.md)），日志、封存与账本重建在证据子系统（见[证据、封存与离线读取](evidence-storage.md)），命令入口在[CLI 与应用分发](cli-dispatch.md)）。

「职责与事实源」「运行路径」「约束与失败行为」三节正文已移入 `scoring-reporting/` 目录；其余各节保留在本页。

## 职责与事实源

正文见 [职责与事实源](scoring-reporting/responsibilities.md#职责与事实源)。

## 运行路径

正文见 [运行路径](scoring-reporting/pipelines.md#运行路径)。

## 约束与失败行为

正文见 [约束与失败行为](scoring-reporting/constraints.md#约束与失败行为)。

### 分析身份与谱系

正文见 [分析身份与谱系](scoring-reporting/constraints.md#分析身份与谱系)。

### 限制、失败模式与兼容规则

正文见 [限制、失败模式与兼容规则](scoring-reporting/constraints.md#限制失败模式与兼容规则)。

## 长上下文速度接入

专项 [`analysis/context_speed.py`](../../cli/src/inferyard/analysis/context_speed.py)
按冻结顺序生成五个样本，逐请求计时复用 engine_rates，稳定测量身份由 context_speed_cohort 归约。
[`context_speed_samples.py`](../../cli/src/inferyard/analysis/context_speed_samples.py) 读取已核验的原件，
投影经共享语义与 binding 校验后交给 speed-results；不创建 HTML 或通用 analysis。
资格及缺测规则见[专项契约](../../cli/docs/contracts/context-speed-contract.md)。
通用消费者 guard 由 B2 另行实施，此处不宣称所有通用出口已拒绝专项。

## 验证

以下命令在仓库根执行；根级 mise tasks 已内置 `cd cli`（见 [mise.toml](../../mise.toml) 与 [cli/pyproject.toml](../../cli/pyproject.toml) 的 pytest/ruff 配置）：

```sh
# 全量单元与集成回归（pytest，testpaths = tests）
mise run test

# Ruff 检查源码、测试与脚本
mise run lint

# JSON Schema 与 catalogue 生成物漂移核对
mise run schemas
mise exec -- uv run --frozen python scripts/export_catalogue.py --check

# 评分、比较与重评分回归
mise exec -- uv run --frozen pytest \
  tests/unit/test_scoring.py \
  tests/unit/test_scoring_contract.py \
  tests/unit/test_scoring_revision.py \
  tests/unit/test_rescore.py \
  tests/unit/test_comparison.py \
  tests/unit/test_performance_comparison.py \
  tests/unit/test_quantization_comparison.py \
  tests/unit/test_cache_comparison.py \
  tests/unit/test_observations.py \
  tests/unit/test_repetition_metrics.py

# 报告、SVG 与导出回归
mise exec -- uv run --frozen pytest \
  tests/unit/test_report_content.py \
  tests/unit/test_report_dashboard.py \
  tests/unit/test_report_facets.py \
  tests/unit/test_report_resources.py \
  tests/unit/test_report_scans.py \
  tests/unit/test_svg_render.py \
  tests/unit/test_export.py \
  tests/unit/test_export_utf8.py \
  tests/unit/test_public_metrics.py
```

`cli/scripts/` 下 `verify_*` 专项脚本会启动服务或发送模型请求，不属于本子系统的离线回归，未列入；catalogue 生成源（指标/方法定义）变更后才需要重跑 `export_catalogue.py` 并更新生成物。

## 与既有文档的差异

- [实验设计](../../cli/docs/experiments/design.md) 的比较模式表列出 `device` / `engine` 两种模式，但 `compare_trials` 只接受 `model` / `config` / `side-by-side`，其余模式抛 `unsupported_comparison_mode`；设备与引擎作为被测因素的条件扩展由 engine-fit 比较路径处理，不经本子系统的 `compare` 命令。
- [报告契约](../../cli/docs/contracts/report-contract.md) 的 CSP 一节描述 `default-src 'none'; style-src 'unsafe-inline'` 与 `script-src` 哈希，实际模板 meta 还含 `img-src data:`、`base-uri 'none'`、`form-action 'none'`；契约只列安全关键指令，语义不冲突。

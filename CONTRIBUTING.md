# 贡献指南

先读[产品说明](README.md)、[架构](docs/architecture.md)和相关[主题契约](docs/contracts/README.md)。
Agent 的文件边界与安全要求见 [AGENTS.md](AGENTS.md)。项目采用 [MIT 许可证](LICENSE)，v0.0.1 目前仅为本地候选。

## 开发环境

版本以 [mise.toml](mise.toml) 为准：Python 3.14.7、uv 0.12.18；Go 用于独立 observer。
依赖由 uv 管理，不用系统 Python 或全局 pip 替代。
若继承的 `UV_PYTHON` 指向其他版本，先取消该覆盖或改为 `mise which python` 返回的路径；
确认实际解释器为 3.14.7 后再同步和运行。

```bash
mise which python
mise which uv
mise exec -- python --version
mise exec -- uv --version
mise exec -- uv sync --frozen
mise exec -- uv run --frozen inferyard --help
mise exec -- uv run --frozen inferyard --versions
```

首次同步需要网络，缓存齐全时才用 `--offline`。用户安装与源码开发分开：
[安装指南](docs/installation.md)使用 wheel 和 `uv tool`，开发命令使用 `mise exec -- uv run --frozen`。

## 修改与验证

先查看工作区差异并保留已有修改。行为修复增加覆盖真实失败路径的回归；契约变更覆盖拒绝路径与旧数据读取。
默认使用夹具和模拟服务，真实模型、构建与长时负载另行确定设备、预算和停止条件。

按影响范围选择检查，不必每次全部执行：

```bash
mise exec -- uv run --frozen pytest -q tests/unit/test_RELEVANT.py
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen ruff format --check src tests scripts
mise exec -- uv run --frozen python scripts/export_schemas.py --check
mise exec -- uv run --frozen python scripts/export_catalogue.py --check
git diff --check
```

`test_RELEVANT.py` 替换为相关测试。纯文档只检查链接、命令和 diff；HTML 还检查实际离线渲染。
全量 pytest 留给影响广泛的代码修改或集成检查。本地 TCP 模拟测试需要 socket 权限，沙箱阻断和实现失败分开报告。
运行脚本前读[脚本说明](scripts/README.md)并检查副作用，不批量执行 `verify_*`。

Schema 源在 `src/inferyard/contracts/`，修改后运行导出器（去掉 `--check`）。
`docs/experiments/metrics.md` 和 `docs/reference/methods-matrix.md` 是 catalogue 生成源，
修改时同步导出，已有 ID 不因排版变动重分配。题包及其审核资料保留原哈希关系，改题后重新人工审核受影响项。

## 文档与证据

- README 是产品入口；操作步骤在安装与使用指南，接口规则在数据/主题契约。
- ADR 记录长期设计理由；计划只保留未完成工作，当前进度集中到 [backlog](docs/backlog.md)。
- 不提交会话交接、阶段流水账、真实设备配置、模型、引擎、缓存、凭据或原始运行。
- `validation/` 是忽略的本机证据目录，不用于默认测试或构建。需要回归样本时提取最小、可公开的夹具。
- 不覆盖原始证据；迁移、重评分和报告重建写入新目录，遵循[证据血缘规则](docs/data-contract.md#证据血缘与比较结论)。

## 本地候选构建与安装检查

确认 LICENSE 与包元数据一致后，在干净的最终集成提交上执行。以下大写项均为占位值：
`BUILD`、`INPUTS`、`CANDIDATE`、`STAGE` 是新目录；`OUTSIDE_CHECKOUT` 必须是源码检出外的新目录。
后四种目录的父目录须已存在。`INTEGRATED_SHA` 是构建所用完整提交 SHA，期间不切换源码。
`BUILD` 选择 Git 忽略目录（如 `dist/...`）或源码外目录，避免生成产物使 clean-source 检查失败。

```bash
mise exec -- uv run --frozen python scripts/build_distribution.py --out BUILD
mise exec -- uv run --frozen python -m tests.packaging.prepare_inputs --out INPUTS
mise exec -- uv run --frozen python tests/packaging/run_installed.py --manifest BUILD/manifest.json --wheel WHEEL --constraints CONSTRAINTS --inputs INPUTS --out OUTSIDE_CHECKOUT
mise exec -- uv run --frozen python scripts/prepare_release_candidate.py --manifest BUILD/manifest.json --installed OUTSIDE_CHECKOUT/result.json --source-commit INTEGRATED_SHA --out CANDIDATE
mise exec -- uv run --frozen python scripts/verify_release_candidate.py --manifest CANDIDATE/manifest.json --manifest-sha256 PRODUCER_OUTPUT_SHA --source-commit INTEGRATED_SHA --stage STAGE
```

`WHEEL` 使用 `BUILD/packages/inferyard-0.0.1-py3-none-any.whl`，`CONSTRAINTS` 使用
`BUILD/attachments/runtime-constraints.txt`。构建同时核验 sdist 重建结果和约束校验文件。
`PRODUCER_OUTPUT_SHA` 取自 `prepare_release_candidate.py` 的 JSON 输出字段 `manifest_sha256`。
保留构建清单、安装结果及候选原件，不手工编辑 JSON 代替通过检查。

安装检查使用合成输入，在独立环境实际检查 uv tool/uvx、核心离线命令、工作区导出及 report/verify，
输出 `installed_safe_checks.v2`；首次准备 Python 和依赖需要联网，不发送模型请求。
至少一个平台完成后可生成 `community_distribution.v2` 候选；每个额外实测平台使用重复的
`--installed RESULT` 参数，未测平台保留 `not_verified`。这不代表原生模型准备或性能验收。

核验绑定源码、版本、许可证、清单及产物摘要，暂存使用已核验的字节快照。`--packages-only`
只暂存 wheel/sdist，仍执行完整候选核验。上述命令均不上传；手动发布 workflow 的默认模式为
`verify-only`，要求指定来源 package-check run、源码提交和候选摘要，详见[脚本说明](scripts/README.md#社区资源与发行检查)。
当前候选工作和发布前剩余项见 [backlog](docs/backlog.md)。
提交应说明改动、实际检查和未验范围；完成相关检查后创建本地 commit，不 push。

## 逐题人工审核

可选 `case_review_records` 只对已有匹配记录的题免重审。首次采用时需人工核对每题，
不能将失配旧整包批准自动转为逐题批准。以下命令只输出摘要，不生成批准：

```bash
mise exec -- uv run --frozen python - bundles/example.json <<'PY'
import sys
from pathlib import Path
from inferyard.evidence.storage import read_json
from inferyard.config.bundle import validate_bundle
from inferyard.config.bundle_review import case_content_hash
bundle = read_json(Path(sys.argv[1]))
validate_bundle(bundle)
for case in bundle['cases']:
    print(case['case_id'], case_content_hash(bundle, case))
PY
```

将路径替换为待审题包。人工检查题目、答案、规则、类别、协议与共享答案政策后，
把实际摘要和审核人填写到该题包的新数组中，例如（占位值必须替换）：

```json
{
  "case_review_records": [
    {
      "definition": "case-review.v1",
      "case_id": "instruction-01",
      "content_sha256": "替换为上面输出的64位十六进制摘要",
      "reviewer": "实际审核人",
      "reviewed_at": "2026-10-07",
      "conclusion": "approved"
    }
  ]
}
```

这是新增字段片段，不是完整题包。每个未被匹配旧整包审核覆盖的题都需要记录。
编辑题意后重审受影响题；共享协议/答案政策改变会影响全部题。已有匹配逐题记录时，
展示版本和许可说明变化无需内容重审。旧整包哈希和历史原件保留；不再适用的旧迁移
证明不能用于编辑后副本，处理规则见[数据契约](docs/data-contract.md#按用途分派)。

# AGENTS.md

Applies to this repository and its subdirectories. Communicate concisely in Chinese by default and keep code identifiers as-is. README, CHANGELOG.md, CONTRIBUTING.md, MACOS.md, WINDOWS.md, Git commits and PRs are written in English; README also ships a Chinese version at [README.zh-CN.md](README.zh-CN.md). AGENTS.md 与其他文档暂用中文。

## Getting started

1. Inspect the working-tree diff and preserve existing changes. Read the [README](README.md) first to confirm the product and platform scope.
2. Read the [CLI reference](cli/docs/cli-surface.md), [architecture](cli/docs/architecture.md), [data contract](cli/docs/data-contract.md), [experiment design](cli/docs/experiments/design.md) and [metrics](cli/docs/experiments/metrics.md) as the task requires. The full entry point list is in the [documentation map](cli/docs/README.md).
3. For platform work read [MACOS.md](cli/MACOS.md) / [WINDOWS.md](cli/WINDOWS.md); for configuration and bundle work read the README of the relevant directory. Read [scripts/README.md](cli/scripts/README.md) before running any script and check its arguments and side effects.
4. Distinguish implementation, simulated verification and real-device evidence. When docs and source disagree, point out the difference and verify it; never silently adopt the looser reading.

本仓库为 monorepo：`cli/` 是 Python CLI 主产品（含独立离线 HTML 报告），`studio/` 是 MyGo 原生桌面面板。 The package name is `inferyard`, the entry points are `inferyard` / `python -m inferyard`; the only active contract is `schema_version = 3`.

## Source boundaries

| 仓库根目录 | 职责 |
| --- | --- |
| `cli/` | Python CLI 与离线 HTML 报告 |
| `studio/` | MyGo 原生桌面应用（Go，独立 module `inferyard/studio`） |

下表为 CLI 内部源码边界：

| Directory (under `cli/src/inferyard/` unless noted) | Responsibility |
| --- | --- |
| `cli/`, `application/` | Arguments, request construction, output, error mapping; shared types and lazy dispatch |
| `contracts/`, `config/` | Structural and semantic validation; configuration, bundles, frozen plans and declaration binding |
| `runtime/`, `adapters/` | Execution, cancellation, draining and safety controls; engine transport and parsing |
| `evidence/` | Journals, sealing, ledgers, provenance and current-format rejection |
| `analysis/`, `reporting/` | Scoring, metrics, comparison, rescoring, reports and exports |
| `platforms/`, `extensions/` | Device and process sampling; the standalone extension protocol |
| `data/`, `templates/` | Catalogue data and report templates shipped with the package |

Business modules do not depend on the CLI; application request/result types live only in `application/types.py`. Help, version and schema queries must not load the live backend; installed resources are read from the package, not from the source checkout.
Modify active source code, not the historical snapshots in the local repository-root `validation/` directory. Follow the Ruff configuration (Python 3.14, 100 columns); keep code files under ~500 lines and split new responsibilities into separate modules.
Before parallel collaboration, assign file ownership first. Only modify authorized files in your own workspace and do not revert other people's changes; report the files and the reason before crossing a boundary.

## Environment and checks

Versions are pinned by [mise.toml](mise.toml); CLI dependencies by `cli/pyproject.toml` / `cli/uv.lock`. Confirm the actual mise paths and versions before developing; do not substitute system Python or global pip. RTK only wraps agent commands; dependencies are managed by uv. Source-development docs use `mise exec -- uv run --frozen`; installed users run `inferyard` directly.

CLI 命令在仓库的 `cli/` 目录内执行（从仓库根先 `cd cli`）；根级 mise tasks 已内置 `cd cli`。下文 CLI 源码、脚本与资源的相对路径均以 `cli/` 为基准；`validation/` 始终位于仓库根。

```bash
rtk proxy mise which python
rtk proxy mise which uv
rtk proxy mise exec -- python --version
rtk proxy mise exec -- uv --version
```

See the [contributing guide](cli/CONTRIBUTING.md#changes-and-verification) for source installation, tests, Ruff and export commands; agents wrap them with RTK.
Use `--frozen` for normal syncs; dependency changes require a stated reason and a lockfile update, and `--offline` only when the cache is complete.
Pure docs do not require an installed environment or the full test suite; behavior changes run the relevant regressions first, and integration checks scale with the impact.

Studio 检查在仓库的 `studio/` 目录内执行；以下环境变量仅作用于对应命令：
`GOTOOLCHAIN` 固定 Go 版本，`GOPROXY` 指定模块代理，`CGO_ENABLED=0` 禁用 CGO。
Studio 当前是 MyGo 原生桌面应用，无 web 前端与前端构建步骤；构建与验收边界说明见 [Studio README](studio/README.md)。

```bash
rtk proxy mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go vet ./...
rtk proxy mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
```

### Studio UI 与模拟行为一致性

修改 Studio 页面或模拟流程前，阅读 [UI 调试预览与测试边界](docs/subsystems/studio-panel.md#ui-调试预览)；具体运行机制和覆盖场景以该参考为准，不在此维护测试数量或验收进度。

- 正式与模拟模式共用页面构建器、主题、控件和按钮入口，不另写一套模拟页面。模拟模式在表单校验后、真实 IO 前通过 `previewIntent` 分流；正常页面按钮应能连续完成流程，场景选择器只用于设置起点。
- 模拟操作只修改本窗口内存，不创建 `core.Service`、执行 CLI、启动模型、下载文件或持久化用户设置与布局。新增操作必须接入模拟分派并覆盖隔离测试；未支持的操作明确报错，不能落入真实执行或伪装成功。
- 修改任务编排、队列、取消、重试或报告归属时，同轮同步模拟实现和 `preview_parity*_test.go` 的共享场景。两条路径分别对照明确预期，再互相比对阶段、可用控件、错误、队列状态和报告范围；不能只要求双方输出相等，也不能从某一实现的输出生成预期。
- 行为测试应能捕获实际分歧。新增一致性检查时，用已复现的回归或隔离的故意错误证明它会失败；不得改写原始证据或保留破坏性修改来制造测试结果。
- 一致性测试的生产侧调用实际编排代码，在服务边界注入受控响应；原生 E2E 驱动模拟窗口。分别报告页面测试、编排行为对照、原生窗口和真实 CLI／模型验证，不能将其中一层的通过扩大为全部一致。未覆盖的行为与平台须明确列出。
- 预览实现和 fixture 仅进入 `ui_debug && !release` 构建。修改调试入口或打包路径时，验证普通构建与 `ui_debug,release` 均拒绝调试入口，正式产物不携带预览实现与 fixture。

涉及上述行为的改动，在 `studio/` 运行带标签的页面回归（包含一致性测试）：

```bash
rtk proxy mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test -tags ui_debug . ./internal/pages -count=1
```

影响原生控件交互或模拟流程时，还应在已登录的 macOS 桌面和完整 Xcode 环境下，从仓库根运行 `rtk proxy mise run test:studio-ui`。条件不足时报告未验证范围，不用内存测试替代原生验收。入口与产物说明见 [原生 UI E2E](studio/tests/ui/README.md)；纯文档改动仍按文档检查处理。

## Documentation and contracts

- The README is the product entry point. Operational steps go into the installation/usage guides; topic rules live in `cli/docs/contracts/`. ADRs record long-term rationale only, plans keep only unfinished work, and current progress is centralized in `cli/docs/backlog.md`.
- 跨包子系统的当前职责、运行路径、状态、约束与验证命令记录在 [docs/subsystems/](docs/subsystems/README.md)；变更改变已记录的职责、接口、状态转换、运行路径、兼容规则或失败行为时，在同一 PR 更新所属参考，事实只留一处并互相链接。页面、目录或概念的改名与合并须在同一 PR 内全局检索（`grep -rn 旧名`）清扫 README、docs、契约与原型中的残留引用，不能只定点更新已知位置。
- Delete outdated handovers, stage logs and superseded plans, first folding valid rules into the current contracts and remaining work into the backlog; do not build a large archive. Platform claims must match actual coverage.
- Changes that break old-evidence reading, field semantics or measurement scope require an ADR, a contract and an implementation plan, verifying software and affected native capabilities separately. Compatible optional behavior only updates the existing contract and adds regressions; relaxing stops or identity/hash/asset verification is never treated as optional behavior.
- Evidence interpretation always cites the [lineage rules](cli/docs/data-contract.md#证据血缘与比较结论) instead of copying disclaimer paragraphs. Release checks do not require completing every historical performance experiment.
- Breaking changes to fields, enums, units or required conditions upgrade the contract and update the legal/illegal samples plus the old-evidence reading policy. The support set follows ADR 038; old formats are handled by the original project — InferYard neither converts nor accepts migrated runs.
- Regenerate schemas with `scripts/export_schemas.py` after changing their source definitions; never hand-edit the JSON. After changing the metric/method generation sources or the bound source code, run `scripts/export_catalogue.py`; existing IDs are never reassigned.
- JSON Schema proves structure only; cross-field/cross-record, state-transition and provenance-hash checks are enforced by parsers and the runtime.
- Missing measurements are `null` with a reason, never 0; reject non-finite numbers, duplicate JSON keys and booleans posing as integers. Respect field units for time and memory and make display conversions explicit.
- Frozen plans conserve the five terminal states; scoring failures do not shrink the denominator; sustained workloads follow the frozen window and draining. A wrong model answer is not a CLI failure.
- stdout is JSON; `device-check` prints a summary by default and agents use `--json`; diagnostics go to stderr. Exit codes: 0 complete, 2 input/preflight blocked, 3 incomplete, 4 tool/evidence error, 130 cancelled; verification-specific semantics are in the CLI contract.

## Execution and evidence

- A benchmark covers both `zh-core` and `zh-svg-pelican` by default; a single bundle runs only when the user explicitly declares a fixed bundle. A single-bundle default in a tool or example is not a user's fixed-bundle declaration. Follow the [default benchmark scope](cli/docs/usage.md#默认测评范围): freeze and run each scope and produce a new report containing both; when complete core results already exist, only add the missing pelican run and never overwrite the originals.
- Use fixtures and simulated services by default. Real model requests, long-running workloads, conversions or builds must be in task scope, with device, budget and stop conditions checked first.
- The CLI never implicitly downloads, starts, stops or restarts services; CLI workflows keep the operator-started model service, and binding re-verifies the model, engine, templates, arguments, PID and start time. Studio may start and manage the model service as the operator's agent: it owns the processes it starts, stops them on Studio shutdown, never restarts them automatically, and binding still goes through the CLI's identity checks. Test configurations are never used on real devices.
- `probe` (alias `check`) sends a normal/streaming request and creates diagnostic evidence; `verify` checks offline. Formal runs follow the frozen order with no implicit retries and no sample-size changes after seeing scores.
- Preserve local endpoints, host mutual exclusion, dirty recovery, cancellation and draining. Never delete lock files, bypass identity checks or relax thresholds. Credentials are referenced only via `endpoint.api_key_env`, redacted in persisted output, and never written to plain-text configs, arguments, logs or Git.
- Never overwrite original runs, manifests, model outputs, historical reports or source snapshots; reruns, rescoring, rebuilds and comparisons write new directories and keep their provenance.
- `validation/` holds local originals only — not in Git, the distribution package or default test/build dependencies; the source project's old history stays in the original repository and is not imported here. When test samples are needed, extract minimal public fixtures without touching original bytes. Bundle review materials and the current root templates remain valid dependencies and must not be deleted with historical cleanup; historical templates were removed per ADR 038.
- Comparisons bind device, engine, frozen configuration, bundle, measurement source and provenance; source changes do not inherit old qualification, and no generic forced-comparability switch is added. Total overhead includes sampling, validation, disk writes, flush/fsync and sealing.
- Simulations, diagnostics, smoke runs, subsets and short tests are explicitly labeled and never extrapolated to untested platforms. Hardware stops keep their reason, counts and takeover conditions without repeated retries; software gaps are not attributed to hardware.
- Manual review binds the case, answer and rule hashes; changes re-review the affected cases. Automated tests and `--diagnostic` do not replace manual review, and a separate operator's sign-off is not an acceptance gate.
- Model weights, engines, real device configurations, environments, caches and credentials are never committed; for new material, check the ignore rules, size and sensitive content.

## Delivery

PR merges always use the repository owner's own account, via the GitHub web Merge button or the local `gh` CLI — never the Lody GitHub App merge entry. After merging, check the commit authors with `git log`; bot-authored commits are not allowed.
Never push, merge or close a PR without the user's explicit approval. Branch first for any deliverable work — never commit it directly on `main` — and keep commits local after the checks pass; pushing, PR creation, merge and close all wait for the user's go-ahead. A user-approved action (for example creating one PR) covers only that action, never an implicit merge or direct push to `main`.
Any role that changes files under `src/inferyard/data/community/` also owns regenerating `resources.json` this round: use `scripts/export_community.py` — call its `generated(None)` with no fixed archives and write the result back into the bundle path, or pass `--archives` when archives exist — and re-run `scripts/export_community.py --check` afterwards. Files in that directory are not treated as pure docs.
Behavior fixes cover the real failure path; contract changes cover rejection paths and old-data support boundaries. Pure-doc changes check local links, anchors, commands and the diff; HTML changes check actual offline rendering.
TCP simulations need local socket permissions; report sandbox blocking, platform skips and implementation failures separately. Do not batch-run `scripts/verify_*` that may start services or send requests.
Report changes, the checks actually run and the unverified scope briefly. Create a local Git commit after the relevant checks pass; never push without the user's explicit approval.

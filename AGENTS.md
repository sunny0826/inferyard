# AGENTS.md

Applies to this repository and its subdirectories. Communicate concisely in Chinese by default and keep code identifiers as-is. README, AGENTS.md, CHANGELOG.md, CONTRIBUTING.md, MACOS.md, WINDOWS.md, Git commits and PRs are written in English; README also ships a Chinese version at [README.zh-CN.md](README.zh-CN.md). All other docs stay Chinese for now.

## Getting started

1. Inspect the working-tree diff and preserve existing changes. Read the [README](README.md) first to confirm the product and platform scope.
2. Read the [CLI reference](docs/cli-surface.md), [architecture](docs/architecture.md), [data contract](docs/data-contract.md), [experiment design](docs/experiments/design.md) and [metrics](docs/experiments/metrics.md) as the task requires. The full entry point list is in the [documentation map](docs/README.md).
3. For platform work read [MACOS.md](MACOS.md) / [WINDOWS.md](WINDOWS.md); for configuration and bundle work read the README of the relevant directory. Read [scripts/README.md](scripts/README.md) before running any script and check its arguments and side effects.
4. Distinguish implementation, simulated verification and real-device evidence. When docs and source disagree, point out the difference and verify it; never silently adopt the looser reading.

The product is a Python CLI plus a self-contained offline HTML report. The package name is `inferyard`, the entry points are `inferyard` / `python -m inferyard`; the only active contract is `schema_version = 3`.

## Source boundaries

| Directory (under `src/inferyard/`) | Responsibility |
| --- | --- |
| `cli/`, `application/` | Arguments, request construction, output, error mapping; shared types and lazy dispatch |
| `contracts/`, `config/` | Structural and semantic validation; configuration, bundles, frozen plans and declaration binding |
| `runtime/`, `adapters/` | Execution, cancellation, draining and safety controls; engine transport and parsing |
| `evidence/` | Journals, sealing, ledgers, provenance and current-format rejection |
| `analysis/`, `reporting/` | Scoring, metrics, comparison, rescoring, reports and exports |
| `platforms/`, `extensions/` | Device and process sampling; the standalone extension protocol |
| `data/`, `templates/` | Catalogue data and report templates shipped with the package |

Business modules do not depend on the CLI; application request/result types live only in `application/types.py`. Help, version and schema queries must not load the live backend; installed resources are read from the package, not from the source checkout.
Modify active source code, not the historical snapshots in the local `validation/` directory. Follow the Ruff configuration (Python 3.14, 100 columns); keep code files under ~500 lines and split new responsibilities into separate modules.
Before parallel collaboration, assign file ownership first. Only modify authorized files in your own workspace and do not revert other people's changes; report the files and the reason before crossing a boundary.

## Environment and checks

Versions are pinned by [mise.toml](mise.toml); dependencies by `pyproject.toml` / `uv.lock`. Confirm the actual mise paths and versions before developing; do not substitute system Python or global pip. RTK only wraps agent commands; dependencies are managed by uv. Source-development docs use `mise exec -- uv run --frozen`; installed users run `inferyard` directly.

```bash
rtk proxy mise which python
rtk proxy mise which uv
rtk proxy mise exec -- python --version
rtk proxy mise exec -- uv --version
```

See the [contributing guide](CONTRIBUTING.md#changes-and-verification) for source installation, tests, Ruff and export commands; agents wrap them with RTK.
Use `--frozen` for normal syncs; dependency changes require a stated reason and a lockfile update, and `--offline` only when the cache is complete.
Pure docs do not require an installed environment or the full test suite; behavior changes run the relevant regressions first, and integration checks scale with the impact.

## Documentation and contracts

- The README is the product entry point. Operational steps go into the installation/usage guides; topic rules live in `docs/contracts/`. ADRs record long-term rationale only, plans keep only unfinished work, and current progress is centralized in `docs/backlog.md`.
- Delete outdated handovers, stage logs and superseded plans, first folding valid rules into the current contracts and remaining work into the backlog; do not build a large archive. Platform claims must match actual coverage.
- Changes that break old-evidence reading, field semantics or measurement scope require an ADR, a contract and an implementation plan, verifying software and affected native capabilities separately. Compatible optional behavior only updates the existing contract and adds regressions; relaxing stops or identity/hash/asset verification is never treated as optional behavior.
- Evidence interpretation always cites the [lineage rules](docs/data-contract.md#证据血缘与比较结论) instead of copying disclaimer paragraphs. Release checks do not require completing every historical performance experiment.
- Breaking changes to fields, enums, units or required conditions upgrade the contract and update the legal/illegal samples plus the old-evidence reading policy. The support set follows ADR 038; old formats are handled by the original project — InferYard neither converts nor accepts migrated runs.
- Regenerate schemas with `scripts/export_schemas.py` after changing their source definitions; never hand-edit the JSON. After changing the metric/method generation sources or the bound source code, run `scripts/export_catalogue.py`; existing IDs are never reassigned.
- JSON Schema proves structure only; cross-field/cross-record, state-transition and provenance-hash checks are enforced by parsers and the runtime.
- Missing measurements are `null` with a reason, never 0; reject non-finite numbers, duplicate JSON keys and booleans posing as integers. Respect field units for time and memory and make display conversions explicit.
- Frozen plans conserve the five terminal states; scoring failures do not shrink the denominator; sustained workloads follow the frozen window and draining. A wrong model answer is not a CLI failure.
- stdout is JSON; `device-check` prints a summary by default and agents use `--json`; diagnostics go to stderr. Exit codes: 0 complete, 2 input/preflight blocked, 3 incomplete, 4 tool/evidence error, 130 cancelled; verification-specific semantics are in the CLI contract.

## Execution and evidence

- A benchmark covers both `zh-core` and `zh-svg-pelican` by default; a single bundle runs only when the user explicitly declares a fixed bundle. A single-bundle default in a tool or example is not a user's fixed-bundle declaration. Follow the [default benchmark scope](docs/usage.md#默认测评范围): freeze and run each scope and produce a new report containing both; when complete core results already exist, only add the missing pelican run and never overwrite the originals.
- Use fixtures and simulated services by default. Real model requests, long-running workloads, conversions or builds must be in task scope, with device, budget and stop conditions checked first.
- Services are started externally by the operator. The CLI never implicitly downloads, starts, stops or restarts services; re-verify the model, engine, templates, arguments, PID and start time. Test configurations are never used on real devices.
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

# InferYard CLI

English | [中文](README.zh-CN.md)

`inferyard` is the command-line execution subproject of
[InferYard Studio](../README.md). Studio provides the desktop interface and
manages model services; it invokes `inferyard` to run frozen task sets against
local models, record answer, latency, and resource evidence, and project the
`studio.json` report displayed in Studio. Quality, latency, and memory are
reported separately; when comparison preconditions are not met, the CLI explains
why instead of producing a single composite score.

For normal desktop benchmarking, start with [InferYard Studio](../README.md) and
the [Studio guide](../studio/README.md). Studio's **准备环境** (Prepare
environment) step installs and manages the matching `inferyard` CLI
automatically. Use `inferyard` directly only when you want to drive runs from a
terminal or develop on the CLI subproject.

## Direct CLI setup

If you have already prepared the environment in Studio, `inferyard` is installed
in the `uv` user tool directory. To install or run the CLI directly with `uv`:

```bash
uv tool install --managed-python --python 3.15.2 inferyard
inferyard --help
```

Or run it with `uvx` without a persistent install:

```bash
uvx --isolated --managed-python --python 3.15.2 inferyard --help
```

For source development from this monorepo, run `mise exec -- uv sync --frozen`
in `cli/`. Preparing Python and dependencies on the first run requires network
access; models and engines are downloaded through Studio or provided by the
operator. The [installation guide](docs/installation.md) covers `uv` setup,
Release asset verification, per-platform asset preparation, service binding, and
offline use.

## Command-line workflow

A standard benchmark covers `zh-core` and `zh-svg-pelican` by default; a single
bundle runs only when you explicitly pin it (see the
[default benchmark scope](docs/usage.md#%E9%BB%98%E8%AE%A4%E6%B5%8B%E8%AF%84%E8%8C%83%E5%9B%B4)).
The example below pins `zh-smoke` for a quick check.

1. Initialize an operations directory (`bench-ops/`) and run a preflight device check:

```bash
inferyard init --out bench-ops --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-ops/preflight/device-1 --json
```

2. Prepare a candidate per the
   [installation guide](docs/installation.md#3-%E5%87%86%E5%A4%87%E5%80%99%E9%80%89),
   start the declared model service yourself in a separate terminal, and bind its
   live PID and endpoint. Unlike Studio, the CLI never starts, stops, or restarts
   model services:

```bash
inferyard config bind --candidate bench-ops/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-ops/bound/service-1
```

3. Run the benchmark and generate the `studio.json` report (`RUN_ID` comes from
   the `run` output):

```bash
inferyard run --config bench-ops/bound/service-1/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
```

`run` includes normal and streaming probes; the standalone `probe` command is
for optional troubleshooting. Host state is created automatically on the first
live run without an explicit initialization step.

## Evidence and workspace layout

- **Two-directory separation:** Preparation checks, operator scripts, and logs
  live in the operations directory (`bench-ops/`), which you can delete once all
  runs are sealed. The benchmark workspace (`bench-work/`) holds only runs,
  batches, and reports, created automatically by `run` and `report`. For handoff,
  copy the sealed runs and the report directory into a separate delivery
  directory; see the [usage guide](docs/usage.md#工作区与交付目录组织).
- **Formal output counts:** The source development version stores a complete
  ordinary single run in eight evidence files and its report as a single
  `studio.json`. Existing valid schema-3 snapshot runs remain readable; see the
  [evidence layout contract](docs/contracts/run-evidence-layout.md).
- **Evidence authority:** `studio.json` is a rebuildable projection for
  [InferYard Studio](../studio/README.md), not a verifiable artifact. `verify`
  checks the raw runs, which remain the evidence authority after the service is
  shut down.

## Platforms and capabilities

| Platform | Implementation scope | Entry |
| --- | --- | --- |
| Linux x64 | Single and batch runs, CPU/memory sampling; manual config preparation | [Installation](docs/installation.md#linux-x64) |
| Windows x64 | Prism/KVMem/NInfer serial text runs, native identity and memory sampling | [Windows](WINDOWS.md) |
| macOS arm64 | Single and batch runs, CPU/Metal verification, CPU/RSS/paging sampling | [macOS](MACOS.md) |

- **Verification status:** [Platform status](docs/platforms.md) separates what is
  implemented, historical native coverage, and what remains unverified for the
  current candidate. A package that installs does not prove that every engine and
  sensor passed native verification. The maintainer's
  [candidate build and installation checks](CONTRIBUTING.md#local-candidate-build-and-installation-checks)
  bind the same release bytes: at least one platform completes the real
  installation check, and the remaining platforms are explicitly marked as not
  verified.
- **Task sets and non-goals:** Bundles cover instruction following, extraction,
  grounded Q&A, math/logic, classification, and structured output; the SVG task
  presents the generated result without scoring it, and `zh-context-speed`
  measures long-context prefill/decode speed. Scoring uses frozen rules and never
  calls a model judge. Community accounts, auto-submission, multi-host
  scheduling, and multimodal executors are out of scope.
- **Memory diagnostic:** On macOS and Windows, `inferyard memory-check` lists
  process memory and classification reasons for user review (see the
  [memory diagnostic contract](docs/contracts/memory-check-contract.md)). Windows
  separates working set from private commit and keeps executable groups
  conservative. It never closes applications or promises how much memory an exit
  will release.

## Read more

[InferYard Studio](../README.md) · [Studio guide](../studio/README.md) ·
[Usage guide](docs/usage.md) · [CLI reference](docs/cli-surface.md) ·
[Reports](docs/reports.md) · [Documentation map](docs/README.md) ·
[Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

Run CLI development commands from `cli/` in this monorepo; root `mise` tasks
already enter that directory.

This repository starts a fresh Git history from the current InferYard source;
original runs, model assets, and the source project's history are not
distributed. Redacted public evidence can be produced with `public package`;
conclusion scope follows the
[evidence lineage rules](docs/data-contract.md#%E8%AF%81%E6%8D%AE%E8%A1%80%E7%BC%98%E4%B8%8E%E6%AF%94%E8%BE%83%E7%BB%93%E8%AE%BA).
Licensed under [MIT](LICENSE).

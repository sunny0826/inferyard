# InferYard

Run frozen task sets against local models, keep the answer, latency and resource
evidence, and generate a self-contained HTML report that can be read offline.
Quality, latency and memory are reported separately; when comparison
preconditions are not met, the tool explains why instead of producing a single
composite score.

**v0.0.1 is published on [PyPI](https://pypi.org/project/inferyard/0.0.1/) and runs with uv / uvx.**
The project is licensed under [MIT](LICENSE).
The product is a Python CLI plus a self-contained offline HTML report; the
package and the command are both named `inferyard`.

## Installation

Install the pinned version from PyPI:

```bash
uv tool install --managed-python --python 3.14.7 inferyard==0.0.1
inferyard --help
```

Or run it directly with `uvx`, without a persistent install:

```bash
uvx --isolated --managed-python --python 3.14.7 inferyard==0.0.1 --help
```

No source checkout or mise is needed. Preparing Python and dependencies on the
first run requires network access; models and engines are provided by the
operator. The [installation guide](docs/installation.md) covers uv installs,
Release asset installs, per-platform asset preparation, service binding and
offline use. The maintainer's
[candidate build and installation checks](CONTRIBUTING.md#%E6%9C%AC%E5%9C%B0%E5%80%99%E9%80%89%E6%9E%84%E5%BB%BA%E4%B8%8E%E5%AE%89%E8%A3%85%E6%A3%80%E6%9F%A5)
bind the same release bytes: at least one platform completes the real
installation check, and the remaining platforms are explicitly marked as not
verified.

## Shortest workflow

A full benchmark covers `zh-core` and `zh-svg-pelican` by default; a single
bundle only runs when you explicitly pin it, see the
[default benchmark scope](docs/usage.md#%E9%BB%98%E8%AE%A4%E6%B5%8B%E8%AF%84%E8%8C%83%E5%9B%B4).
The example below uses the pinned `zh-smoke` bundle for a quick check.

```bash
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-work/preflight --json
```

Prepare a candidate per the
[installation guide](docs/installation.md#3-%E5%87%86%E5%A4%87%E5%80%99%E9%80%89),
start the declared model service yourself in a separate terminal — the CLI
never starts, stops or restarts services — then bind the actual PID and
endpoint:

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
inferyard verify --path bench-work/report
```

`RUN_ID` comes from the run output. Open `bench-work/report/report.html` in a
browser; it stays readable and verifiable after the service is shut down.
The CLI connects to a local service started by the operator. `run` includes
the current normal/streaming probes; a standalone `probe` is optional
troubleshooting. Host state is created automatically on the first live run —
no explicit initialization step is required.

## Platforms and capabilities

| Platform | Implementation scope | Entry |
| --- | --- | --- |
| Linux x64 | Single and batch runs, CPU/memory sampling; manual config preparation | [Installation](docs/installation.md#linux-x64) |
| Windows x64 | Prism/KVMem/NInfer serial text runs, native identity and memory sampling | [Windows](WINDOWS.md) |
| macOS arm64 | Single and batch runs, CPU/Metal verification, CPU/RSS/paging sampling | [macOS](MACOS.md) |

[Platform status](docs/platforms.md) lists what is implemented, historical
native coverage and what remains unverified for the current candidate.
A package that installs does not prove every engine and sensor passed native
verification.

Bundles cover instruction following, extraction, grounded Q&A, math/logic,
classification and structured output; the SVG task only presents the generated
result. Scoring uses frozen rules and never calls a model judge. Community
accounts, auto-submission, multi-host scheduling and multimodal executors are
out of scope.

## Read more

[Usage guide](docs/usage.md) · [CLI reference](docs/cli-surface.md) ·
[Reports](docs/reports.md) · [Documentation map](docs/README.md) ·
[Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

This repository starts a fresh Git history from the current InferYard source;
original runs, model assets and the source project's history are not
distributed. Redacted public evidence can be produced with `public package`;
conclusion scope follows the
[evidence lineage rules](docs/data-contract.md#%E8%AF%81%E6%8D%AE%E8%A1%80%E7%BC%98%E4%B8%8E%E6%AF%94%E8%BE%83%E7%BB%93%E8%AE%BA).

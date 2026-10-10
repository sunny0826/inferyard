# macOS Guide

The target is Apple Silicon (arm64). Native single and batch execution,
CPU/Metal identity verification and resource sampling are supported.
Historical model coverage and the current candidate's unverified items are
tracked in [platform status](docs/platforms.md).

## Candidate preparation and external services

Install following the [installation guide](docs/installation.md), then export
a workspace and detect an existing model:

```bash
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model /path/to/model.gguf --out bench-work/preflight --json
inferyard config create --preflight bench-work/preflight/device-preflight.json --engine /path/to/llama-server --bundle bench-work/bundles/zh-smoke.json --results bench-work/results --out bench-work/candidate
```

Candidate generation currently requires Prism `prism-b10743-adfffbe`, with the
engine and its dynamic libraries in the same directory; nothing is downloaded
or built automatically.
It runs one bounded `--version` pass, re-checks capacity and AC, and reads
the GGUF template and asset hashes.
Metal requires a bound `libggml-metal.dylib`; the actual load is verified by
the run preflight.

Start the service in a separate terminal using `engine.binary_path` and the
full `engine.startup_args` from `candidate.toml`.
Do not reuse historical PIDs; bind with the actual PID and endpoint:

```bash
inferyard config bind --candidate bench-work/candidate/candidate.toml --pid 1234 --endpoint http://127.0.0.1:48857 --out bench-work/bound
inferyard run --config bench-work/bound/config.toml
inferyard report --runs bench-work/results/RUN_ID --out bench-work/report
inferyard verify --path bench-work/report
```

The tool does not change power policies or manage model services; the
operator shuts the service down after the run.
See the [usage guide](docs/usage.md) for batch plans, cancellation and dirty
recovery.

## Native sampling boundaries

Process identity uses start time and file identity; `lsof` verifies the
binary, listeners and the Metal dynamic library.
Model binding comes from the files pointed to by the startup arguments; the
model mapping remains unobserved — Metal capability and library loading do
not prove the model is resident on the GPU.

`macos-resource.v1` records CPU, RSS, host available memory and paging; the
read-only AppleSMC temperature records the actual source and the reason for
any missing value.
Paging belongs to the host and is not attributed to the model directly.
Unified memory must not be added to RSS; discrete VRAM, frequency, power and
energy are null when not sampled.
Benchmark power policy comes from IOKit and swap counters from `host_statistics64`.
Failures remain missing measurements; there is no `pmset`/`vm_stat` fallback.
`device-check` keeps its existing readers. Linux governor/EPP values are never fabricated.
Missing policies or sensors block or are disclosed per protocol.

The first live entry creates a clean host state automatically when it
acquires the host lock; no explicit initialization command is needed. The
lock is `/var/tmp/inferyard-host.lock`, the state is
`inferyard-host.state.json` in the same directory.
Old-tool files are neither read nor locked; isolated-root process tests do
not count as acceptance of a real host deployment.
When draining cannot be confirmed after a cancellation, the dirty state is
kept; do not delete the lock or state to bypass recovery.

## Engine diagnostics and development

Same-host engine diagnostics follow the [engine-fit guide](docs/engine-fit-common.md);
MLX-LM/LM Studio services are still started by the operator.
The MLX controlled-service script path can be queried with
`inferyard engine-fit engines`; do not guess paths from the source directory.
See the [contributing guide](CONTRIBUTING.md) for the development environment
and source commands, and the [macOS contract](docs/contracts/macos-contract.md)
for implementation details.

# InferYard CLI

English | [中文](README.zh-CN.md)

`inferyard` is the command-line execution subproject of
[InferYard Studio](../README.md). Studio provides the desktop interface and
manages model services; it invokes `inferyard` to run frozen task sets against
local models, record answer, latency, and resource evidence, and project the
`studio.json` report displayed in Studio. Quality, latency, and memory are
reported separately; when comparison preconditions are not met, the CLI explains
why instead of producing a single composite score.

## Install

Requires Python 3.15 (managed automatically by `uv`) and `uv >= 0.13.6`:

```bash
uv tool install --managed-python --python 3.15 inferyard
inferyard --versions
inferyard --help
```

To upgrade an existing installation:

```bash
uv tool upgrade inferyard
```

## Quick command reference

| Command | Purpose |
| --- | --- |
| `inferyard init --out DIR` | Export a starter workspace and task bundles (default catalog bundles, or repeatable `--bundle <name\|path>` for registered or local JSON bundles) |
| `inferyard bundles list\|verify` | List registered bundles in `benchmarks/` or verify a local bundle JSON file/directory |
| `inferyard device-check --json` | Read-only hardware preflight and hardware-aware starter recommendation (PolyStrata vs. `llama.cpp`) |
| `inferyard engine prepare-polystrata` | Automated local PolyStrata setup (preflight, isolated Python 3.10–3.13 `.venv`, runtime, model, `installation.json`) |
| `inferyard engine prepare` / `engine check` | Download, verify, and unpack a pinned engine archive, or verify a local engine / `installation.json` read-only |
| `inferyard engine detect` / `engine init-installation` | Detect local engine binaries/venvs and generate a validated `installation.json` |
| `inferyard model acquire` / `model resolve` | Resolve or download pinned Hugging Face / ModelScope models (single/multi-shard GGUF or directory models, with resume & proxy) |
| `inferyard assets scan` | Read-only scan of local model and engine directories |
| `inferyard serve prepare` | Generate a loopback `serve-plan.json` (`serve_plan` schema) to launch a local OpenAI-compatible model service without creating a benchmark task (`--model`, `--installation \| --engine \| --runtime-receipt`, `--context-size`, `--port`, `--out`, `--json`) |
| `inferyard service discover` | Read-only discovery of running loopback inference services (`/health`, `/v1/models`) |
| `inferyard plan` | Preview (`--dry-run`) or freeze an experiment plan, including `--preset context-speed` for `zh-context-speed` |
| `inferyard probe` / `run` / `resume` | Probe a bound service, run a serial or planned benchmark, or resume unexecuted cases |
| `inferyard verify` / `report` / `speed-results` | Verify sealed runs offline, build a combined `studio.json` report, or read verified context-speed batch results |
| `inferyard compare` / `compare-engines` | Compare frozen trial evidence or multi-block engine comparison batches offline |
| `inferyard --schema <kind>` / `--versions` | Export active JSON Schemas (`schema_version = 3`) or runtime/dependency versions |

## Start from the terminal

1. Follow the [installation guide (Chinese)](docs/installation.md) to install `inferyard`, initialize the operations directory, and run the device preflight.
2. [Prepare a candidate for your platform (Chinese)](docs/installation.md#3-准备候选). Start the declared model service in another terminal, then bind its actual PID and local endpoint.
3. Run `zh-core` and `zh-svg-pelican` separately, verify each raw run, and generate one combined report. If the `zh-core` run is already complete, add only the missing `zh-svg-pelican` run.
4. Open `studio.json` in Studio. For handoff, copy the sealed runs along with the report (or produce a redacted evidence bundle with `inferyard public package`).

The desktop workflow starts on the [Studio Tasks page](../studio/README.md#pages-and-first-launch). Studio can reuse an existing CLI, automatically upgrade it after Studio updates, and manage services it starts.

## Read more

[InferYard Studio](../README.md) · [Studio guide](../studio/README.md) ·
[Installation (Chinese)](docs/installation.md) ·
[Task sets](bundles/README.md) ·
[Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

Run CLI development commands from `cli/` in this monorepo; root `mise` tasks
already enter that directory. Licensed under [MIT](LICENSE).

# InferYard

English | [中文](README.zh-CN.md)

**InferYard is a local LLM benchmarking suite built around the InferYard Studio
desktop app and its command-line execution subproject.**

It runs frozen Chinese task sets and long-context speed tests against models
running on your own machine, records per-case answers, latency, and system
resource telemetry, and keeps the raw evidence on disk alongside native
reports.

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## What InferYard does

- **Deterministic local evaluation.** Evaluates locally served models against
  frozen, human-reviewed task sets and scores quality with fixed rules rather
  than another model.
- **Separated, honest metrics.** Reports quality, request latency, throughput,
  and CPU, memory, and available temperature telemetry separately instead of
  collapsing them into a single composite score. Missing measurements stay
  `null` with a stated reason, never `0`.
- **Reproducible raw evidence.** Preserves per-case outputs, execution metadata,
  and resource samples on disk so runs can be verified offline later and
  projected into `studio.json` reports.
- **Desktop management with Studio.** [InferYard Studio](studio/README.md)
  provides a native Go/MyGo desktop interface to check device readiness,
  download or register models and engines, manage local service lifecycles,
  watch live progress curves, and inspect reports.
- **CLI subproject for execution and automation.** The [CLI](cli/README.md)
  subproject (`inferyard`) owns the benchmark runner, host locking, evidence
  sealing, offline verification, and report projection. Studio invokes it under
  the hood, and operators can also run it directly from a terminal.

## Project structure

| Component | Role | Language / Stack |
| --- | --- | --- |
| [InferYard Studio](studio/README.md) (`studio/`) | Native desktop app for configuring runs, managing downloads and local engines, monitoring execution, and viewing reports | Go, MyGo, onevra |
| [InferYard CLI](cli/README.md) (`cli/`) | Execution and evidence subproject invoked by Studio (or directly via terminal) for preflight checks, frozen runs, verification, and `studio.json` reports | Python |

## Available task sets

| Task set | What it contains |
| --- | --- |
| `zh-core` | 120 Chinese tasks: instruction following, extraction, grounded Q&A, math and logic, classification, and structured output (20 each) |
| `zh-svg-pelican` | 1 SVG drawing task. The generated result is shown without a quality score |
| `zh-smoke` | 40 tasks (instruction following and extraction) for a quick check |
| `zh-context-speed` | 3 long prompts (64K / 128K / 256K) that measure prefill and decode speed without a quality score |

A standard benchmark runs `zh-core` and `zh-svg-pelican` together. Every task
set is reviewed by a person before release; see [task sets](cli/bundles/README.md).

## Supported platforms

| Platform | Studio UI backend | Benchmark & telemetry scope |
| --- | --- | --- |
| macOS (Apple Silicon, 12+) | Metal | Single and batch runs, CPU/Metal verification, CPU/RSS/paging and AppleSMC temperature sampling |
| Windows x64 (10/11) | Direct3D 11 (WARP fallback) | Prism/KVMem/NInfer serial text runs, native identity and memory sampling |
| Linux x64 | GTK 3 and OpenGL (CPU fallback) | Single and batch runs, CPU/memory sampling |

Supported engines and native verification status differ by platform. Check
[platform status](cli/docs/platforms.md), [macOS](cli/MACOS.md), and
[Windows](cli/WINDOWS.md) before relying on a result.

## Design principles and boundaries

- **Local first.** Models, runs, and reports stay on your machine. InferYard
  has no user accounts and never submits results remotely.
- **Controlled service ownership.** Studio starts and stops only the local
  model services it launches (without auto-restarting them) and can bind a
  local service you started yourself after PID and start-time checks.
- **Explicit non-goals.** InferYard does not use a model as a judge, produce a
  single cross-dimension ranking, schedule across multiple machines, or test
  image/audio models.

## Documentation and downloads

Prebuilt Studio packages (`.dmg` for macOS, `.exe` installer for Windows, and
`.tar.gz` for Linux; currently Chinese UI, unsigned) are published on
[GitHub Releases](https://github.com/sunny0826/inferyard/releases/latest).

| Topic | Document |
| --- | --- |
| Studio pages, usage, and builds | [Studio guide](studio/README.md) |
| Report structure and metrics | [Reports](cli/docs/reports.md) |
| CLI subproject and terminal workflow | [CLI guide](cli/README.md) |
| Local monorepo development | [Development commands](tools/README.md) |
| Full documentation index | [Documentation map](cli/docs/README.md) |

## License

[MIT](LICENSE)

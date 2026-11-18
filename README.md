# InferYard

English | [中文](README.zh-CN.md)

**InferYard Studio is a desktop app that benchmarks local models on your machine
and keeps the raw evidence.**

Pick a model and an engine, click **开始测评** (Start benchmark), and Studio runs
a fixed set of tasks against the model. It records each answer, its latency, and
CPU and memory usage, then displays the report in the same window.

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## What you can do in Studio

- **Prepare in one step.** Set up the CLI, workspace, results folder, and model
  folder from the task page, then choose evaluation sets and a model/engine pair.
- **Download or register models and engines.** Create, cancel, and retry
  downloads on the Downloads page, or register an existing local installation
  declaration on the Settings page.
- **Let Studio run the engine.** Studio starts and verifies the model service it
  needs and stops it when Studio shuts down. It never restarts a service on its
  own, and can also bind a local service you started yourself.
- **Watch runs live.** Follow per-case progress and CPU, memory, and available
  temperature curves while the benchmark runs.
- **Read reports.** Review a summary and open the report directly when a
  benchmark finishes, or browse historical tasks and model-grouped reports on
  the Reports page. A report with multiple runs lets you switch between them.
- **Check your device.** View and refresh a hardware snapshot, with a reason
  shown for every value that could not be measured.

## Why InferYard

- **Local first.** Models run on your own machine, and runs and reports stay
  on your disk. InferYard has no accounts and does not submit results anywhere.
- **Honest numbers.** Quality, latency, and memory are reported separately
  without a single composite score. A missing measurement is shown as missing
  with a reason, never as 0.
- **Repeatable.** Task sets are frozen and scored by fixed rules, not by another
  model. Each run keeps its raw evidence so you can verify it offline later.

## Quick start

1. Download Studio for your system from the
   [latest release](https://github.com/sunny0826/inferyard/releases/latest):
   mount the `.dmg` on macOS, run the `.exe` installer on Windows, or unpack
   the `.tar.gz` archive on Linux (earlier releases ship as archives). On macOS,
   open the app with `open "InferYard Studio.app"`.
2. On first launch, Studio opens the task page. Click **准备环境** (Prepare
   environment). Studio reuses an existing `inferyard` CLI and `uv` when
   present, or downloads a pinned standalone `uv` and installs the matching CLI
   and managed Python into the `uv` user tool directory without changing your
   shell configuration. It then creates or reuses the workspace, results folder,
   and model folder.
3. Download a model and an engine from **下载** (Downloads), or register an
   existing local installation declaration in **设置** (Settings).
4. Return to the task page, select the model, engine, and **测评题集**
   (Evaluation sets), and click **开始测评** (Start benchmark).
5. When the run finishes, click **查看报告** (View report) on the completion
   step, or open historical results at any time from **报告** (Reports).

The Studio interface is currently in Chinese. The [Studio guide](studio/README.md)
describes every page.

> Studio builds are not signed or notarized yet. Your system may warn you the
> first time you open it.

## Available task sets

| Task set | What it contains |
| --- | --- |
| `zh-core` | 120 Chinese tasks: instruction following, extraction, grounded Q&A, math and logic, classification, and structured output (20 each) |
| `zh-svg-pelican` | 1 SVG drawing task. The result is shown, not scored |
| `zh-smoke` | 40 tasks (instruction following and extraction) for a quick check |
| `zh-context-speed` | 3 long prompts (64K / 128K / 256K) that measure long-context speed without a quality score. Choose **长上下文速度** (Long-context speed) as the test type and click **开始专项测评** (Start specialist benchmark) |

A standard benchmark runs `zh-core` and `zh-svg-pelican` together. Each task set
is reviewed by a person before release; see [task sets](cli/bundles/README.md).

## Supported platforms

| Platform | Requirements |
| --- | --- |
| macOS (Apple Silicon) | macOS 12+ (Metal) |
| Windows x64 | Windows 10/11 (Direct3D 11, WARP fallback) |
| Linux x64 | GTK 3 and OpenGL (CPU rendering fallback) |

Supported engines and verification status differ by platform. Check
[platform status](cli/docs/platforms.md), [macOS](cli/MACOS.md), and
[Windows](cli/WINDOWS.md) before you rely on a result.

## What InferYard does not do

- It does not use a model as a judge.
- It does not combine quality, speed, and memory into one ranking.
- It does not upload results or offer community accounts.
- It does not schedule across several machines or test image/audio models.

## Documentation

| I want to… | Read |
| --- | --- |
| Learn every Studio page | [Studio guide](studio/README.md) |
| Understand the report | [Reports](cli/docs/reports.md) |
| Benchmark from the command line | [CLI guide](cli/README.md) |
| Develop from a source checkout | [Development commands](tools/README.md) |
| Browse everything | [Documentation map](cli/docs/README.md) |

## License

[MIT](LICENSE)

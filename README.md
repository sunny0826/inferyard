# InferYard

English | [中文](README.zh-CN.md)

**InferYard Studio is a desktop app for benchmarking the models you run on your
own machine — and keeping the evidence.**

Pick a model and an engine, click run, and Studio tests the model with a fixed set
of tasks. It records every answer, how long each one took and how much CPU and
memory were used, and shows the results as a report in the same window.

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## What you can do in Studio

- **Prepare in one step.** Set up the workspace, choose task sets and a
  model/engine pair from the task page.
- **Download models and engines.** Create, cancel and retry downloads from the
  Downloads page.
- **Let Studio run the engine.** Studio starts the model service it needs and
  stops it when Studio shuts down. It never restarts a service on its own.
- **Watch runs live.** Follow case progress and CPU/memory curves while the
  benchmark runs.
- **Read reports.** Browse past tasks and open each report without leaving the
  app. A report with several runs lets you switch between them.
- **Check your device.** See a hardware snapshot, with a reason shown for every
  value that could not be measured.

## Why InferYard

- **Local first.** Models run on your own machine, and runs and reports stay
  on your disk. InferYard has no accounts and does not submit results anywhere.
- **Honest numbers.** Quality, latency and memory are shown separately. There is
  no single "magic score". A missing measurement is shown as missing, never as 0.
- **Repeatable.** Task sets are frozen and scored by fixed rules, not by another
  model. Each run keeps its raw evidence, so you can check it again later.

## Quick start

1. Download Studio for your system from the
   [latest release](https://github.com/sunny0826/inferyard/releases/latest)
   and unpack it. On macOS, open it with `open "InferYard Studio.app"`.
2. On first launch Studio opens the task page. Click **准备环境** (Prepare
   environment). Studio installs the InferYard CLI it needs (and uv, if it is
   missing) and creates the results folder, model folder and workspace. The
   first run downloads a Python runtime and can take a few minutes.
3. Get a model and an engine from **下载** (Downloads).
4. Pick the model, engine and task sets, then click **运行 benchmark** (Run
   benchmark).
5. Open the result from **报告** (Reports).

The Studio interface is currently in Chinese. The [Studio guide](studio/README.md)
describes every page.

> Studio builds are not signed or notarized yet. Your system may warn you the
> first time you open it.

## Available task sets

| Task set | What it contains |
| --- | --- |
| `zh-core` | 120 Chinese tasks: instruction following, extraction, grounded Q&A, math and logic, classification and structured output (20 each) |
| `zh-svg-pelican` | 1 SVG drawing task. The result is shown, not scored |
| `zh-smoke` | 40 tasks (instruction following and extraction) for a quick check |
| `zh-context-speed` | 3 long prompts (64K / 128K / 256K) that measure long-context speed, without a quality score. Choose **长上下文速度** (Long-context speed) as the test type |

A standard benchmark runs `zh-core` and `zh-svg-pelican` together. Each task set
is reviewed by a person before release; see [task sets](cli/bundles/README.md).

## Supported platforms

| Platform | Requirements |
| --- | --- |
| macOS (Apple Silicon) | macOS 12+ |
| Windows x64 | Windows 10/11 |
| Linux x64 | GTK 3 and OpenGL |

Supported engines and verification status differ by platform. Check
[platform status](cli/docs/platforms.md), [macOS](cli/MACOS.md) and
[Windows](cli/WINDOWS.md) before you rely on a result.

## What InferYard does not do

- It does not use a model as a judge.
- It does not combine quality, speed and memory into one ranking.
- It does not upload results or offer community accounts.
- It does not schedule across several machines or test image/audio models.

## Documentation

| I want to… | Read |
| --- | --- |
| Learn every Studio page | [Studio guide](studio/README.md) |
| Understand the report | [Reports](cli/docs/reports.md) |
| Benchmark from the command line | [CLI guide](cli/README.md) |
| Browse everything | [Documentation map](cli/docs/README.md) |

## License

[MIT](LICENSE)

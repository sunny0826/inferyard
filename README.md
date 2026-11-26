<p align="left">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-mark-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo-mark-light.svg">
    <img src="assets/logo-mark-light.svg" alt="InferYard logo" width="64" height="64">
  </picture>
</p>

# InferYard

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/inferyard-studio)

English | [中文](README.zh-CN.md)

**Benchmark local LLMs on your own hardware—with reproducible scores, real
resource curves, and every piece of raw evidence preserved.**

Public leaderboards tell you how a model scored on someone else's cluster, not
how it runs on your desk. InferYard brings deterministic evaluation into a
native desktop app (**InferYard Studio**) powered by its **CLI** execution
subproject: test locally served models against frozen Chinese task sets and
long-context speed workloads, watch CPU, memory, and temperature telemetry in
real time, and inspect recorded answers in the raw runs and timing summaries in Studio.
Results stay on your machine. Frozen rules score the quality tasks.

## What InferYard does

- **Reproducible, rule-based evaluation.** Run frozen, human-reviewed task sets
  on your local models and grade outputs with fixed deterministic rules instead
  of another model's opinion.
- **No magic composite score.** Quality, latency, throughput, and memory/thermal
  telemetry stay separate so you can see real speed-vs-accuracy trade-offs.
  Anything that cannot be measured stays `null` with an explicit reason, never `0`.
- **Evidence you can verify later.** Every answer, timing sample, and system
  reading is saved and sealed on disk for offline verification, then projected
  into clean `studio.json` reports.
- **Native desktop workbench.** [InferYard Studio](studio/README.md) is a
  Go/MyGo native desktop app featuring:
  - **Multi-language UI (i18n):** English and Simplified Chinese (`Follow System` / `English` / `简体中文` in **Settings → Language**, switching live without restart; supports `MYGO_LOCALE` override and macOS bundle localizations).
  - **Desktop UX & self-update:** Light / Dark / Follow System themes with live visual previews, optional minimize to system tray on window close to keep background runs and downloads active, scoped HTTP(S) download proxy, and automatic GitHub Releases update checks with one-click SHA-256 verified self-update.
  - **Hardware-aware downloads & assets:** Starter recommendations prioritizing **PolyStrata** on Apple Silicon, NVIDIA CUDA, and AMD ROCm hardware (with one-click 4-stage guided setup/recovery and `llama.cpp` fallback), extensible multi-engine catalog (`PolyStrata`, `llama.cpp`, `mlx-lm`, `vLLM`, `SGLang`, `LM Studio`), Hugging Face & ModelScope mirrors, custom URL resolution (`model resolve`), multi-shard GGUF and directory models with streaming progress and resume, plus multi-root model directories and safe uninstall/disk cleanup for Studio-installed PolyStrata runtimes and downloaded models.
  - **Tasks & live monitoring:** Automatic CLI version check and automatic CLI upgrade alongside Studio updates (`uv >= 0.13.6`, surfacing an error banner only if the upgrade fails), standard bundles and **Long-Context Speed** testing (`zh-context-speed`: 64K / 128K / 256K), an engine status card with direct Web UI link, live **Resource Curves**, in-app notification inbox, OS notifications, and model-grouped reports.
- **Scriptable CLI subproject.** The [CLI](cli/README.md) subproject
  (`inferyard`) handles preflight checks, host locking, frozen execution,
  evidence sealing, and report projection—invoked automatically by Studio or
  directly from your terminal.

## Getting started

- **Desktop** — Download Studio for your platform from [Releases](https://github.com/sunny0826/inferyard/releases/latest). On macOS, open the `.dmg`, drag `InferYard Studio.app` into `Applications`, and clear the quarantine attribute before first launch (the release build is unsigned):

  ```sh
  xattr -dr com.apple.quarantine /Applications/InferYard\ Studio.app
  ```

  Open **Services**, choose **Prepare Environment**, then select a model, engine, and task sets. See the [first-launch guide](studio/README.md#pages-and-first-launch).
- **Terminal** — Follow the [installation and candidate preparation guide](cli/docs/installation.md) (Chinese) or the [CLI guide](cli/README.md). Start the service with the candidate's arguments, bind its PID and endpoint, run each bundle, and generate a combined report.

## Available task sets

| Task set | What it contains |
| --- | --- |
| `zh-core` | 120 Chinese tasks: instruction following, extraction, grounded Q&A, math and logic, classification, and structured output (20 each) |
| `zh-svg-pelican` | 1 animated SVG drawing task. Raw evidence retains the generated output and Studio renders an animated preview without a quality score |
| `zh-smoke` | 40 tasks (instruction following and extraction) for a quick check |
| `zh-context-speed` | 3 fixed prompts for 64K / 128K / 256K context settings that measure prefill and decode speed without a quality score |

A standard benchmark runs `zh-core` and `zh-svg-pelican` separately. Every task
set is reviewed by a person before release; see [task sets](cli/bundles/README.md).

## Supported platforms

| Platform | Studio UI backend | Benchmark & telemetry scope |
| --- | --- | --- |
| macOS (Apple Silicon, 12+) | Metal | Single and batch runs, PolyStrata (Metal) & `llama.cpp` / `mlx-lm` / `vLLM` / `SGLang`, CPU/RSS/paging and AppleSMC temperature sampling |
| Windows x64 (10/11) | Direct3D 11 (WARP fallback) | PolyStrata (CUDA/HIP) & `llama.cpp` serial text runs, native process identity and memory sampling |
| Linux x64 | GTK 3 and OpenGL (CPU fallback) | Single and batch runs, PolyStrata (CUDA/HIP) & `llama.cpp` / `vLLM` / `SGLang`, CPU/memory sampling |

The table describes implementation scope. Automated native desktop UI verification currently runs on macOS (Apple Silicon); native Windows and Linux window interactions are not yet covered by automated UI tests. See [CLI installation and platform setup](cli/docs/installation.md) (Chinese) for platform setup details.

## Design principles and boundaries

- **Local first.** Models, runs, and reports stay on your machine. InferYard
  has no user accounts and never submits results remotely.
- **Controlled service ownership.** Studio starts and stops only the local
  model services it launches (without auto-restarting them) and can bind a
  local service you started yourself after PID and start-time checks.
- **Explicit non-goals.** InferYard does not use a model as a judge, produce a
  single cross-dimension ranking, schedule across multiple machines, or test
  image/audio models.

## Documentation

| Topic | Link |
| --- | --- |
| Download Studio builds | [GitHub Releases](https://github.com/sunny0826/inferyard/releases/latest) |
| Studio pages, i18n, and builds | [Studio guide](studio/README.md) |
| CLI subproject and terminal workflow | [CLI guide](cli/README.md) |
| CLI installation and candidate setup (Chinese) | [Installation guide](cli/docs/installation.md) |
| Frozen task sets | [Task sets](cli/bundles/README.md) |
| Local monorepo development | [Development commands](tools/README.md) |
| Contributing | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Release history | [Changelog](cli/CHANGELOG.md) |

## Acknowledgments

Thanks to the following projects:

- [PolyStrata](https://github.com/simplexai-labs/PolyStrata)
- [llama.cpp](https://github.com/ggml-org/llama.cpp) / [PrismML-Eng/llama.cpp](https://github.com/PrismML-Eng/llama.cpp)
- [mlx-lm](https://github.com/ml-explore/mlx-lm)
- [vLLM](https://github.com/vllm-project/vllm) / [vllm-metal](https://github.com/vllm-project/vllm-metal)
- [SGLang](https://github.com/sgl-project/sglang)
- [LM Studio](https://lmstudio.ai)
- [MyGo](https://github.com/egoist/mygo) / [onevra](https://github.com/oneo-me/onevra)

## License

[MIT](LICENSE)

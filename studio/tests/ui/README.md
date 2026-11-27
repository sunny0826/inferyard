# Native preview UI e2e

Builds Studio with `ui_debug` and drives one native macOS `--debug-ui` window via XCUITest against simulated data (no model service, downloads, or user settings writes). Tests pin `MYGO_LOCALE=zh-Hans` by default in the launch environment so accessibility label matching remains deterministic regardless of the host macOS locale.

From the repository root (requires an unlocked, logged-in macOS desktop session with full Xcode):

```sh
mise run test:studio-ui       # Daily: 4 core workflows
mise run test:studio-ui:full  # Full: all 8 workflows (4 daily + 4 full-only; also run by CI)
```

Watch for the macOS automation authorization prompt on first launch and keep the desktop unlocked throughout the run. Artifacts (`run.json`, `xcodebuild.log`, `Results.xcresult`) are written to a new `studio/build/ui-e2e/run-*` directory.

Runner unit checks (no native window required, from `studio/`):

```sh
mise exec -- python -m unittest discover -s tests/ui -p 'test_*.py'
```

## Production package smoke

[release.yml](../../../.github/workflows/release.yml) mounts the packaged macOS DMG and verifies `InferYard Studio.app` (`--help` exits 0 and `--debug-ui` exits 2). For local native release-app UI checks, run `tests/ui/run_release.py` (`ReleaseUITests`) against a mounted `InferYard Studio.app` without `--debug-ui`, reading a `studio.json` produced by `TestChainBindRunReport` in an isolated `HOME`.

## Local real-machine PolyStrata UI E2E (not in CI)

To drive a real production `InferYard Studio.app` window (without `--debug-ui`) end-to-end against an existing local PolyStrata runtime and smallest installed model without downloading weights or mutating `~/.inferyard`, run from the repository root on an unlocked macOS desktop:

```sh
mise run test:studio-ui:polystrata
```

`tests/ui/run_polystrata.py` (`PolystrataRealUITests`) auto-discovers `~/.inferyard/engines/polystrata*/installation.json`, Studio `settings.json`, or `~/PolyStrata/installation.json` (override via `--installation` or `INFERYARD_POLYSTRATA_INSTALLATION`), clones local PolyStrata assets via APFS copy-on-write into an isolated temporary workspace and `HOME`, verifies **Library**, **Services** (standalone launch, live `/v1/chat/completions` probe, multi-process memory reporting, clean stop), and **Benchmarks** (**Thinking Effort** selection and `zh-svg-pelican` through `studio.json` **View Report**), and asserts that the host `~/.inferyard` directory remains untouched. This task is intentionally excluded from CI workflows; `PolystrataRealUITests` skips automatically during `test:studio-ui` and `test:studio-ui:full`.

## Real-machine multi-engine one-click upgrade & run UI E2E

To verify that all Studio-managed engine families on Apple Silicon detect upstream updates, complete one-click upgrades (`UpdateEngine`), re-verify (`engine check`), and run real inference in a production `InferYard Studio.app` window (without `--debug-ui`), run from the repository root on an unlocked macOS desktop:

```sh
mise run test:studio-ui:engines     # Full local suite: llama-cpp, mlx-lm, vllm, polystrata
mise run test:studio-ui:engines:ci  # Lightweight CI tier: llama-cpp, mlx-lm, vllm (~500 MiB total models)
```

`tests/ui/run_engines.py` (`EnginesRealUITests`) caches small baseline engines and models under `studio/build/engines-ui-cache` and stages an isolated CoW workspace and `HOME`:

| Engine Family | Staged Baseline | One-Click Upgrade Path | Smallest Verification Model | UI Execution Path |
| --- | --- | --- | --- | --- |
| `llama-cpp` | `prism-b10743-adfffbe` | Direct archive download + replaced binary cleanup | `SmolLM2-135M-Instruct-Q4_K_M.gguf` (~101 MiB) | **Services** (`/v1/chat/completions` HTTP 200) |
| `mlx-lm` | `0.31.1` (managed venv) | In-place `uv pip install --upgrade` (`EngineUpdateActionVenv`) | `mlx-community/SmolLM2-135M-Instruct-8bit` (~141 MiB) | **Services** (`/v1/chat/completions` HTTP 200) + **Benchmarks** (`zh-svg-pelican` -> `studio.json`) |
| `vllm` | `0.31.0+cpu` + `vllm-metal 0.31.0` | Paired macOS arm64 GitHub Release wheels (`EngineUpdateActionVenv`) | `HuggingFaceTB/SmolLM2-135M-Instruct` (~260 MiB) | **Services** (`/v1/chat/completions` HTTP 200) |
| `polystrata` | Local install downgraded to `v0.2.20-poly.4` | Managed 4-stage `prepare-polystrata` reusing CoW `polystrata-data` | Smallest existing local Strata Pack (`strata-*.json`) | **Services** (`/v1/chat/completions` HTTP 200) |

Non-managed catalog entries (`SGLang` manual source archive on macOS and `LM Studio` external desktop app) are verified in the **Library -> Engines** catalog view. `test:studio-ui:engines:ci` runs daily in [nightly.yml](../../../.github/workflows/nightly.yml) on `macos-15`, while full `test:studio-ui:engines` (`polystrata` included) stays local-only due to unified memory requirements. `EnginesRealUITests` skips automatically during `test:studio-ui` and `test:studio-ui:full`.

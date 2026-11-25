# Native preview UI e2e

Builds Studio with `ui_debug` and drives one native macOS `--debug-ui` window via XCUITest against simulated data (no model service, downloads, or user settings writes). Tests pin `MYGO_LOCALE=zh-Hans` by default in the launch environment so accessibility label matching remains deterministic regardless of the host macOS locale.

From the repository root (requires an unlocked, logged-in macOS desktop session with full Xcode):

```sh
mise run test:studio-ui       # Daily: 3 core workflows
mise run test:studio-ui:full  # Full: all 7 workflows (3 daily + 4 full-only; also run by CI)
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

`tests/ui/run_polystrata.py` (`PolystrataRealUITests`) clones local PolyStrata assets via APFS copy-on-write into an isolated temporary workspace and `HOME`, verifies **Library**, **Services** (standalone launch, live `/v1/chat/completions` probe, clean stop), and **Benchmarks** (`zh-svg-pelican` through `studio.json` **View Report**), and asserts that the host `~/.inferyard` directory remains untouched. This task is intentionally excluded from CI workflows; `PolystrataRealUITests` skips automatically during `test:studio-ui` and `test:studio-ui:full`.

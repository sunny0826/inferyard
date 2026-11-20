# Native preview UI e2e

Builds Studio with `ui_debug` and drives one native macOS `--debug-ui` window via XCUITest against simulated data (no model service, downloads, or user settings writes).

From the repository root (requires an unlocked, logged-in macOS desktop session with full Xcode):

```sh
mise run test:studio-ui       # Daily: 3 core workflows
mise run test:studio-ui:full  # Full: all 12 workflows (also run by CI)
```

Watch for the macOS automation authorization prompt on first launch and keep the desktop unlocked throughout the run. Artifacts (`run.json`, `xcodebuild.log`, `Results.xcresult`) are written to a new `studio/build/ui-e2e/run-*` directory.

Runner unit checks (no native window required, from `studio/`):

```sh
mise exec -- python -m unittest discover -s tests/ui -p 'test_*.py'
```

## Production package smoke

[studio-release.yml](../../../.github/workflows/studio-release.yml) mounts the packaged macOS DMG and runs `tests/ui/run_release.py` (`ReleaseUITests`) against `InferYard Studio.app` without `--debug-ui`, reading a `studio.json` produced by `TestChainBindRunReport` in an isolated `HOME`.

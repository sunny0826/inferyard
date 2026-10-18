# Native preview UI e2e

This suite builds the actual Go app with `ui_debug` and starts a fresh native macOS
window with `--debug-ui` for each test. [XCUITest](https://developer.apple.com/documentation/xcuiautomation/xcuiapplication) drives public controls and checks the
accessibility tree; it does not call preview internals, advance a test clock, or use
the scene selector to complete workflows.

From `studio/`, in a logged-in, unlocked macOS desktop session with full Xcode selected:

Before starting this suite, remind the user to watch for the macOS password or
automation authorization prompt. The user enters the password directly in that system
dialog. An automation-mode initialization timeout can occur before any test starts if
authorization is still pending; retry after the user confirms authorization.
Keep the desktop unlocked throughout the run. A lock screen can block foreground
activation after the app starts, leaving XCTest waiting at `app.launch()` with
`Running Background`; completing the authorization prompt does not unlock the desktop.

```sh
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 python tests/ui/run.py
```

The runner creates a new `build/ui-e2e/run-*` directory containing the build, log and
`Results.xcresult`. It opens and closes only the dedicated `PreviewHost.app` test app
(`local.inferyard.preview-e2e`). It does not package or publish Studio. UI operations
use simulated data: no CLI/model service, real downloads, or user settings writes.
Go/Xcode may populate their normal build caches. No output directory is overwritten.
Use `--output-root PATH` to retain the artifacts elsewhere. The command returns a
nonzero status on unsupported platforms, build errors or test failures.

Coverage: prepare → download → select model → two-bundle benchmark → live details →
report; failed startup and retry; run cancellation and a new task; failed download,
retry with a fresh name, preservation across new tasks, and scene reset. A test failure
with the app still running attaches its accessibility tree to the Xcode result, plus a
screenshot when the preview window exists. Screenshots capture only that window.
Build and launch failures remain in the log and result bundle.

This is native UI coverage with a simulated backend. The existing fast `ui.NewTester`
regressions remain under `internal/pages`; real CLI/fake-engine boundary tests remain
under [`../e2e`](../e2e/README.md). None establishes real model qualification or
Windows/Linux native UI acceptance.

The context-speed preview tests drive the existing task form through tier selection,
five simulated measurements, the six-column result, history reopening, cancellation,
startup failure, missing timing and result-read retry. Screenshots of these states are
retained in the result bundle even on success. Production CLI integration is implemented;
the user has approved the UI design. Native execution and the real CLI/model workflow
remain separate verification steps. Result and history tables show the model and engine
recorded for each task. See the [context-speed reference](../../../docs/subsystems/studio-panel/context-speed.md).

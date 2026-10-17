# Native preview UI e2e

This suite builds the actual Go app with `ui_debug` and starts a fresh native macOS
window with `--debug-ui` for each test. [XCUITest](https://developer.apple.com/documentation/xcuiautomation/xcuiapplication) drives public controls and checks the
accessibility tree; it does not call preview internals, advance a test clock, or use
the scene selector to complete workflows.

From `studio/`, in a logged-in macOS desktop session with full Xcode selected:

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
with the app still running attaches its screenshot and accessibility tree to the Xcode
result; build and launch failures remain in the log and result bundle.

This is native UI coverage with a simulated backend. The existing fast `ui.NewTester`
regressions remain under `internal/pages`; real CLI/fake-engine boundary tests remain
under [`../e2e`](../e2e/README.md). None establishes real model qualification or
Windows/Linux native UI acceptance.

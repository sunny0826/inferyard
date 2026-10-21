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
The runner checks for an unlocked, logged-in console session before building or
opening the test app. An unavailable or unverifiable desktop fails immediately;
it does not skip tests or disable screenshot assertions. The desktop must stay
unlocked after this initial check.

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 python tests/ui/run.py
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

The local-engine scenarios cover a directory model through both bundles and a task report,
and LM Studio waiting for a fresh service, a failed second binding, manual retry and the
final two-bundle report. These are simulated services, not real engine acceptance.

The first-use scenario starts with no model or engine, acquires both through the
download controls, returns to the task, and runs both default bundles to a report.
It checks the resource guide and live queue labels, and retains screenshots before
resource acquisition, before execution, and during execution. Its owned test window
is resized to at most 1024 × 681 points and placed inside the main display's usable
area, excluding the menu bar and Dock. Every scenario uses this compact window so
local runs also exercise the CI viewport. Resource controls and complete charts are
scrolled into view using their current frames, rather than fixed scroll distances.
Scene choices use a bounded scroll list; tests check that it stays inside the window
and scroll each choice into view before selecting it.

The resource chart scenario first opens a dense 158-sample memory fixture with CPU
and temperature unavailable, then selects the CPU and multi-sensor fixture in the same
window. It captures the charts, switches temperature sources through the real selector,
and checks the displayed table. A separate engine-status scenario checks hover and click
controls, then starts and stops an engine without starting a benchmark.
The first-use workflow checks the running engine's disabled stop control and the live
queue panel during its existing benchmark, without preparing another task for those checks.
The optional `TestRecordedNativeResourceProjection` core test reads a new diagnostic run
from `INFERYARD_TEST_RESOURCE_RUN` and checks that actual CPU/temperature observations reach
Studio's projection. It is skipped when the path is absent and does not start a model.

This is native UI coverage with a simulated backend. The existing fast `ui.NewTester`
regressions remain under `internal/pages`; real CLI/fake-engine boundary tests remain
under [`../e2e`](../e2e/README.md). None establishes real model qualification or
Windows/Linux native UI acceptance.

Wait helpers check the current condition before polling; an already-present element
or enabled button needs no extra delay. Pending conditions retain their original timeouts.
Scrolling stops when the complete control is inside its viewport. Fractional scroll
distances round away from zero so a sub-point remainder cannot cause repeated no-op events.
Each independent workflow still launches a fresh app; tests do not share mutable state
across cases, accelerate the simulator clock, or bypass normal page controls.

The context-speed preview tests drive the existing task form through tier selection,
five simulated measurements, the six-column result, history reopening, cancellation,
startup failure, missing timing and result-read retry. Screenshots of these states are
retained in the result bundle even on success.
The suite also checks header visibility in the compact test window and the disclosure of
cancellation and missing-measurement diagnostics, retaining screenshots before and after expansion.
Production CLI integration is implemented; the user has approved the UI design.
Native execution and the real CLI/model workflow
remain separate verification steps. Result and history tables show the model and engine
recorded for each task. See the [context-speed reference](../../../docs/subsystems/studio-panel/context-speed.md).

# Native preview UI e2e

Both suites build the actual Go app with `ui_debug` and open one native macOS
window with `--debug-ui` for the entire selected suite. [XCUITest](https://developer.apple.com/documentation/xcuiautomation/xcuiapplication) drives public controls and checks the
accessibility tree; it does not call preview internals, advance a test clock, or use
the scene selector to complete workflows.

From the repository root:

```sh
mise run test:studio-ui       # Daily: 3 key workflows
mise run test:studio-ui:full  # Full: all 12 workflows; also used by CI
```

| Coverage | Daily | Full |
| --- | --- | --- |
| First use: recommendations, engine/model downloads, two bundles, live queue and report | Yes | Yes |
| Context-speed result, measurements, history and starting another task | Yes | Yes |
| Resource charts, missing sources and temperature selection | Yes | Yes |
| Prepared environment, downloaded-model switching and report preservation | — | Yes |
| Startup/download failures, cancellation, retries and reset isolation | — | Yes |
| Context-speed cancellation, missing timing and read failure | — | Yes |
| Engine hover/start/stop, local directory model and LM Studio binding retry | — | Yes |

Daily selects the same workflow functions used by Full; it does not shorten their
assertions or simulation. Use Full for queue, cancellation, retry, engine binding,
or test-harness changes, and before release. Daily success does not establish Full
coverage. Each result retains an attachment listing the selected suite and workflows.
Direct Xcode runs default to Full; the Python runner defaults to Daily and passes
the selection through Xcode's [test-runner environment](https://developer.apple.com/documentation/xcode/environment-variable-reference).

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
# Add --suite full for complete coverage.
```

The runner creates a new `build/ui-e2e/run-*` directory containing the build, log,
`Results.xcresult` and `run.json` (suite, exit code and elapsed build-and-test seconds).
The XCTest case duration excludes building; do not compare it with `run.json` elapsed time.
It opens and closes only the dedicated `PreviewHost.app` test app
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
It also checks platform-filtered recommendations and changes the selected model before
downloading, then checks the resource guide and live queue labels. It retains screenshots before
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
Pending checks use a monotonic deadline and poll every 100 ms while allowing the test
runner's run loop to process events. A missing condition still fails at the deadline;
failure screenshots and the accessibility tree remain attached by teardown.
Scrolling stops when the complete control is inside its viewport. Fractional scroll
distances round away from zero so a sub-point remainder cannot cause repeated no-op events.
One XCTest method groups workflows into named activities. The app launches, positions
its window, and terminates once; the suite checks that its process ID remains unchanged.
Between independent workflows, the scene selector resets window-local state and widget
state. Reusing the same scene presses Reset instead of selecting the unchanged option.
Failure, cancellation, and context-speed activities start from the prepared scene;
preparation remains covered by the normal benchmark and first-use activities. Scene
selection only establishes the starting state; each workflow uses normal page controls.
Each suite has a 600-second total allowance; individual control waits retain their existing
timeouts. A failure stops the suite and attaches the window and accessibility tree to the
result, under the failing activity. No simulator clock is accelerated.

Runner argument and failure-reporting checks require no native window:

```sh
mise exec -- python -m unittest discover -s tests/ui -p test_run.py
```

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

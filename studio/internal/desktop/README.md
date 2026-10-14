# Desktop notifications

`Subscribe` consumes the core EventBus replay ring on a background goroutine.
It maps `run.state` and `download.state` terminal states (done, failed,
cancelled), plus `report.created`, to fixed titles and one-sentence bodies.
Task paths and CLI output never enter notification content. Headless mode does
not register a subscriber; main starts desktop delivery only in MyGo WhenReady.
Delivery errors are logged and do not propagate to the event publisher. A replay
overrun is logged; desktop notifications are best-effort, not a durable queue.

MyGo v0.4.8 provides `NewNotification(NotificationOptions).Show()`.
The API was checked against the pinned source and
https://mygo.egoist.dev/docs/native#notifications . macOS requires an app bundle;
`scripts/package.py` produces one without running signing or notarization tools.

## T10 verification (2026-10-13)

Evidence directory: `build/t10-evidence/` (local, ignored by Git).
Every Go command below used command-scoped
`GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0`.

| Command | Exit | Evidence |
| --- | --- | --- |
| `go vet ./...` | 0 | `vet.log` |
| `go test ./... -count=1` | 0 | `test.log` |
| `go test -race ./...` | 0 | `race.log` |
| `sh scripts/build-all.sh` | 0, all three targets | `build.log` |
| `python3 scripts/verify-embed.py --binary dist/inferyard-studio-v0.1.0-darwin-arm64/inferyard-studio` | 0 | `embed.log` |
| `bun run --registry=https://registry.npmmirror.com web/tests/verify.mjs` | 0, all PASS | `web.log` |
| `python3 build/t10-evidence/native-check.py` | 0, notification arrival NOT passed | `native-check.log`, `native.log` |

The requested verbose PASS count was 79 before this change, 81 after adding two
notification tests. Tests publish actual EventBus events for all six terminal
cases and report completion, exercise delivery failure and headless suppression,
and exclude nonterminal/progress events. These tests do not prove OS delivery.

The native check launched the distributed `.app`, configured a temporary fixture
CLI, and used the authenticated HTTP API to create a download. The real core
workflow reached `failed`; notification delivery logged:

    desktop notification delivery failed: mygo: the user does not allow notifications

No first-permission dialog was observed. No actual notification arrival or
Notification Center screenshot was obtained. The screenshot tool could not
capture the remote-rendered System Settings window. Existing notification
permissions were not changed. Original Studio settings were restored and the
app exited 0. The fixture CLI performs no remote download or model inference.

Remaining verification: macOS notification arrival after notification permission
is granted; native Windows/Linux execution. Cross-compilation does not establish
platform runtime behavior. No signing, notarization, publishing, or push occurred.

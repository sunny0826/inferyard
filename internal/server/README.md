# HTTP/SSE server (frozen API v0, T4)

`server.go` dispatches directly through `net/http`, without a third-party router,
WebSocket or MyGo bindings. `Listen` accepts only literal `127.0.0.1`. Each server
creates a random 32-byte hex session token and compares it in constant time.
REST requires `Authorization: Bearer`; only entry pages, reports and EventSource
accept the query token. Assets remain public. Every response sets
`Referrer-Policy: no-referrer`. No request URL, query, User-Agent or token is
logged. Main optionally publishes the initial URL through an inherited private
pipe (`-session-fd`, descriptor >=3); it never persists the token or logs the URL.

All application errors use `{error:{code,message,limitations,details}}` with fixed
messages. CLI stdout is parsed for structured limitations, never used as a free
text message. Report generation uses the application context, not the HTTP
request context. The synchronous handler has no short write timeout.

SSE subscribes before replay to avoid a replay/live race. Subscribers receive
coalesced notifications and then read the retained ring. A stale/future replay
cursor emits `sync.reset`; live subscribers that overrun the ring also reset.
A single connection carries all event types. Heartbeats occur after 15 seconds
without events. Event writes use a five-second deadline for stalled clients.
There are no M0 `count` events.

## Contract coverage

All routes are dispatched in `server.go:serve`; service implementations:

| Method / route | Implementation |
| --- | --- |
| GET /api/status | core/service.go: Status |
| GET /api/settings | core/service.go: Settings |
| PUT /api/settings | core/service.go: UpdateSettings |
| GET /api/device | core/service.go: Device |
| POST /api/device/refresh | core/service.go: RefreshDevice |
| GET /api/runs | core/aggregate.go: History, scan |
| POST /api/runs | core/runs.go: StartRuns |
| POST /api/runs/resume | core/runs.go: Resume |
| GET /api/run-tasks | core/runs.go: RunTasks |
| GET /api/run-tasks/{task_id} | core/runs.go: RunDetail |
| POST /api/run-tasks/{task_id}/cancel | core/runs.go: CancelRun |
| GET /api/reports | core/reports.go: Reports |
| POST /api/reports | core/reports.go: CreateReport |
| GET /reports/{report_id}/report.html and sibling files | core/reports.go: ReportFile; server.go: serve |
| GET /api/downloads | core/downloads.go: Downloads |
| POST /api/downloads | core/downloads.go: CreateDownload |
| POST /api/downloads/{task_id}/cancel | core/downloads.go: CancelDownload |
| GET /api/events | server.go: events; core/events.go: EventBus |

| SSE event | Producer |
| --- | --- |
| run.state | core/runs.go: state |
| run.progress | core/runs.go: executeRun |
| run.resources | core/runs.go: executeRun |
| download.state | core/downloads.go: CreateDownload, download |
| download.progress | core/downloads.go: download |
| device.updated | core/service.go: RefreshDevice |
| report.created | core/reports.go: CreateReport |
| sync.reset | server.go: events |

## Reproducible verification

All commands use command-scoped `GOTOOLCHAIN=go1.27.2`,
`GOPROXY=https://goproxy.cn,direct`, `CGO_ENABLED=0` (2026-10-12).

| Acceptance | Command/evidence | Exit |
| --- | --- | --- |
| 1. Frozen contract | Route/event tables above; exact JSON tags in core/types.go | — |
| 2. Authentication/binding | `go test ./internal/server -run TestAuthenticationAndLoopback -v`: missing/wrong/bare token rejected, Bearer accepted, non-127.0.0.1 listen refused | 0 |
| 3. Fixture run → SSE | `go test ./internal/server -run TestFixtureRunSSE -v`: POST runs, evidence directory creation, tail, state/progress/resources and done observed over a real HTTP connection | 0 |
| 4. Report static | `go test ./internal/server -run TestReport -v`: HTML 200, missing 404, traversal 400, client-disconnect completion | 0 |
| 5. Vet | `go vet ./...` | 0 |
| 5. Tests | `go test ./...`: 81 passed | 0 |
| 5. Race | `go test -race ./...` with CGO_ENABLED=0 on this cached toolchain: 81 passed | 0 |
| 5. Windows build | `GOOS=windows GOARCH=amd64 go build -o /tmp/inferyard-studio-t4-windows.exe .` | 0 |
| 5. Linux build | `GOOS=linux GOARCH=amd64 go build -o /tmp/inferyard-studio-t4-linux .` | 0 |
| 6. Single binary | `go build -o /tmp/inferyard-studio-t4-darwin .` then `python3 scripts/verify-embed.py --binary /tmp/inferyard-studio-t4-darwin` | 0 |
| 7. Q5 measurements | `go test ./internal/core ./internal/server -run 'Test(EventReplayLoad\|FixtureRunSSE\|HeartbeatReplayAndReset)$' -v` | 0 |

The embed harness temporarily renames web/dist and restores it in `finally`. The
binary runs in an empty temporary working directory without Vite. Entry HTML is
200, embedded JS is 230232 bytes, CSS is 5813 bytes, unauthenticated entry is 401.
The session travels through an anonymous pipe. The harness verifies that the
token is absent from captured logs and the headless process exits 0. It writes no
artifacts under web/ or docs/. A compiled standalone test binary also checks
`web.Assets` (`TestEmbeddedArtifact`, HTML 439 bytes).

## Q5 fixture measurements

Observed on the development Mac, not a real-model throughput benchmark:

- Input: 40 event records over approximately 2 seconds, plus four memory samples.
- HTTP client received 4 run.state, 4 run.progress, 3 run.resources events; no
  sync.reset during live delivery. Progress minimum interval: **500.136 ms**.
- Idle SSE heartbeat arrived after **15.001922 s**.
- A separate 10000-event synthetic ring burst took **10.274458 ms**; 2048 records
  remained, 2047 newer records replayed from the oldest retained ID. Old and
  future IDs correctly required reset.

Keep the initial 2048-event buffer, 15-second heartbeat and 500-ms progress
interval. At the contract's assumed 5 events/s the ring spans about 409.6 seconds
(calculated, not a measured long-duration retention run). Concurrent downloads
increase the rate and reduce that span; reset plus REST remains the recovery
mechanism. These data do not establish production steady-state throughput or
memory consumption under large numbers of downloads.

## Boundaries and proposals

Q4 wording proposal and source references are in `../core/README.md`; frozen docs
were not edited. No added dependencies. This delivery uses fixture executors and
T2 subprocess tests; it does not start a real model server or run. The native
MyGo GUI was also smoke-tested with `-spike-evidence /tmp/inferyard-studio-t4-gui`:
local HTTP loaded, two DOM title checks returned InferYard Studio, screenshot
capture succeeded, Device rendered its unconfigured empty state with SSE connected,
and the process exited 0 after SIGINT. Screenshot: `/tmp/inferyard-studio-t4-gui/desktop.png`
(temporary local evidence; no frontend files changed).
Cross-compilation does not prove Windows/Linux process runtime behavior. Existing
`docs/reviews/` untracked files belong to other work and are not included.

# Frontend (T3 + T5)

- `bun run dev:mock`: Vite with in-memory mock (`VITE_API_MODE=mock`). Open `#/dashboard`, `#/runs`, `#/runs/<task_id>`, `#/device`, or `#/settings`.
- `bun run dev:web`: real API mode. REST and SSE use the page origin; integration requires a server exposing the contract routes on that origin. The embedded production build always uses real APIs.
- `bun run typecheck`: strict TypeScript plus Svelte component checks.
- `bun run build`: build frontend into `web/dist` (ignored build output, retained for Go embed).
- `bun run build:desktop`: frontend plus the existing Go desktop build.
- `bun run test:web`: repeatable local Chrome verification. Requires installed Google Chrome; does not download a browser. Starts and closes local Vite/HTTP fixtures automatically.

The mock starts with an empty device snapshot; refresh publishes a device update after 650 ms. Settings persist only for the current page session. `/mock/blocked` as the inferyard path simulates a save error with `limitations`; relative paths simulate validation failures. Mock mode never invokes the CLI.

`src/api/types.ts` is an unmodified copy of `docs/api-types.ts`. T5 refreshes the frontend mirror from the frozen `6a07b19` version. The existing refresh acknowledgment schema narrows the documented successful value to `{ refreshing: true }`. Directory updates follow `UpdateSettingsRequest`: unchanged fields are omitted, changed directories must be nonempty absolute paths, and only `inferyard_path` can be reset to null. The prose in §4.2 is ambiguous about nullable directory updates; integration should resolve against the frozen contract.

TypeScript 6 is used because the installed Svelte checker does not support TypeScript 7 standalone. Runtime schemas validate the T3 responses and preserve extensible CLI fields. Native EventSource owns Last-Event-ID and reconnect behavior; the same instance remains alive across hash navigation. `sync.reset` rebuilds Device, Settings, run history, task list, and the selected task detail (including progress and resources), including data for pages that are not visible. Other future pages must join this rebuild callback.

Verification on 2026-10-12:

- Strict tsc and svelte-check: pass, zero Svelte errors/warnings.
- Production Vite build: pass, output in `web/dist`.
- Local Chrome: empty device state, refreshing button, event-driven snapshot, device sections, settings save and effective paths, error message/limitations, path rejection, automatic PATH selection, placeholder navigation, in-session persistence, 390 px layout, no localStorage writes: pass.
- Local HTTP fixture with native EventSource: Bearer token, SSE query token, reconnect with Last-Event-ID 41, sync.reset REST rebuild of Device and Settings, contract error body display: pass.
- No browser page exceptions observed.

Not verified: real Go/CLI integration, MyGo native webview, production backend SSE replay, other browsers/platforms, frozen contract alignment. No Go or main inferyard repository files were changed.


## T5 runs

Dashboard and Runs share a launch form, session-local launch groups, a task queue,
and batch/standalone evidence history. Task links open `#/runs/<task_id>`.
History is read from `GET /api/runs`; it is never inferred from task completion.
Batch overview priority is cancelled > dirty > partial > complete. Standalone
resume is disabled even if an inconsistent server advertises `resumable=true`.
A 409 not_resumable disables the rejected source and keeps its error visible.

Default launch uses two bound config paths. Single mode requires a separate,
initially unchecked confirmation; only that path sends `allow_single_bundle`.
The response tasks stay grouped in memory across navigation. Reload/server restart
cannot reconstruct launch groups because the contract defines no group ID.
Resume sends from_run with either both endpoint_url/server_pid or neither.

All run REST and SSE payloads are Zod-parsed. Pending detail/list reads retain
incoming events and replay them after the snapshot resolves. Resources are
appended, deduplicated by UTC, ordered, and bounded to 600 points; reset replaces
the snapshot. uPlot preserves null gaps, converts bytes to explicitly labeled GiB,
and plots server-provided CPU percent and temperature in Celsius. Numeric samples
remain available in the expandable table. No CLI or model server is started.

Mock fixtures:

- Default config paths: complete, two serialized tasks, two selected cases.
- A path containing `/slow` waits for cancellation.
- `/blocked`, `/error`, `/partial`, `/dirty`: exit 2, 4, 3, 3 respectively.
- `/invalid`: invalid_request; `/outside`: output_root_outside_results.
- Repeated active config: queue_duplicate.
- `resume-source`: one-case frozen resume subset; `stale-source`: deliberately
  stale resumable hint rejected with not_resumable.
- The third sample of the eight-point run is missing; first CPU sample is also
  null. Queued cancellation has exit=null; running cancellation exits 130.

### T5 verification (2026-10-12)

Commands (from repository root):

```sh
bunx --registry=https://registry.npmmirror.com tsc --noEmit --strict -p web/tsconfig.json
bunx --registry=https://registry.npmmirror.com svelte-check --tsconfig web/tsconfig.json
bun --registry=https://registry.npmmirror.com run build
bun --registry=https://registry.npmmirror.com run test:web
```

Acceptance evidence:

1. `tests/verify-runs.mjs`: hierarchy, default double launch, single confirmation,
   response grouping, queued/running cancellation, resume pair validation and
   subset, stale resume refusal, all five exit mappings and raw limitations.
2. Same script: live uPlot canvas, eight SSE samples, null/zero distinction,
   GiB conversion, replay deduplication and missing-value table assertions.
3. Same script: priority aggregation for all four history states, partial/dirty
   histories after exit 3, cancelled history after exit 130, disabled standalone
   resume. `tests/verify-runs-wire.mjs`: real HTTP 400/409 errors and exact bodies.
4. Strict TypeScript, Svelte check (0 errors/warnings), production Vite build pass.
5. `tests/verify.mjs` runs both T5 scripts plus T3 regression checks in installed
   Google Chrome. HTTP fixture verifies native run.state/progress/resources,
   in-flight snapshot/event merging, sync.reset rebuilding all run views, and
   unknown-task errors. Existing native Last-Event-ID reconnect test also passes.
   No downloaded browser or external service is used by the browser harness.
6. 390px RunDetail and Runs have no document overflow. Tables scroll locally.
   Screenshots: `/tmp/inferyard-t5-detail-mobile.png` and
   `/tmp/inferyard-t5-runs-mobile.png`; detail screenshot visually inspected.

Added dependency: `uplot@1.6.32` for the required streaming resource curves.

Contract clarification proposal: T5's task wording mentions raw
system_mem_available/service_rss/service_cpu_ticks/temperature. Frozen §4.5 sends
mem_available_bytes/service_rss_bytes/service_cpu_percent/temperature_celsius.
CPU ticks conversion belongs to the backend and cannot be repeated from this API.
Implementation follows the frozen API; no change to docs/api-contract.md or
docs/api-types.ts is proposed or made. The task description should use the frozen
field names and make the backend ownership of the CPU calculation explicit.

Not verified: real Go/inferyard integration (T7), real CPU ticks calculation,
hardware sampling, native MyGo webview for these pages, other browsers/platforms.

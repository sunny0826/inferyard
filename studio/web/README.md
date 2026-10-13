# Frontend (T3 + T5 + T6)

- `bun run dev:mock`: Vite with in-memory mock (`VITE_API_MODE=mock`). Open `#/dashboard`, `#/runs`, `#/runs/<task_id>`, `#/reports`, `#/models`, `#/device`, or `#/settings`.
- `bun run dev:web`: real API mode. REST and SSE use the page origin; integration requires a server exposing the contract routes on that origin. The embedded production build always uses real APIs.
- `bun run typecheck`: strict TypeScript plus Svelte component checks.
- `bun run build`: build frontend into `web/dist` (ignored build output, retained for Go embed).
- `bun run build:desktop`: frontend plus the existing Go desktop build.
- `bun run web/tests/verify.mjs` **from the repository root**: repeatable local Chrome verification. Requires installed Google Chrome; does not download a browser. Starts and closes local Vite/HTTP fixtures automatically.

The mock starts with an empty device snapshot; refresh publishes a device update after 650 ms. Settings persist only for the current page session. `/mock/blocked` as the inferyard path simulates a save error with `limitations`; relative paths simulate validation failures. Mock mode never invokes the CLI.

`src/api/types.ts` is an unmodified copy of `docs/api-types.ts`. T5 refreshes the frontend mirror from the frozen `6a07b19` version. The existing refresh acknowledgment schema narrows the documented successful value to `{ refreshing: true }`. Directory updates follow `UpdateSettingsRequest`: unchanged fields are omitted, changed directories must be nonempty absolute paths, and only `inferyard_path` can be reset to null. The prose in §4.2 is ambiguous about nullable directory updates; integration should resolve against the frozen contract.

TypeScript 6 is used because the installed Svelte checker does not support TypeScript 7 standalone. Runtime schemas validate the T3 responses and preserve extensible CLI fields. Native EventSource owns Last-Event-ID and reconnect behavior; the same instance remains alive across hash navigation. `sync.reset` rebuilds Device, Settings, run history, task list, and the selected task detail (including progress and resources), including data for pages that are not visible. Reports and Downloads also join this rebuild callback.

Verification on 2026-10-12:

- Strict tsc and svelte-check: pass, zero Svelte errors/warnings.
- Production Vite build: pass, output in `web/dist`.
- Local Chrome: empty device state, refreshing button, event-driven snapshot, device sections, settings save and effective paths, error message/limitations, path rejection, automatic PATH selection, page navigation, in-session persistence, 390 px layout, no localStorage writes: pass.
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


## T6 reports and downloads

Reports lists directory names, directory mtime (unknown when null), and each
source run, including “来源不可达” for broken source paths. Select 1–100 unique
history paths to generate a report. The synchronous POST uses a 180-second
client timeout. `report.created` refreshes the list even when another client
initiated generation. Report links open `/reports/{id}/report.html?token=...`
in a new top-level tab with `noopener noreferrer`; no iframe is used.

Models validates `out_name` immediately with `^(?!\.+$)[A-Za-z0-9._-]+$`.
Download bytes are shown as MiB and exact bytes (1 MiB = 1,048,576 bytes);
speed remains bytes/s, with null shown as unknown. The frozen download contract
has no total field, so no percentage or percentage progress bar is rendered.
Failed and cancelled tasks show that their output directory is retained.
Errors reuse ErrorNotice and preserve server code, message, limitations and reason.
Download snapshots replay SSE received during pending reads; POST responses do
not replace newer terminal events. `sync.reset` replaces both asset lists.

Mock fixtures: choose `old-dirty` to get a report `unsupported_format` error.
Download names containing `slow` wait for cancellation, names containing `fail`
fail with a retained directory, and other names complete after four progress
updates. Reusing any occupied name returns `output_exists`. Mock report links
have the production navigation shape; actual HTML navigation is served and
verified by the wire fixture.

### T6 verification

**Run from `/Users/guoxudong/codes/local-ai/inferyard-studio`:**

```sh
bun run web/tests/verify.mjs
bunx --registry=https://registry.npmmirror.com tsc --noEmit --strict -p web/tsconfig.json
bunx --registry=https://registry.npmmirror.com svelte-check --tsconfig web/tsconfig.json
bun --registry=https://registry.npmmirror.com run build
```

Do not run the verification script from `web/`: Vite loses the repository-root
configuration and reports an App.svelte parse error.

`tests/verify-assets.mjs` covers mock reports, source/time nulls, multiselect,
pending state, generation failure, download validation, progress, cancellation,
retained failures, occupied names, successful results, and 390px overflow checks.
`tests/verify-assets-wire.mjs` covers real HTTP bodies/statuses, 100-selection
limit, 180-second timeout configuration, authenticated top-level HTML navigation,
error passthrough, native SSE, pending snapshot merging, early terminal events,
and sync.reset rebuilding both lists. `tests/verify.mjs` runs these alongside
all T3/T5 regression checks in installed Chrome.

Mobile screenshots: `/tmp/inferyard-t6-reports-mobile.png` and
`/tmp/inferyard-t6-models-mobile.png`.

Added dependencies: none. Contract files and the frontend type mirror are unchanged.
Task wording clarification: “total=null” means unknown download total; the frozen
API actually omits total entirely. No contract patch is required.

Not verified: real Go/inferyard integration (T7), actual remote model downloads,
real CLI report generation, native MyGo webview, other browsers/platforms.

T6 acceptance result (2026-10-12): full Chrome harness PASS, including T3/T5
regressions and both T6 segments; strict tsc PASS; svelte-check 0 errors / 0
warnings; production build PASS. Both 390px screenshots were visually inspected.
The timeout assertion verifies the configured 180,000 ms signal; the harness
does not wait three minutes to exercise timeout expiry.

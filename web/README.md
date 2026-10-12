# T3 frontend

- `bun run dev:mock`: Vite with in-memory mock (`VITE_API_MODE=mock`). Open `#/device` or `#/settings`.
- `bun run dev:web`: real API mode. REST and SSE use the page origin; integration requires a server exposing the contract routes on that origin. The embedded production build always uses real APIs.
- `bun run typecheck`: strict TypeScript plus Svelte component checks.
- `bun run build`: build frontend into `web/dist` (ignored build output, retained for Go embed).
- `bun run build:desktop`: frontend plus the existing Go desktop build.
- `bun run test:web`: repeatable local Chrome verification. Requires installed Google Chrome; does not download a browser. Starts and closes local Vite/HTTP fixtures automatically.

The mock starts with an empty device snapshot; refresh publishes a device update after 650 ms. Settings persist only for the current page session. `/mock/blocked` as the inferyard path simulates a save error with `limitations`; relative paths simulate validation failures. Mock mode never invokes the CLI.

`src/api/types.ts` is an unmodified copy of `docs/api-types.ts`. The refresh acknowledgment `{ refreshing: true }` is described in contract §4.3 but has no named type in that mirror; it is declared locally without changing the mirror. Directory updates follow `UpdateSettingsRequest`: unchanged fields are omitted, changed directories must be nonempty absolute paths, and only `inferyard_path` can be reset to null. The prose in §4.2 is ambiguous about nullable directory updates; integration should resolve against the frozen contract.

TypeScript 6 is used because the installed Svelte checker does not support TypeScript 7 standalone. Runtime schemas validate the T3 responses and preserve extensible CLI fields. Native EventSource owns Last-Event-ID and reconnect behavior; the same instance remains alive across hash navigation. `sync.reset` rebuilds both currently implemented REST resources, including the page that is not visible. Future pages must join this rebuild callback.

Verification on 2026-10-12:

- Strict tsc and svelte-check: pass, zero Svelte errors/warnings.
- Production Vite build: pass, output in `web/dist`.
- Local Chrome: empty device state, refreshing button, event-driven snapshot, device sections, settings save and effective paths, error message/limitations, path rejection, automatic PATH selection, placeholder navigation, in-session persistence, 390 px layout, no localStorage writes: pass.
- Local HTTP fixture with native EventSource: Bearer token, SSE query token, reconnect with Last-Event-ID 41, sync.reset REST rebuild of Device and Settings, contract error body display: pass.
- No browser page exceptions observed.

Not verified: real Go/CLI integration, MyGo native webview, production backend SSE replay, other browsers/platforms, frozen contract alignment. No Go or main inferyard repository files were changed.

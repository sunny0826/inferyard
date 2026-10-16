# InferYard Studio

InferYard Studio is a desktop application for local model evaluation. Its native UI is
written in Go on the [onevra](https://github.com/oneo-me/onevra) component library (MIT),
which is built on MyGo v0.5.4, and calls `core.Service` directly. In-process events refresh
the windows. Studio has no browser dashboard, headless mode, REST/SSE server, or generated
JavaScript bindings. The separately installed [InferYard CLI](../cli/README.md) executes
operations and owns the evidence contract.

## Pages and first launch

The sidebar opens on Reports and lists Reports, Models, Downloads, Device, and Settings.
A “新任务” (New task) button above the navigation opens Dashboard; no sidebar item is
selected while Dashboard is open. The six page bodies are:

| Page | Purpose |
| --- | --- |
| Dashboard | Prepare the environment with one action, choose workspace bundles and a model/engine pair, then create candidates, start and bind the service, run, and generate a task report. Inspect case progress, view CPU/memory curves, and resume eligible runs opened from Reports. |
| Device | Inspect and refresh the hardware snapshot with inline missing-measurement reasons. |
| Models | Select an engine, then a model, and open a prefilled task through the same entry as New task. Active services or runs show the pair as text. Empty registries offer only Go to Downloads. Inspect service status without start, stop, binding, or orphan recovery actions; orphaned engines show a warning and hide the new-task action. No absolute paths. |
| Downloads | Inspect, cancel, and retry download tasks (with their actual directory names), and create new model/engine downloads into settings-configured directory names. |
| Reports | Browse historical tasks, enter or resume a run, inspect live details, verify the workspace report, and read each report index inside the main window. |
| Settings | Configure the CLI path, workspace, results directory, and model directory — the only page that shows full paths — plus the model/engine download directory names. |

Dashboard keeps the active task's selected bundles and new run paths in memory. New task
cancels binding and all non-terminal runs, waits for them to finish, and returns to the
shared engine/model selection without cancelling downloads or deleting evidence. The form
lists only workspace bundles and checks `zh-core` when present. An unchecked bundle does
not block the task. The old quick-run form, queue card, and run history card are removed from Dashboard.
Reports starts with task history, followed by the report list. Creating another report from
1–100 historical runs is available in a collapsed section. Verification appears only when
workspace `next_action` is `verify`; a `verified` report shows its verified status instead.
Without an active task, the page restores workspace progress from `progress.v1`.

Prepare environment rechecks readiness after installing the CLI, provisioning missing
model/results directories, and initializing a new workspace. Existing initialized workspaces
skip `init`; new workspaces omit `--bundle`. The first workspace device check runs once
with `--models-root` and no `--model`. A later model-specific check still supplies `--model`.
The task generates `report --runs` for its own sealed runs with verification disabled, displays
the successful output path, and leaves verification on Reports. Duration and unfinished
batch plans display their blocking reason without an execution button.

A fixed top-right status row shows engine and queue state; its idle text is
“引擎 已停止 · 队列 空”. Active runs and non-stopped engine states stay visible during
background refresh and busy operations. Otherwise, refresh and operations show “执行中”.
During a run, the model-service card collapses into this row, including between bundles
in an active task. Once execution ends, service recovery controls remain available in
the run/resume stage. Stop is available for a starting or running engine
when no non-terminal run exists and Studio is not busy, except on Models.

When no CLI is configured, the Dashboard’s Prepare environment action installs the CLI: it reuses an existing
`inferyard`/`uv` when found (including Homebrew locations outside the GUI PATH), downloads
a pinned standalone `uv` into the Studio tools directory when none exists — verified
against the release checksum, without touching shell configuration — and installs
`inferyard` matching the Studio version from PyPI via `uv tool install`. Manual path
configuration in Settings remains available.

Run benchmark creates fresh candidates for the checked bundles and starts and binds the
service automatically. Binding has its own cancellation context; a failure offers a retry
and the advanced manual binding fold. Existing single-config runs rebind the new PID before
running; eligible resumes pass the started engine's PID and endpoint directly to `resume`.
Studio stops its managed engine on exit and never restarts it automatically.
Dashboard and Models show Studio-started services from `EngineState`, independently of
service discovery. Starting, running, and stopping services remain visible when scanning
fails or the model directory is unavailable. Leftover records are labelled unconfirmed;
failed and stopped services are not shown as active. Candidate names come from the service
state, not the current model selection.

Other endpoints with model directories appear under the collapsed “待确认的兼容接口”
(Unconfirmed compatible interfaces) section. They may be proxies: a model directory does
not prove a model is loaded locally. Expanding an interface shows endpoint health and
ambiguous-listener warnings; its model directory remains separately collapsed. Partial
scan limitations remain visible. Only Dashboard offers explicit external binding after a binding failure, with the
discovered PID and endpoint, through the existing CLI checks; Models remains read-only.
Discovery does not establish bind eligibility: rejection reasons such as
`startup_arguments_mismatch` remain visible. The collapsed advanced manual binding controls
remain available. See the [service discovery contract](../cli/docs/cli-surface.md#本机服务发现契约)
for the read-only probe scope and the [Studio subsystem reference](../docs/subsystems/studio-panel.md#约束与失败行为)
for display deduplication rules.
Binding always goes through the CLI's PID and start-time checks. Studio persists an
ownership record (PID + start ticks) for each engine it starts. If Studio exits
abnormally and the record survives, the next launch re-verifies the process start ticks
before offering an explicit "stop leftover engine" action on Dashboard; a PID whose identity no
longer matches is never signalled — only the stale record is removed. Invalid records,
unverifiable identities, and failed cleanup expose a "delete record only (do not terminate
process)" action for workspace `logs/engine-owner.json`. It re-reads the record and refuses
deletion when the process identity still matches; that process must use the stop action.
Successful deletion clears the orphaned state and allows a new startup. Model credentials
are referenced only by an environment-variable name.

The Downloads page presents one form card: the recommended llama-server engine first,
then SmolLM2-135M-Instruct-Q4_K_M. Both HTTPS URLs are prefilled and editable; model
credentials accept only an environment-variable name under “需要凭证时”. Empty pages omit
the task card; existing tasks list engines before models. Download
directory names come from Settings (defaults `model` / `engine`); an occupied name
automatically gains a `-2`/`-3` suffix instead of prompting per download, and the task
detail shows the actual directory. Name allocation stops after 1,000 occupied candidates
with `output_name_exhausted`; new binding directories use the same limit. Engine
archives are downloaded and unpacked by the Studio side — preserving the sibling shared
libraries and recreating only in-archive relative symlinks — into a fresh directory that
never overwrites existing files.

The initial effective workspace is `~/.inferyard`. Preparing directories is an explicit,
idempotent action; saving settings alone does not create them. CLI `init` requires a new
directory. For an existing uninitialized workspace, choose a new child directory; Studio
switches settings to it only after initialization succeeds and retains existing files.

## Design and page review

The [Studio Design System](docs/design-system.md) defines shared layout, theme, typography,
components, feedback, accessibility, and verification rules against the onevra v0.2026.10
component library on MyGo v0.5.4, including its collapsible, resizable sidebar shell and
component mapping.
The [page audit](docs/design-system-audit.md) records implementation gaps across all five
pages, the run detail view, and the report view. These documents define the target and
review scope; they do not establish native desktop acceptance.

## Build and run

Use the repository's mise-managed Go 1.27.2. Run these commands from `studio/`:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -o build/inferyard-studio .
./build/inferyard-studio
```

On Windows, build with `-o build/inferyard-studio.exe` and launch that executable.
Development also works with `mise exec -- go run .`. The app uses a plain Go build;
there is no frontend installation or build step. Environment overrides above apply only
to the command, not to global configuration.

To cross-build macOS arm64, Windows amd64, and Linux amd64 and assemble unsigned distributions:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct sh scripts/build-all.sh
```

[build-all.sh](scripts/build-all.sh) compiles the three binaries into `build/<os>-<arch>/`
and invokes [package.py](scripts/package.py), using mise-managed Python, to assemble
`dist/inferyard-studio-v<version>-<os>-<arch>/`. Packaging copies binaries and writes launch
instructions; it does not sign, notarize, or publish. On macOS, launch the packaged app with
`open "InferYard Studio.app"` to use desktop notifications.

The version's single source is [mygo.config.ts](mygo.config.ts). Go embeds the configuration,
so an ordinary rebuild picks up version changes without generated files or linker flags.
The packaging script reads the same version.

## Platform dependencies

| Platform | Native windows |
| --- | --- |
| macOS 12+ | Metal |
| Windows 10/11 | Direct3D 11 (WARP fallback) |
| Linux | GTK 3 and OpenGL; MyGo can fall back to CPU rendering |

Reports use native controls inside the main window and do not create a WebView.
The CLI still generates offline HTML, but Studio reads `index.json`: the existing
`runs[].dashboard` quality, category denominators and latency, plus `runs[].requests`.
Studio does not rescore results. Null latency displays as —. An unreadable index is
marked in the list without hiding other reports; opening it offers retry and return.

## Safety and operation rules

Model endpoints remain local loopback endpoints. Reports and evidence can contain private
local paths and must not be exposed on the LAN. Tokens, credentials, signed URLs, and full
session URLs must never enter logs, persisted settings, or user-facing errors. Show fixed
error summaries and structured CLI limitation reasons rather than raw stderr.

Settings are stored in the system user configuration directory under
`InferYard Studio/settings.json` (POSIX directory mode 0700, file mode 0600). Invalid JSON
or field types fall back to defaults with a diagnostic; the file is replaced only when
the user saves. Historical scans skip damaged runs and log the skipped count. Evidence
directories remain the source of truth; in-memory task lists do not survive app restart.

Runs execute serially and retain CLI host locking, identity checks, cancellation, and
draining. The default benchmark includes both `zh-core` and `zh-svg-pelican`; a single
bundle requires an explicit choice. Bound configuration output roots must stay inside the
configured results root; duplicate queued configurations are rejected.

| CLI exit | Studio meaning |
| --- | --- |
| 0 | Completed task. |
| 2 | Blocked/failed task with the CLI's limitation reasons. |
| 3 | Finished task with incomplete evidence; display partial status, not an execution error. |
| 4 | Tool/evidence failure. |
| 130 | Cancelled task. |

Cancelling a queued task requires no child process or exit code. Cancelling an active task
requests interruption and waits for exit. Terminal states are final. History preserves the
priority `cancelled` > `dirty` > `partial` > `complete`; sealing alone is not verification.
An unreadable host-state marker must not be treated as clean. Resume is limited to eligible
unfinished batch runs, with the CLI making the final decision; standalone runs cannot resume.
Endpoint and PID overrides must be provided together. Fixed-window workloads may require a
new window instead of resume. Progress uses the frozen selected-case denominator, including
the frozen resume subset when applicable.

Unknown measurements remain `null` with a reason, never zero or fabricated percentages.
Memory is stored in bytes and rates in bytes/s; display conversions are explicit. Resource
curves retain missing samples. Downloads without a known total do not show a percentage.
Downloads may run concurrently with the serial run queue. Failed or cancelled download
directories remain occupied; CLI owns cleanup of temporary `.part` files.

Report generation validates 1–100 distinct run paths within the results root and writes a
new uniquely named directory under the derived sibling `reports` directory. Original runs
and reports are never overwritten. Report paths must remain within that report directory;
missing source runs are shown as unavailable without preventing the report from opening.
Evidence interpretation follows the CLI's
[lineage rules](../cli/docs/data-contract.md#证据血缘与比较结论).

Desktop notifications announce terminal run/download states and completed reports with
fixed text and no task paths. Denied notification permission or delivery failures do not
change task outcomes.

## Verification boundaries

From `studio/`, the code checks are:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go vet ./...
mise exec -- env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
```

`go test ./...` includes the boundary e2e suite under `tests/e2e`: the real CLI executes
every operation against a fake engine (the test binary re-executing itself), each CLI call
is recorded to a blame transcript, and workspace evidence is validated against the exported
schemas in `cli/schemas/`. The suite attributes every failure to the CLI side or the Studio
side; see [tests/e2e/README.md](tests/e2e/README.md) for the layout, fixture boundaries, and
the `INFERYARD_E2E_CLI` / `INFERYARD_E2E_ROOT` knobs. CI runs it on macOS and Windows via
`.github/workflows/studio-check.yml`.

Native view tests, the e2e suite, and fixtures do not establish real-device acceptance. Verify all five
pages, the Dashboard's stage navigation and restored progress, resource curves, and the
report index view on actual desktop windows. Cross-compilation is not Windows/Linux runtime evidence.
The native UI migration requires fresh acceptance; historical Web UI checks do not qualify
it. Original diagnostic artifacts under `docs/evidence/` are retained as historical evidence,
not as current build instructions. Signing, notarization, publication, and the full real
model workflow require separate verification.

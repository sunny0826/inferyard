# InferYard Studio

InferYard Studio is a desktop application for local model evaluation. Its native UI is
written in Go on the [onevra](https://github.com/oneo-me/onevra) component library (MIT),
which is built on MyGo v0.4.8, and calls `core.Service` directly. In-process events refresh
the windows. Studio has no browser dashboard, headless mode, REST/SSE server, or generated
JavaScript bindings. The separately installed [InferYard CLI](../cli/README.md) executes
operations and owns the evidence contract.

## Pages and first launch

The main window has five pages:

| Page | Purpose |
| --- | --- |
| Dashboard | Prepare the environment (CLI installation, directory provisioning, workspace initialization), work through the current benchmark stage (device check, candidate, service startup and binding, run, report verification), queue or cancel evaluations, inspect history and case progress, view CPU/memory curves, and resume eligible runs. |
| Device | Inspect hardware, measurement availability, collection sources, and device-check results. |
| Models and downloads | Prepare model and engine assets, inspect and cancel downloads, manage the engine service, and check binding hints. |
| Reports | Generate reports and open each offline HTML report in its own in-app WebView window. |
| Settings | Configure the CLI path, workspace, results directory, and model directory. |

The main window always opens on the Dashboard, which drives the full benchmark flow from
the CLI-generated `progress.v1` workspace state: it shows the current stage with its
blocking reason, offers the matching action inline, and hosts quick re-runs, the task
queue, run history, resume, and the in-page run detail view. Models and downloads
prepares assets; Device runs the device check and shows its details; Reports generates
and verifies the combined report; Settings edits paths. Reopening Studio restores
progress from workspace evidence through `progress.v1`. A missing CLI or incomplete
configuration does not prevent opening any page.

When no CLI is configured, the Dashboard offers one-tap installation: it reuses an existing
`inferyard`/`uv` when found (including Homebrew locations outside the GUI PATH), downloads
a pinned standalone `uv` into the Studio tools directory when none exists — verified
against the release checksum, without touching shell configuration — and installs
`inferyard` matching the Studio version from PyPI via `uv tool install`. Manual path
configuration in Settings remains available.

The Dashboard's stage actions follow the CLI-generated next command. Studio starts the frozen
engine as a managed child process, waits for its health endpoint, and stops it when Studio
exits — it owns the processes it starts and never restarts them automatically; services
the operator started themselves can still be bound from the process list. Binding always
goes through the CLI's PID and start-time checks. If Studio exits abnormally and leaves
an engine process running, stop it manually and rebind on the Dashboard. Model credentials
are referenced only by an environment-variable name.

Models and downloads prefills built-in test assets: a tiny SmolLM2-135M GGUF (pinned Hugging
Face revision) and the pinned Prism engine archive for the current platform. Engine
archives are downloaded and unpacked by the Studio side — preserving the sibling shared
libraries and recreating only in-archive relative symlinks — into a fresh directory that
never overwrites existing files.

The initial effective workspace is `~/.inferyard`. Preparing directories is an explicit,
idempotent action; saving settings alone does not create them. CLI `init` requires a new
directory. For an existing uninitialized workspace, choose a new child directory; Studio
switches settings to it only after initialization succeeds and retains existing files.

## Design and page review

The [Studio Design System](docs/design-system.md) defines shared layout, theme, typography,
components, feedback, accessibility, and verification rules against the onevra v0.2026.9
component library on MyGo v0.4.8, including its collapsible, resizable sidebar shell and
component mapping.
The [page audit](docs/design-system-audit.md) records implementation gaps across all five
pages, the run detail view, and the offline report window. These documents define the target and
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

| Platform | Native windows | Offline report windows |
| --- | --- | --- |
| macOS 12+ | Metal | System WKWebView |
| Windows 10/11 | Direct3D 11 (WARP fallback) | Microsoft WebView2 Runtime; included with Windows 11, install on Windows 10 if absent |
| Linux | GTK 3 and OpenGL; MyGo can fall back to CPU rendering | WebKitGTK 4.1 (4.0 fallback) |

Native controls do not use WebView2 or WebKitGTK. Studio still needs the system WebView
for HTML reports, including WebKitGTK on Linux. Debian/Ubuntu provide it as
`libwebkit2gtk-4.1-0`. These requirements follow the pinned MyGo v0.4.8
[getting-started guide](https://github.com/egoist/mygo/blob/v0.4.8/docs/getting-started.md),
[rendering guide](https://github.com/egoist/mygo/blob/v0.4.8/docs/ui/rendering.md), and
[architecture](https://github.com/egoist/mygo/blob/v0.4.8/docs/architecture.md).

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

Native view tests and fixtures do not establish real-device acceptance. Verify all five
pages, the Dashboard's stage navigation and restored progress, resource curves, and an
offline report WebView on actual desktop windows. Cross-compilation is not Windows/Linux runtime evidence.
The native UI migration requires fresh acceptance; historical Web UI checks do not qualify
it. Original diagnostic artifacts under `docs/evidence/` are retained as historical evidence,
not as current build instructions. Signing, notarization, publication, and the full real
model workflow require separate verification.

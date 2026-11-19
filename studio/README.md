# InferYard Studio

InferYard Studio evaluates local models through a native Go UI using
[onevra](https://github.com/oneo-me/onevra) (MIT) and MyGo v0.7.4.
Pages call `core.Service` and refresh through in-process events.
Its [CLI subproject](../cli/README.md) executes benchmark operations and owns evidence.
There is no browser UI, headless mode, REST/SSE server, or generated JavaScript.

## Pages and first launch

When the workspace, results, and model paths are all unset in settings, Studio opens
on the task page with environment preparation. Once any of these paths has been saved,
launching without an explicit page opens Reports. The sidebar lists Tasks, Reports,
Models, Downloads, Device, and Settings.
For a first benchmark:

1. Open Tasks and choose **准备环境** (Prepare environment). Studio checks the CLI and workspace. If assets are missing, open Downloads to prepare a model and a compatible engine.
2. On Tasks, select the model and engine. Keep both default task sets for a standard benchmark and choose **开始测评** (Start benchmark). Studio can start its own local service or bind a service you started. The CLI checks the live identity before running.
3. Open **查看报告** (View report) after the runs finish. The report shows each run separately. To check evidence integrity later, use the CLI on the [original run directories](../cli/docs/guides/onboarding-and-scope.md#默认测评范围).

Choose **任务** to return to the current task with its progress and selections. **新任务** starts a new round and asks for confirmation when work is in flight.

| Page | Purpose |
| --- | --- |
| Task page | Prepare the environment with one action (reusing the configured CLI when available), choose workspace evaluation sets and a model/engine pair, then create evaluation configurations, start and bind the service, run, and review the completion summary or open the report. Inspect per-case progress, view CPU, memory, and available temperature curves, and resume eligible runs opened from Reports. |
| Reports | Browse compact task history, enter or resume a run, inspect live details, and open model-led report cards inside the main window. Reports are display artifacts with no verification entry. |
| Models | Read-only inventory of installed models and engines (format, architecture, size, verification verdict) plus running services. Choosing the pair happens only on the task page. Empty inventories offer Go to Downloads; orphaned engines show a warning; no absolute paths. |
| Downloads | Inspect, cancel, and retry download tasks (with their actual directory names), and create new model/engine downloads into settings-configured directory names. Platform-filtered resource rows show pinned engines and models beside their download controls. Reference resources stay collapsed with their preparation instructions. |
| Device | Inspect and refresh the hardware snapshot with inline missing-measurement reasons. |
| Settings | Configure the CLI path, workspace, results directory, model directory, and registered local installation declarations — the only page that shows full paths — plus the model/engine download directory names. Each path field keeps manual entry and adds `Browse…` to open the native file or directory picker for an existing target; a workspace that does not exist yet is typed by hand, and saving never creates directories. |

The [pages reference](docs/pages.md) covers the per-page rules: the first-launch flow,
preparation, evaluation-set selection, engine and model binding, local installation
registration, service discovery, download directory naming, and workspace initialization.

## Design and page review

The [Studio Design System](docs/design-system.md) defines shared layout, theme, typography,
components, feedback, accessibility, and verification rules against the onevra v0.2026.17
component library on MyGo v0.7.4, including its collapsible, resizable sidebar shell and
component mapping.
The [page audit](docs/design-system-audit.md) records implementation gaps across all six
pages, the run detail view, and the report view. These documents define the target and
review scope; they do not establish native desktop acceptance.

## Build and run

For paired development from the current checkout, run `mise run dev` from the
repository root. Use `mise run dev:rebuild` for fresh development settings and a new
workspace while retaining previous evidence and model/engine assets. The
[development commands](../tools/README.md) describe startup, shutdown, isolation, and
verification. On macOS, the launcher creates a separate `InferYard Studio Dev.app`.
This real CLI workflow is separate from the simulated UI preview below.

Use the repository's mise-managed Go 1.27.3. Run these commands from `studio/`:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -o build/inferyard-studio .
./build/inferyard-studio
```

On Windows, build with `-o build/inferyard-studio.exe` and launch that executable.
Development also works with `mise exec -- go run .`. The app uses a plain Go build with
no frontend installation or build step. The environment overrides above apply only
to the command, not to global configuration.

For source development, prepare the CLI with `mise exec -- uv sync --frozen` in `cli/`,
then set Studio's CLI path to the absolute path of `cli/.venv/bin/inferyard`
(`cli/.venv/Scripts/inferyard.exe` on Windows). An installed CLI can report the same
version while lacking newer source features. Specialist tasks require engine
verification and the context-speed result schema; an older CLI shows a settings action
instead of claiming that an unverified engine is ready.

To cross-build macOS arm64, Windows amd64, and Linux amd64 for local development and
assemble unsigned distributions:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct sh scripts/build-all.sh
```

[build-all.sh](scripts/build-all.sh) compiles the three binaries into `build/<os>-<arch>/`
and invokes [package.py](scripts/package.py) with mise-managed Python to assemble new
`dist/inferyard-studio-v<version>-<os>-<arch>/` directories. Packaging copies the binaries,
MIT LICENSE, and launch instructions, and refuses to overwrite an existing distribution.
It does not sign, notarize, or publish. On macOS, launch the packaged app with
`open "InferYard Studio.app"` to use desktop notifications.

Formal release packages are built and uploaded by
[Studio release candidate](../.github/workflows/studio-release.yml), not by this local
cross-build. Its macOS, Windows, and Linux jobs each build natively with `-tags release`
and call `package.py --target TARGET --source-commit SHA --out NEW_DIR`. The workflow
checks build provenance and generates a three-platform manifest and
`SHA256SUMS.studio`:

- **macOS:** `.dmg` disk image containing `InferYard Studio.app` and an `/Applications` shortcut.
- **Windows:** Inno Setup `.exe` installer with per-user installation by default and an uninstaller.
- **Linux:** `.tar.gz` archive.

The macOS job mounts the disk image before testing the packaged app; the Windows job
installs the application, checks its command boundary, and uninstalls it. The unified
[publish workflow](../.github/workflows/publish.yml) verifies both CLI and Studio
candidates before creating a GitHub draft with all nine attachments.

The version's single source is [mygo.config.ts](mygo.config.ts). It carries only the MyGo
embed metadata and is not a web frontend build configuration. Go embeds the file directly,
so an ordinary rebuild picks up version changes without generated files or linker flags.
The packaging script reads the same version.

## UI debug preview

UI preview is available only in an explicitly tagged development/test build. From
`studio/`, open the native pages with simulated data:

```sh
mise exec -- go run -tags ui_debug . --debug-ui
```

To build first and launch the local executable:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -tags ui_debug -o build/inferyard-studio .
./build/inferyard-studio --debug-ui
```

Normal builds and packaged releases omit the switch, preview UI, and embedded fixtures.
Their help does not list `--debug-ui`, and passing it is an input error. The `release` tag
disables preview even when combined with `ui_debug`; `build-all.sh` always sets `release`,
and packaging rejects binaries without that tag before writing distributions.

The sidebar's “模拟场景” selector provides task preparation, running, completion, and failure;
live run details; report list, details, long tables, and read errors; empty data; models;
downloads; device measurements; settings; a specialist history fixture; and the five
local-engine preparation scenes. Pages use the same components as normal Studio. The
selector is a shortcut to set a starting state, while normal page buttons drive the workflow:

- **Standard benchmark flow:** Start with “准备环境”, select the model and evaluation sets,
  and click “开始测评”. Simulated startup, per-bundle progress, and report generation advance
  automatically. Open live details or switch pages while work continues; “查看报告” opens
  that task's model and run results.
- **Downloads, settings, and recovery:** Downloads can complete, be cancelled, or fail and be
  retried; completed assets appear in Models. Settings are saved in memory, `Browse…` writes a
  simulated path without opening a native dialog, and download directory names affect later
  tasks. “下次操作模拟失败” injects a one-shot failure for testing recovery. “新任务”, “取消运行”,
  “取消任务”, “停止服务”, and “取消下载” open the same inline confirmation as production before
  cancelling work, while retaining settings, downloads, and existing results. “重置场景”
  restores all fixture data, form values, table selection, and collapsed sections, and discards
  pending simulation steps.
- **Long-context speed flow:** The bundled `zh-context-speed` corpus carries its human approval
  of 2026-10-18 (`init` still refuses an unapproved or modified corpus with
  `human_corpus_review_required`). After preparing the environment, choose “长上下文速度” in
  “测评类型”, select 64K, 128K, or 256K, and click “开始专项测评”. The same native page builders
  show progress, cancellation, and six-column results: model / engine, Context, Prompt length,
  Time, Prefill, and Decode. Results and history show the recorded model and engine together,
  independent of the current selection; GPU model and memory are not shown in specialist
  results. “下次专项模拟缺测” makes the fifth sample lack timing; “下次操作模拟失败” applies to
  startup or opening results. Reading again does not rerun measurements. The Reports page keeps
  specialist history for this window only; “专项 · 历史恢复示例” shows a fixed history fixture,
  not evidence restored from disk. Production history reads task records and frozen batches,
  and opening a result invokes `speed-results`. See the
  [context-speed reference](../docs/subsystems/studio-panel/context-speed.md).

The preview window is labelled “UI 调试 · 模拟数据”. It does not create `core.Service`, read
user settings or workspace evidence, persist window layout, execute the CLI, start models,
download files, or save settings to disk. Progress, resource samples, and results are simulated.
Restart the command after changing source code; there is no hot reload. Previewing a native
window verifies its presentation, not the real model workflow. Implementation boundaries
are described in the [subsystem reference](../docs/subsystems/studio-panel.md#ui-调试预览).

## Platform dependencies

| Platform | Native windows |
| --- | --- |
| macOS 12+ | Metal |
| Windows 10/11 | Direct3D 11 (WARP fallback) |
| Linux | GTK 3 and OpenGL; MyGo can fall back to CPU rendering |

## Reports, services and recovery

Reports open in the main window. Select a run to inspect its completion, timing, memory and per-case rows. Missing measurements display as — with available reasons. An unreadable report stays visible for retry. The [report reference](../cli/docs/reports.md) describes the fields and how to locate raw answers.

Studio binds local endpoints and stops the services it starts on shutdown. When binding an existing service, its owner remains responsible for stopping it. Runs execute serially. Cancelling an active run waits for it to exit; resume is available only for eligible unfinished batches. Check the [service ownership rules](../docs/subsystems/studio-panel.md#约束与失败行为) and [queue and history states](../docs/subsystems/studio-panel/preview.md#关键状态转换) when diagnosing a blocked task.

Settings live under `InferYard Studio/settings.json` in the system user configuration directory. Reports and raw evidence can include private local paths. Keep them local or follow the CLI's [public package procedure](../cli/docs/guides/operations.md#公开包与扩展) when sharing redacted results. The [Studio subsystem reference](../docs/subsystems/studio-panel/responsibilities.md) identifies the components responsible for settings, downloads, evidence and notifications.

## Verification boundaries

To test the native preview window on macOS with Xcode, run `mise run test:studio-ui`
from the repository root for daily checks, or `mise run test:studio-ui:full` for
complete coverage. Each suite uses one `--debug-ui` window throughout. Daily checks
cover first-use downloads and a two-bundle report, context-speed results and history,
and resource charts. Full checks also cover cancellation, failures, retry and engine
binding. Logs, suite metadata, failure screenshots and the Xcode result stay in a new
`studio/build/ui-e2e/run-*` directory. See [native UI e2e](tests/ui/README.md) for
requirements and the coverage matrix. CI runs Full on macOS; Windows/Linux
native UI coverage is not claimed.

The release-candidate workflow runs a separate production package smoke on macOS.
`ReleaseUITests` launches the exact packaged `.app` without `--debug-ui`, injects
an isolated HOME, visits Reports, Models, Downloads, Device and Settings, and opens
the `studio.json` produced by the real CLI/fake-engine chain. Its runner, logs,
screenshots and Xcode result are retained separately from the preview evidence.
This checks the production package and report-reading path; it does not start a
real model or replace the Full preview suite. Windows/Linux release jobs verify
native compilation, packaging and the command-line boundary only, not native UI.

Keep checks that demonstrate product or logical invariants, or protect packaging
and release; follow the [test policy](../docs/testing.md). `go test` includes compilation
and its default vet checks; there is no separate broad vet gate.
From `studio/`, run:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
```

Also check the development preview and the release override:

```sh
mise exec -- go test -tags ui_debug . ./internal/pages
mise exec -- go test -tags ui_debug,release .
mise exec -- python -m unittest discover -s scripts -p 'test_*.py'
mise exec -- python -m unittest discover -s tests/ui -p 'test_*.py'
```

The tagged page tests include `TestPreviewBenchmarkParity`: shared scenarios compare
the production benchmark orchestration with scripted service responses against the
preview simulator. Both must satisfy explicit expectations for stages, available
controls, serial queue states, cancellation, startup retry, and report scope. These
tests start with prepared candidates and do not establish parity for real CLI events,
downloads, or every error path. See the [preview test boundaries](../docs/subsystems/studio-panel.md#ui-调试预览).

`go test ./...` includes the boundary e2e suite under `tests/e2e`: the real CLI executes
every operation against a fake engine (the test binary re-executing itself), each CLI call
is recorded to a blame transcript, and the chain proves the cross-process command sequence
and the `studio.json` report contract that Studio consumes. The CLI's own evidence
journals and ledgers stay covered by the CLI test suite. The e2e suite attributes every
failure to the CLI side or the Studio side; see [tests/e2e/README.md](tests/e2e/README.md)
for the layout, fixture boundaries, and the `INFERYARD_E2E_CLI` / `INFERYARD_E2E_ROOT`
knobs. CI runs it on macOS and Windows via `.github/workflows/studio-check.yml`.

Native view tests, the e2e suite, and fixtures do not establish real-device acceptance. Verify all six
pages, the task page's stage navigation and restored progress, resource curves, and the
report index view on actual desktop windows. Cross-compilation is not Windows/Linux runtime evidence.
The native UI migration requires fresh acceptance; historical Web UI checks do not qualify
it. Original diagnostic artifacts under `docs/evidence/` are retained as historical evidence,
not as current build instructions. Signing, notarization, publication, and the full real
model workflow require separate verification.

# Studio pages and first launch

This reference describes each Studio page, the first-launch flow, and the rules the pages
enforce. The [Studio README](../README.md) keeps the product entry point and the build,
preview and verification rules.


When the workspace, results, and model paths are all unset in settings, Studio opens
on the task page with environment preparation. Once any of these paths has been saved,
launching without an explicit page opens Reports. The sidebar lists Reports, Models,
Downloads, Device, and Settings.
“新任务” (New task) opens the task page with no sidebar selection.

| Page | Purpose |
| --- | --- |
| Task page | Prepare the environment with one action, choose workspace bundles and a model/engine pair, then create candidates, start and bind the service, run, and generate a task report. Inspect case progress, view CPU/memory curves, and resume eligible runs opened from Reports. |
| Device | Inspect and refresh the hardware snapshot with inline missing-measurement reasons. |
| Models | Select an engine, then a model, and open a prefilled task through the same entry as New task. Active services or runs show the pair as text. Empty registries offer only Go to Downloads. Service cards are read-only; orphaned engines show a warning and hide the new-task action. No absolute paths. |
| Downloads | Inspect, cancel, and retry download tasks (with their actual directory names), and create new model/engine downloads into settings-configured directory names. Platform-filtered resource rows show pinned engines and models beside their download controls. Reference resources stay collapsed with their preparation instructions. |
| Reports | Browse historical tasks, enter or resume a run, inspect live details, verify the workspace report, and read each report index inside the main window. |
| Settings | Configure the CLI path, workspace, results directory, and model directory — the only page that shows full paths — plus the model/engine download directory names and registered local installation declarations. |

The task page keeps the active task's selected bundles and new run paths in memory. New task
cancels binding and all non-terminal runs, waits for them to finish, and returns to the
shared engine/model selection without cancelling downloads or deleting evidence. The form
lists only workspace bundles and checks both `zh-core` and `zh-svg-pelican` when present.
Unchecking a bundle explicitly narrows the scope; unchecked bundles do not block the task.
Reports starts with task history, followed by the report list. Creating another report from
1–100 historical runs is available in a collapsed section. Verification appears only when
workspace `next_action` is `verify`; a `verified` report shows its verified status instead.
Without an active task, the page restores workspace progress from `progress.v1`.

Prepare environment rechecks readiness after installing the CLI, provisioning missing
model/results directories, and initializing a new workspace. Existing initialized workspaces
skip `init`; new workspaces omit `--bundle`. Missing models or engines lead to a resource
guide with Downloads and Settings actions; other blockers remain visible.
During preparation, Downloads places resource forms first and offers Return to task.
For the original binary-engine workflow, once resources exist, the first workspace device
check runs once with `--models-root`
and no `--model`. A sole available model is selected automatically; multiple models
require a choice. Engines still require verification. Once a model appears, the old
`no_local_model` warning clears; candidate preparation rechecks the model with `--model`.
The task generates `report --runs` for its own sealed runs with verification disabled, displays
only a “查看报告” button that opens that exact report, and leaves verification on Reports.
Duration and unfinished batch plans display their blocking reason without an execution button.

Top-right engine and task icons pulse while active, respecting Reduce Motion.
Hover, click, or press Enter to open their panels; Escape dismisses them.
The engine panel shows status and start/stop controls on every page. Starting requires
a prepared candidate and only starts the service; it does not bind or run a benchmark.
The task panel separates executing and waiting tasks. Active downloads have a separate
icon and count in their panel. Background refresh does not change activity.
Active bundles show live state; interrupted bundles retain recovery guidance.
The service card stays collapsed throughout an active benchmark, then exposes
recovery controls in the run/resume stage. Stop is available for a starting or running engine
when no non-terminal run exists and Studio is neither busy nor resetting a task.

New macOS/Linux single runs collect CPU and available temperature sensors through the CLI.
Resource charts keep sources separate and retain 600 observations per source. CPU shows
single-core equivalent usage; temperature has a source selector. Elapsed-time labels use
equally spaced samples, with UTC and original units in details and a paged raw table.
Missing values remain gaps with reasons. Older memory-only runs are not backfilled.

When no CLI is configured, the task page’s Prepare environment action installs the CLI: it reuses an existing
`inferyard`/`uv` when found (including Homebrew locations outside the GUI PATH), downloads
a pinned standalone `uv` into the Studio tools directory when none exists — verified
against the release checksum, without touching shell configuration — and installs
`inferyard` matching the Studio version from PyPI via `uv tool install`. Manual path
configuration in Settings remains available.

Run benchmark creates fresh candidates for the checked bundles. Direct services start and
bind automatically; LM Studio requires an operator-provided service identity. Binding has
its own cancellation context. The original binary-engine workflow offers binding retry
and an advanced manual binding fold. Existing single-config runs rebind the new PID before
running; eligible resumes pass the started engine's PID and endpoint directly to `resume`.
Studio stops its managed engine on exit and never restarts it automatically.

Register an existing local installation declaration in Settings, then select its engine and
model on the task page. llama-cpp and LM Studio use GGUF files; MLX, vLLM and SGLang use
model directories. Local profiles currently target macOS Metal and do not support the
context-speed specialist. Each selected bundle uses a fresh direct service, or waits for
a newly replaced LM Studio service. Studio never stops or restarts an existing LM daemon.
A failure stops the remaining scopes; manual retry keeps completed runs and runs only the
unfinished bundles. The final report contains this task's successful runs. See the
[local-engine reference](../../docs/subsystems/studio-panel/local-engines.md) for registration,
CLI query versions, process ownership and verification boundaries.

The task page and Models show Studio-started services from `EngineState`, independently of
service discovery. Starting, running, and stopping services remain visible when scanning
fails or the model directory is unavailable. Leftover records are labelled unconfirmed;
failed and stopped services are not shown as active. Candidate names come from the service
state, not the current model selection.

Other endpoints appear under “待确认的兼容接口” (Unconfirmed compatible interfaces).
They may be proxies; a model directory does not prove that a model is loaded locally.
Only the task page offers external binding; Models remains read-only. Binding always
uses CLI PID and start-time checks, with rejection reasons preserved. See the
[service discovery contract](../../cli/docs/cli-surface.md#本机服务发现契约).

Studio records the identity of services it starts. After an abnormal exit, it checks
that identity before offering to stop a leftover service. Uncertain identities block
startup; the task page provides recovery actions with separate safeguards for local
process trees. Credentials use environment-variable references. Recovery and display
rules live in the [Studio subsystem reference](../../docs/subsystems/studio-panel.md#约束与失败行为).

The Downloads page puts platform-filtered recommendations beside the download controls,
with engines first and models second. Each resource row shows its name, platform, size,
and selection state. Only assets matching the running OS and CPU architecture, plus
platform-independent assets, appear. GGUF models are shared across platforms; the MLX
directory-model reference appears only on macOS arm64. Matching a platform does not
establish hardware eligibility, which is checked before a benchmark.

The form URL is the selection's single source of truth. Defaults fill empty fields once;
clicking a resource replaces its URL. Editing an address clears the matching selection,
and rendering never overwrites an existing custom address. Custom addresses, fixed revisions,
and SHA256 details are available in collapsed sections. A manually entered catalogue URL
for another platform shows a warning. Unknown platforms never default to another platform's
engine. Model credentials accept only an environment-variable name under “需要凭据时”.

“其他准备方式” lists matching reference resources with their address, unsupported-download
reason, and preparation document. It starts collapsed and has no download action.
Fixed catalogue addresses and the download channel validators remain authoritative: the
engine channel downloads and unpacks llama.cpp archives, preserving sibling libraries and
only in-archive relative symlinks; the model channel accepts a single GGUF at a fixed revision.

Empty pages omit the task card; existing tasks list engines before models. Download directory
names come from Settings (defaults `model` / `engine`); occupied names gain a `-2`/`-3` suffix.
Task details show the actual directory. Allocation stops after 1,000 occupied candidates with
`output_name_exhausted`; new binding directories use the same limit. Existing files are never
overwritten.

The initial effective workspace is `~/.inferyard`. Preparing directories is an explicit,
idempotent action; saving settings alone does not create them. CLI `init` requires a new
directory. For an existing uninitialized workspace, choose a new child directory; Studio
switches settings to it only after initialization succeeds and retains existing files.

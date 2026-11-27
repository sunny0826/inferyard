# Changelog

## 0.0.20

- feat(cli,studio): add PolyStrata thinking config and fix worker RSS tracking (#264)
- feat(cli,studio): polish run detail and report UI and add case Q&A inspection (#263)
- feat(studio): managed one-click engines, live tray status, and icon buttons (#261)
- feat(studio): unify task steps and live progress with PipelineRail (#262)
- fix(studio): replace wrong engine icons and optimize model brand icons (#260)
- fix(cli,studio): avoid false incompatible artifact banner during active benchmark runs (#258)
- feat(studio): refresh design system palettes, accent picker, optical icon buttons, and tiered notices (#259)
- ci: split fast PR lint/compile gates from heavy Nightly CI (#257)

## 0.0.19

- fix(studio): resolve 12 UI/UX issues identified via native UI E2E and visual audit (#255)
- fix(release,studio): publish PyPI before Studio release and retry auto CLI upgrade (#254)
- fix(studio): anchor update prompt and notification overlays with absolute bounds (#253)

## 0.0.18

- feat(studio): unify design-system sizing and polish Library, Benchmarks, and Run History UX (#251)
- refactor: decouple Studio UI/preview hotspots and share dev assets across worktrees (#250)
- perf(polystrata): speed up Studio service and multi-bundle startup (#249)
- fix(studio): discover versioned PolyStrata installations in local E2E (#248)
- perf(studio): slim UI refresh pipeline and cache CLI/workspace probes (#247)
- feat(studio): add workspace benchmark artifact cleanup action (#246)

## 0.0.17

- perf: eliminate model hashing, pre-run probes/waits, and cold service restarts (#244)
- perf(studio): speed up task detail and report loading (#243)
- refactor: remove remaining anti-tamper locks across power, model hashing, community, and lab paths (#242)
- refactor: remove anti-tamper runtime allowlists while preserving safety floors (#241)
- refactor: remove temperature stop thresholds and retain telemetry recording (#240)
- fix(studio): remove source dropdown from resource temperature chart (#239)
- fix: support PolyStrata v0.2.20-poly.5 layout and one-click updates (#238)

## 0.0.16

- fix(studio): support real PolyStrata lifecycle and add opt-in local E2E suites (#236)
- fix(studio): restore sidebar/library accessibility roles and sync native preview UI tests (#235)
- fix(studio): verify local engines on benchmark and service entry and sync service handoff (#234)
- perf(studio): optimize local model startup latency, concurrency, and error diagnostics (#233)
- feat(studio): automatic engine version checks and one-click updates (#232)
- feat(studio): serve update manifest via Cloudflare R2 (#231)

## 0.0.15

- feat(studio): replace benchmark ETA guesswork with backend elapsed time (#229)
- fix(studio): move notification bell to top-right header and polish popover (#228)
- feat(studio): trailing sidebar toggle and P4 evidence feedback, runtime memory, exclusivity checks, and engine updates (#227)
- perf(cli): eliminate local-engine re-verification overhead on macOS (#226)
- feat(studio): multi-service concurrency, port conflict checks, memory budget, and LAN access (P3) (#225)
- feat(studio): standalone service presets, keep-alive adoption, exit notices, and usage examples (P2) (#224)
- feat(studio): redesign sidebar, services home, library, and benchmarks list (P1) (#223)
- feat(studio): add About tab in Settings with build metadata (#222)
- feat: support standalone model serving from CLI and Studio Models page (#221)

## 0.0.14

- chore(studio): remove unused isExtraModelRoot helper and TASKS.md (#219)
- test(studio): align e2e, ui tests, and docs with unified workspace directory
- refactor(studio-ui): expose only workspace directory in settings and allow in-place init
- refactor(studio-core): unify workspace directory derivation and remove legacy path settings
- fix(polystrata): rebind cross-install worker configs and align macOS SMC safety ceiling (#218)

## 0.0.13

- docs: document animated SVG report showcase in Studio and READMEs
- feat(studio): render animated SVG outputs in report detail WebView
- feat(report): export and validate svg_outputs in studio report
- fix(studio,cli): use ~/.inferyard/workspace and ~/.inferyard/results as default directories (#216)
- refactor(studio): remove redundant card detail folds (#215)
- fix: eliminate macOS local-engine per-request stall and auto-recover replaced dirty services (#213)
- fix(studio): move notification close button to top-right and auto-dismiss popups (#214)
- fix(studio): format resource curve values to 2 decimals and readable timestamps (#212)

## 0.0.12

- feat(studio): auto-upgrade outdated CLI on update and show banner only on failure (#210)
- fix: support local-engine workspace progress and harden macOS PolyStrata worker tracking (#209)
- feat(studio): replace raw release notes with release changelog links (#208)
- feat(studio): add i18n support, refresh documentation, and add PR copy/docs workflow (#207)
- docs(release): allow agent to execute release directly without preview confirmation (#206)

## 0.0.11

- style(studio): unify concentric corner radii and 4px grid in shared tokens and components (#204)
- fix: unblock local engine runs for zh-core-v2 and cache model asset hashes (#203)
- feat(studio): add minimize to system tray setting (#202)
- chore(tests): prune redundant implementation tests and keep product hypothesis & release gates (#201)
- ci: optimize workflows for runner concurrency, caching, and Linux unit coverage (#200)
- feat: upgrade hardware-aware starter recommendation UX and CLI ranking (#198)
- fix(studio): require minimum uv version before reusing for CLI install (#199)
- refactor(studio): streamline PolyStrata install card UI and merge header cancel action (#197)

## 0.0.10

- fix(studio): deduplicate PolyStrata preflight errors, localize failure stages, and fix notification and action copy (#195)
- feat(studio): support safe uninstall and disk cleanup for managed PolyStrata installations and model packs (#194)
- fix(studio): deduplicate PolyStrata GGUF/config models, improve display names, and auto-select for benchmark (#193)
- feat(cli,studio): unify PolyStrata 4-stage progress and recovery actions (#192)
- feat(studio): redesign starter card and PolyStrata setup drawer (#191)
- fix(cli,studio): pin PolyStrata source integrity (SHA-256/size) and refine proxy/mirror routing (#190)
- fix(studio): validate CLI version, upgrade outdated CLI, and classify progress_version mismatch (#188)
- fix(cli): align PolyStrata disk estimation and surface lightweight alternatives (#189)
- fix(cli,studio): decouple PolyStrata venv from Python 3.15 and align Linux preflight (#186)

## 0.0.9

- feat(studio): add automatic update discovery and one-click in-app update (#177)
- feat(studio): add light/dark theme mode switcher in Settings (#176)
- refactor(studio): remove logo and title from sidebar (#175)

## 0.0.8

- feat(release): add fast PR-compliant release workflow and agent skill (#171)
- feat(studio): redesign engine status card with web UI link and unified header toggle (#170)
- refactor(studio): clean defensive and explanatory UI copy (#169)
- test(studio): update native preview XCUITest for PolyStrata starter recommendation (#168)
- feat(studio): wire hardware-aware starter recommendations, PolyStrata UI flows, previews, and docs (#167)
- feat(studio): integrate PolyStrata engine catalog, managed setup, lifecycle, and recommendation service (#166)
- feat(cli): add managed engine prepare-polystrata command with preflight and streaming progress (#165)
- feat(cli): add PolyStrata HTTP protocol adapter and service discovery support (#164)
- feat(cli): extend local-engine contracts, cross-platform installation, and hip backend schema for PolyStrata (#163)
- feat(cli): add hardware-aware starter engine and model recommendation (#155) (#162)
- feat(cli): enhance cross-platform hardware detection (#154) (#161)
- docs: add macOS quarantine removal step after Studio install (#153)

## 0.0.7

- Redesigned Studio UI onto a unified design system with semantic color tokens, surface materials, concentric radii, drawer overlays, unsaved-form guards, and an in-app toast and notification inbox.
- Unified CLI asset scanning, added a local engine setup wizard and acquire diagnostics, and upgraded benchmark execution with streaming download progress and task ergonomics.
- Added model URL resolution, mirror support, scoped download proxy settings, multi-root asset management, directory models, and multi-shard GGUF acquisition.
- Added an extensible multi-engine catalog and a redesigned Studio Downloads page.
- Introduced the new InferYard A3-2 brand identity across vector marks, multi-platform desktop icons, macOS runtime Dock icon, Studio sidebar header, and READMEs, and streamlined the macOS `.dmg` to contain only `InferYard Studio.app` (with bundled `AppIcon.icns` and `LICENSE`) and the `/Applications` shortcut.

## 0.0.6

- Streamlined Studio UI, lightweight editable dev mode (`mise run dev`), version-based identity without forced source/corpus/resource hash locks, and a single tag-triggered `release.yml` workflow.

## 0.0.5

### Added

- Native Browse... controls on Studio Settings path fields, including the local
  installation declaration. Picking writes only the draft, cancelling changes
  nothing, and a picker failure is reported next to that field so typing stays
  possible.
- A 任务 sidebar entry that only switches pages. Running work keeps showing its
  progress, an idle page keeps the prepared model, engine and evaluation set,
  and 新任务 keeps its confirm-and-reset behaviour.
- Inline confirmation for destructive actions: new task, cancel run, cancel
  task, stop service, cancel download and the orphan stop. Each prompt states
  what is cancelled and what stays, and re-reads its target before dispatch, so
  a finished run, stopped engine or completed download dispatches nothing.

### Changed

- Studio now ships a macOS `.dmg` containing the app and an `/Applications`
  shortcut, and a Windows `.exe` installer with per-user installation by default
  and an uninstaller. Linux remains a `.tar.gz` archive. Release CI mounts the
  DMG, installs and removes the Windows application, and checks the packaged
  command boundaries on their native runners.
- Studio reports lead with their recorded model and keep identifiers, paths and
  raw codes in a fold. Cards read 答案通过, 请求完成, 完成请求耗时（中位） and
  服务进程内存（采样峰值）; throughput labels state their threshold and
  sequential-batch scope, and output intervals keep saying that blocks are not
  tokens. Missing measurements keep a stated reason instead of showing zero.
- The Models page is a read-only inventory of installed models, installed
  engines and the running-service card. The duplicated picker, combination
  panel and new-task shortcut were removed, leaving the task page as the only
  selection entry.
- Studio reduces pass rates the way the CLI does: an execution failure stays in
  the denominator and an unscorable case withholds the rate with a reason, so a
  sealed plan with three passes and one failure reads 3 / 4 rather than 100%.
  A run counts as complete only when it stopped with `plan_finished`.
- Dropdowns keep the field name as their accessibility label and export the
  chosen option as the value, so a screen reader hears both.
- Internal: pruned tests that only pinned implementation shape, duplication or
  style, keeping product/logical hypotheses and packaging/release guarantees.
  The root, Studio and CLI READMEs were rewritten around the Studio-first
  workflow.

### Compatibility and evidence boundaries

- This release changes no CLI contract: the public envelope remains
  `schema_version = 3`, report and comparison formats, exit codes and metric
  IDs are unchanged, and existing `studio.json` reports stay readable.
- Studio archive formats changed for macOS and Windows, but no release artifact
  inherits the previous candidate validation. Signed and notarized Studio
  distribution, native Windows/Linux UI coverage, notification delivery and
  long-context real-model measurements remain unverified.

Earlier releases (`0.0.1` – `0.0.4`) are archived in [docs/changelog-archive.md](docs/changelog-archive.md).

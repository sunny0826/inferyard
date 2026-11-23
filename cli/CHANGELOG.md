# Changelog

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

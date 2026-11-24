# Contributing

Follow the guide for the subproject you are changing:

- CLI: [Contributing](cli/CONTRIBUTING.md).
- Studio: [README and checks](studio/README.md).

Shared repository rules are in [AGENTS.md](AGENTS.md).

## Copy (i18n) & Documentation Update Workflow

Every PR must check whether user-facing copy or documentation needs to be updated before opening or merging. Use the checklist in [.github/PULL_REQUEST_TEMPLATE.md](.github/PULL_REQUEST_TEMPLATE.md) together with the trigger map below.

### 1. Studio Copy (i18n) Rules

- Never hardcode user-facing Chinese or English strings in `studio/` production Go files (`studio/*.go`, `studio/internal/{core,desktop,pages}/*.go`).
- Add or update keys in the matching domain pair under `studio/internal/l10n/locales/<domain>/{en,zh-Hans}.json` (`common`, `settings`, `notifications`, `core`, `core_catalog`, `core_update`, `dashboard`, `speed`, `errors`, `errors_engines`, `downloads`, `models`, `preview`, `reports`).
- Follow Studio's copy conventions:
  - **Title Case** for short UI labels (buttons, nav items, tabs, card headings, table headers, badges); **Sentence case** for full sentences, descriptions, tooltips, errors, and notifications.
  - Keep copy concise and direct—do not add defensive disclaimers to the UI.
  - Keep `en.json` and `zh-Hans.json` in 100% key and `{placeholder}` parity, and resolve strings dynamically via `tr(c, ...)` / `trf(c, ...)` (in UI frames) or `l10n.TR(...)` / `l10n.TRf(...)` (outside UI frames) so live language switching works without restarting.
- Run `mise run test:studio` and `mise run test:studio:preview`. Automated tests in `studio/internal/l10n/l10n_test.go` enforce key/placeholder parity, valid ICU MessageFormat, zero CJK/fullwidth characters in `en.json`, and zero hardcoded CJK strings in `studio/` production Go files.

### 2. Documentation Trigger Map

Check and update the corresponding docs whenever a PR changes user-visible behavior:

| Change Area | Docs to Check & Update |
| --- | --- |
| New/changed headline features, supported platforms, or user quick-start flow | `README.md` and `README.zh-CN.md` (keep EN & ZH-CN in sync) |
| Studio pages, settings, i18n, themes, tray, updates, downloads, or `mise` Studio tasks | `studio/README.md` (and `README.md` / `README.zh-CN.md` if user-facing) |
| Studio native macOS UI E2E workflows or launch flags | `studio/tests/ui/README.md` |
| CLI commands, flags, PyPI install/upgrade, `installation.json`, or bundle presets | `cli/README.md`, `cli/README.zh-CN.md`, and `cli/docs/installation.md` |
| Exported schemas (`bundle`, `config_input`, `context_speed_result`, `engine_check`, `manifest`, `studio_report`) | Run `mise run schemas` (`export_schemas.py`) and check affected docs |
| Analysis methods, scoring metrics, or resource telemetry definitions | `cli/docs/reference/methods-matrix.md`, `cli/docs/experiments/metrics.md`, `cli/docs/experiments/metrics-resources.md` |
| Tooling, `mise.toml` tasks, or release process | `tools/README.md`, `cli/CONTRIBUTING.md`, `AGENTS.md` |

### 3. Pre-PR Verification Steps

1. Run the relevant test and lint tasks (`mise run test:unit`, `mise run lint`, `mise run test:studio`, `mise run test:studio:preview`, `mise run schemas`).
2. Verify that any updated Studio UI labels match the exact text in `en.json` (for English docs) and `zh-Hans.json` (for Chinese docs).
3. Verify that all relative links and anchors in edited Markdown files resolve to existing files.
4. Complete the **Copy (i18n) & Documentation Checklist** in the PR description before requesting review or merging.

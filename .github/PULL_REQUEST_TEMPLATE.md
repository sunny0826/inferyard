## Summary

<!-- Briefly describe what changed and why. -->

## Verification

<!-- List the commands you ran (e.g. `mise run test:unit`, `mise run lint`, `mise run test:studio`, `mise run test:studio:preview`). -->

- [ ] Relevant automated checks pass locally

## Copy (i18n) & Documentation Checklist

> Required for every PR — check each item that applies, or confirm no user-facing copy or docs are affected.

- [ ] **Studio i18n copy**: Every new or changed user-facing string in `studio/` uses `studio/internal/l10n` with matching keys and placeholders in both `en.json` and `zh-Hans.json` (no hardcoded Chinese/English UI literals), and `mise run test:studio` passes.
- [ ] **Copy review**: Checked capitalization (Title Case for short labels/buttons, Sentence case for sentences), canonical terminology, and concise wording without defensive UI disclaimers.
- [ ] **Documentation**: Checked whether user-facing docs need updates and updated all affected files (or confirmed N/A):
  - [ ] `README.md` & `README.zh-CN.md` (top-level features, quick start, platforms, links)
  - [ ] `studio/README.md` & `studio/tests/ui/README.md` (Studio pages, settings, i18n, UI workflows)
  - [ ] `cli/README.md`, `cli/README.zh-CN.md` & `cli/docs/installation.md` (CLI commands, installation, `installation.json`)
  - [ ] `cli/docs/reference/methods-matrix.md` & `cli/docs/experiments/metrics*.md` (methods, metrics, schemas)
  - [ ] No user-facing copy or documentation changes required for this PR

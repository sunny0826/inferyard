# Changelog

## Unreleased

### Removed

- Removed the `inferyard host-state migrate` command and the legacy host-state
  retirement mechanism (migration transactions, retirement markers and
  maintenance receipts).
- Legacy `local-ai-benchmark-host` lock/state files from the old tool are now
  completely ignored: they are never read, locked or retired, and InferYard no
  longer provides cross-tool mutual exclusion with the old tool.

### Changed

- `run` preflight no longer builds the unused device recommendation and hardware
  snapshot, no longer spawns `nvidia-smi` on non-CUDA backends, and no longer
  writes `device.preflight.json` or `input-provenance.json`. The identity record
  keeps only the preflight free-disk figure the report reads. Existing evidence
  is read unchanged.
- Live entry points now create a clean host state automatically when acquiring
  the host lock for the first time; no explicit initialization step is needed
  on new machines.
- v0.0.1 host states carrying a `migration` envelope are accepted and the
  envelope is ignored; it disappears on the next state write. Leftover
  `inferyard-host-migration.json` receipt files are treated as unrelated files
  and left untouched.
- README, AGENTS.md, CHANGELOG.md, CONTRIBUTING.md, MACOS.md and WINDOWS.md
  are now written in English; a Chinese README is available at
  `README.zh-CN.md`. All other docs stay Chinese for now.

## 0.0.1

First public distribution. The Python package and CLI are both named
`inferyard`, the license is MIT, and the active data contract remains
`schema_version = 3`.
See [GitHub Releases](https://github.com/sunny0826/inferyard/releases) for the
actual release state and assets.

### First use and legacy data

- Run `inferyard host-state migrate` before the first live operation to
  explicitly initialize the new host state or retire the fixed old entry
  point. Existing dirty state must go through the original recovery flow; it
  cannot be cleared by deleting locks or re-running the migration. (Removed in
  the Unreleased changes above — live entries now initialize automatically.)
- Only the [current format set](docs/contracts/inferyard-current-format.md) is
  accepted. Old runs, old report formats and `origin=migrated` are explicitly
  rejected; old evidence stays with the original project and is not converted
  in InferYard.
- Installation verification uses `installed_safe_checks.v3`, covering current
  report generation/verification and old-report rejection; old installation
  results inherit no qualification.

### Features

- Executes frozen task bundles, saving per-case answers, deterministic
  scoring, client-side timings and platform resource evidence.
- Supports single and batch plans, cancellation and resume, and explicit
  reruns; model services are started externally by the operator.
- Generates self-contained offline HTML reports, plus comparison, rescoring,
  export, redacted public packages and evidence verification.
- Ships task bundles, platform templates and fixed Windows runtime profiles;
  supports `init`, `config assets/create/bind` and `runtime prepare`. Models,
  engines, real configurations and original runs are not distributed with the
  package.
- Supports `uv tool` / `uvx` installation from the same batch of wheels and
  dependency constraints; Windows catalogues and metrics are explicitly read
  as UTF-8.

### Distribution and verification

- GitHub Actions builds wheels/sdists, rebuilds from the sdist and checks the
  resources, then runs installation checks on Linux x64, Windows x64 and
  macOS arm64.
- `community_distribution.v2` candidates bind source, build and installation
  results; after verification the same bytes are uploaded with `SHA256SUMS`.
- The publish workflow is manual-trigger only. `verify-only` runs a release
  rehearsal; `github-release` creates a draft with attachments.
  PyPI uses a separate Trusted Publisher configuration; this version has not
  been uploaded to PyPI.

Installation checks use synthetic inputs; native model preparation, full
bundles and strict performance qualification for the current version are not
proven by these checks.
See [platform status](docs/platforms.md) and the [backlog](docs/backlog.md)
for verified scope and remaining items.

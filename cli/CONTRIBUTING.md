# Contributing

Read the [product README](README.md) and [AGENTS.md](../AGENTS.md) first.
The project uses the [MIT license](LICENSE); release status is on
[PyPI](https://pypi.org/project/inferyard/) and
[GitHub Releases](https://github.com/sunny0826/inferyard/releases).

## Development environment

Versions are pinned by [mise.toml](../mise.toml): Python 3.15.2 and uv 0.13.6;
Go is used for the native Studio application. See [Studio](../studio/README.md) for its checks.
Dependencies are managed by uv; do not substitute system Python or global pip.
If an inherited `UV_PYTHON` points to another version, remove that override or
point it at the path returned by `mise which python`; confirm the interpreter
is actually 3.15.2 before syncing and running.

Run all CLI development commands below from the monorepo's `cli/` directory
(`cd cli` from the repository root). Root mise tasks already enter `cli/`.

```bash
mise which python
mise which uv
mise exec -- python --version
mise exec -- uv --version
mise exec -- uv sync --frozen
mise exec -- uv run --frozen inferyard --help
mise exec -- uv run --frozen inferyard --versions
```

The first sync needs network access; use `--offline` only when the cache is
complete. User installation and source development are separate: the
[installation guide](docs/installation.md) uses PyPI / wheels and
`uv tool` / `uvx`, while development commands use `mise exec -- uv run --frozen`.

## Changes and verification

Inspect the working-tree diff first and preserve existing changes. Keep tests
only when they demonstrate a product or logical invariant, or protect packaging,
installation and release.
Behavior fixes cover the real failure path; contract changes cover rejection
paths and old-data reading. Remove redundant assertions and implementation-shape
tests instead of preserving them under a new name.
Use fixtures and simulated services by default; real models, builds and
long-running workloads require separately agreed device, budget and stop
conditions.

Pick the checks that match the impact; not everything has to run every time:

```bash
mise exec -- uv run --frozen pytest -q tests/unit/test_RELEVANT.py
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen python scripts/export_schemas.py --check
mise exec -- uv run --frozen python scripts/export_catalogue.py --check
git diff --check
```

Replace `test_RELEVANT.py` with the relevant tests. Pure-doc changes only
check links, commands and the diff; HTML changes also check actual offline
rendering.
Leave the full pytest run for far-reaching code changes or integration
checks. Local TCP simulation tests need socket permissions; report sandbox
blocking and implementation failures separately.
Read the [scripts README](scripts/README.md) and check side effects before
running scripts; never batch-execute `verify_*`.

Schema sources live in `src/inferyard/contracts/`; run the exporter (without
`--check`) after changing them.
`docs/experiments/metrics.md`, `docs/experiments/metrics-resources.md` and
`docs/reference/methods-matrix.md` are the catalogue generation sources; re-export when they change, and never reassign
existing IDs for layout reasons. Bundles and their review materials keep the
original hash relationships; re-run manual review for affected cases after
changing them.

## Documentation and evidence

- README, CHANGELOG.md, CONTRIBUTING.md, MACOS.md, WINDOWS.md, Git
  commits and PRs use English; a Chinese README is available at
  [README.zh-CN.md](README.zh-CN.md). Repository agent rules live in
  [AGENTS.md](../AGENTS.md).
- Source code and exported schemas/catalogues are the source of truth; update
  user-facing README and installation docs only when user workflows change.
- Never commit session handovers, stage logs, real device configurations,
  models, engines, caches, credentials or original runs.
- `validation/` is an ignored local evidence directory, not used by default
  tests or builds. Extract minimal, publishable fixtures when regression
  samples are needed.
- Never overwrite original evidence; migration, rescoring and report rebuilds
  write to new directories.

## Local candidate build and installation checks

Run this on a clean final integration commit, after confirming that LICENSE
and the package metadata agree. Uppercase items below are placeholders:
`BUILD`, `INPUTS`, `CANDIDATE` and `STAGE` are new directories;
`OUTSIDE_CHECKOUT` must be a new directory outside the source checkout.
The parent directories of the last four must already exist. `INTEGRATED_SHA`
is the full commit SHA used for the build; do not switch sources in between.
`BUILD` picks a Git-ignored directory (such as `dist/...`) or a directory
outside the checkout, so generated artifacts do not fail the clean-source
check.

```bash
mise exec -- uv run --frozen python scripts/build_distribution.py --out BUILD
mise exec -- uv run --frozen python -m tests.packaging.prepare_inputs --out INPUTS
mise exec -- uv run --frozen python tests/packaging/run_installed.py --manifest BUILD/manifest.json --wheel WHEEL --constraints CONSTRAINTS --inputs INPUTS --out OUTSIDE_CHECKOUT
mise exec -- uv run --frozen python scripts/prepare_release_candidate.py --manifest BUILD/manifest.json --installed OUTSIDE_CHECKOUT/result.json --source-commit INTEGRATED_SHA --out CANDIDATE
mise exec -- uv run --frozen python scripts/verify_release_candidate.py --manifest CANDIDATE/manifest.json --manifest-sha256 PRODUCER_OUTPUT_SHA --source-commit INTEGRATED_SHA --stage STAGE
```

Use `BUILD/packages/inferyard-0.0.5-py3-none-any.whl` for `WHEEL` and
`BUILD/attachments/runtime-constraints.txt` for `CONSTRAINTS`. The build also
verifies the sdist rebuild and the constraint-check file.
`PRODUCER_OUTPUT_SHA` comes from the `manifest_sha256` field of
`prepare_release_candidate.py`'s JSON output.
Keep the build manifest, installation results and candidate originals; never
hand-edit JSON instead of passing the checks.

Installation checks use synthetic inputs and exercise uv tool/uvx, core
offline commands, workspace export and report generation in separate
environments, producing `installed_safe_checks.v4`; preparing Python and
dependencies the first time needs network access, and no model requests are
sent.
A `community_distribution.v2` candidate can be produced once at least one
platform completes; each additional physically tested platform is added with
a repeated `--installed RESULT` argument, and untested platforms keep
`not_verified`. This does not represent native model preparation or
performance acceptance.

Verification binds source, version, license, manifest and artifact digests;
staging uses the verified byte snapshot. `--packages-only` stages only the
wheel/sdist while still running the full candidate verification. None of the
commands above upload anything; the manual publish workflow defaults to
`verify-only` mode and requires both candidate runs, both candidate digests
and their shared source commit — see the
[scripts README](scripts/README.md#社区资源与发行检查).
Commits should state the changes, the checks actually run and the unverified
scope; create a local commit after the relevant checks pass, and do not push.

## Preparing a release with GitHub Actions

All formal release packages and uploads are produced by GitHub Actions. Local
candidate commands above are development checks, not a release-upload path.

1. Merge release docs and fixes through a PR first, freezing a clean final
   commit and version. Manually trigger
   [Package checks](https://github.com/sunny0826/inferyard/actions/workflows/package-check.yml)
   on the `main` commit. It runs the full regressions, build, three-platform
   installation and CLI candidate generation. Trigger
   [Studio checks](https://github.com/sunny0826/inferyard/actions/workflows/studio-check.yml)
   for the native preview and real-CLI/fake-engine boundaries, and
   [Model e2e](https://github.com/sunny0826/inferyard/actions/workflows/model-e2e.yml)
   when its separately approved real-model scope is required.
2. On the same source commit, manually trigger
   [Studio release candidate](https://github.com/sunny0826/inferyard/actions/workflows/studio-release.yml).
   Native macOS, Windows and Linux jobs build with `-tags release` and check the
   packaged command boundary. They produce a macOS DMG, a Windows Inno Setup
   installer and a Linux tar.gz. The macOS job mounts the DMG and launches its
   production app with an isolated HOME and a real CLI/fake-engine report. The
   Windows job installs the application, checks the installed command boundary
   and uninstalls it. The assemble job writes
   `studio-release-manifest.json` and `SHA256SUMS.studio`. A regular PR run of
   either candidate workflow cannot serve as a release source.
3. Inspect the two candidate manifests and generation output in Actions.
   Record the CLI `run_id` and `manifest_sha256`, the Studio `studio_run_id`
   and `studio_manifest_sha256`, and their shared full `source_commit`.
4. Trigger the
   [publish workflow](https://github.com/sunny0826/inferyard/actions/workflows/publish.yml)
   with a dispatch ref matching `source_commit`, passing all five values and
   first using `destination=verify-only`. The verifier checks both manual-run
   origins, both manifests, and the same-batch bytes without publishing.
5. After the rehearsal passes, use the same inputs with
   `destination=github-release`. The `verify` job checks both candidates and
   the release job creates the `v0.0.5` draft from those verified bytes with
   nine attachments: five CLI files, the Studio DMG, Windows installer and
   Linux tar.gz, and `SHA256SUMS.studio`. Edit the release notes on GitHub
   with features, compatibility boundaries, installation methods, actual
   platform coverage, sources and digests.
6. With explicit publication approval, use `destination=pypi` with the same
   inputs. The workflow publishes the verified wheel and sdist, then runs the
   `pypi-smoke` installation matrix on Linux, Windows and macOS. After those
   checks pass and the draft is reviewed, publish the draft on GitHub.

Do not rebuild and replace uploaded files after either candidate is prepared;
rebuild the candidates and their evidence instead when package content must
change. Actions artifacts have a retention period, so release records should
keep the sources and digests. The source tag must point at the candidates'
full `source_commit`; the publish workflow refuses if it points elsewhere.

`pypi` / `both` actually upload to PyPI; configure the project name and a
Trusted Publisher matching this repository's `publish.yml` and `pypi`
environment first. The GitHub draft preparation does not need these PyPI
settings.

## Per-case manual review

The optional `case_review_records` field only exempts cases with a matching
record from re-review. On first adoption every case must be reviewed
manually; a mismatching old whole-bundle approval cannot be auto-converted
into per-case approvals. The following command only prints a summary and
creates no approval:

```bash
mise exec -- uv run --frozen python - bundles/example.json <<'PY'
import sys
from pathlib import Path
from inferyard.evidence.storage import read_json
from inferyard.contracts.bundle import validate_bundle
from inferyard.contracts.bundle_review import case_content_hash
bundle = read_json(Path(sys.argv[1]))
validate_bundle(bundle)
for case in bundle['cases']:
    print(case['case_id'], case_content_hash(bundle, case))
PY
```

Replace the path with the bundle under review. After manually checking the
task, answer, rules, category, protocol and shared-answer policy, fill in the
actual digests and reviewer in a new array in that bundle, for example
(placeholder values must be replaced):

```json
{
  "case_review_records": [
    {
      "definition": "case-review.v1",
      "case_id": "instruction-01",
      "content_sha256": "replace with the 64-hex digest printed above",
      "reviewer": "actual reviewer",
      "reviewed_at": "2026-10-07",
      "conclusion": "approved"
    }
  ]
}
```

This is a new-field fragment, not a complete bundle. Every case not covered by
a matching old whole-bundle review needs a record. Re-review affected cases
after editing their content; changing a shared protocol or answer policy
affects all cases. When a matching per-case record exists, presentation-only
version and license-note changes do not require content re-review. Old
whole-bundle hashes and original files are preserved; old migration proofs
that no longer apply cannot be used for edited copies.

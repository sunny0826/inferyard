# Contributing

Read the [product README](README.md), the [architecture](docs/architecture.md)
and the relevant [topic contracts](docs/contracts/README.md) first.
File boundaries and safety requirements for agents are in [AGENTS.md](AGENTS.md).
The project uses the [MIT license](LICENSE); release status is on
[PyPI](https://pypi.org/project/inferyard/) and
[GitHub Releases](https://github.com/sunny0826/inferyard/releases).

## Development environment

Versions are pinned by [mise.toml](mise.toml): Python 3.14.7 and uv 0.12.18;
Go is used for the standalone observer.
Dependencies are managed by uv; do not substitute system Python or global pip.
If an inherited `UV_PYTHON` points to another version, remove that override or
point it at the path returned by `mise which python`; confirm the interpreter
is actually 3.14.7 before syncing and running.

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

Inspect the working-tree diff first and preserve existing changes. Behavior
fixes add regressions covering the real failure path; contract changes cover
rejection paths and old-data reading.
Use fixtures and simulated services by default; real models, builds and
long-running workloads require separately agreed device, budget and stop
conditions.

Pick the checks that match the impact; not everything has to run every time:

```bash
mise exec -- uv run --frozen pytest -q tests/unit/test_RELEVANT.py
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen ruff format --check src tests scripts
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
`docs/experiments/metrics.md` and `docs/reference/methods-matrix.md` are the
catalogue generation sources; re-export when they change, and never reassign
existing IDs for layout reasons. Bundles and their review materials keep the
original hash relationships; re-run manual review for affected cases after
changing them.

## Documentation and evidence

- README, AGENTS.md, CHANGELOG.md, CONTRIBUTING.md, MACOS.md, WINDOWS.md, Git
  commits and PRs use English; a Chinese README is available at
  [README.zh-CN.md](README.zh-CN.md). All other docs stay Chinese for now.
- The README is the product entry point; operational steps live in the
  installation and usage guides, interface rules in the data/topic contracts.
- ADRs record long-term design rationale; plans keep only unfinished work,
  and current progress is centralized in the [backlog](docs/backlog.md).
- Never commit session handovers, stage logs, real device configurations,
  models, engines, caches, credentials or original runs.
- `validation/` is an ignored local evidence directory, not used by default
  tests or builds. Extract minimal, publishable fixtures when regression
  samples are needed.
- Never overwrite original evidence; migration, rescoring and report rebuilds
  write to new directories, following the
  [evidence lineage rules](docs/data-contract.md#证据血缘与比较结论).

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

Use `BUILD/packages/inferyard-0.0.1-py3-none-any.whl` for `WHEEL` and
`BUILD/attachments/runtime-constraints.txt` for `CONSTRAINTS`. The build also
verifies the sdist rebuild and the constraint-check file.
`PRODUCER_OUTPUT_SHA` comes from the `manifest_sha256` field of
`prepare_release_candidate.py`'s JSON output.
Keep the build manifest, installation results and candidate originals; never
hand-edit JSON instead of passing the checks.

Installation checks use synthetic inputs and exercise uv tool/uvx, core
offline commands, workspace export and report/verify in separate
environments, producing `installed_safe_checks.v3`; preparing Python and
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
`verify-only` mode and requires the source package-check run, the source
commit and the candidate digest — see the
[scripts README](scripts/README.md#社区资源与发行检查).
See the [backlog](docs/backlog.md) for current candidate work and the
remaining pre-release items.
Commits should state the changes, the checks actually run and the unverified
scope; create a local commit after the relevant checks pass, and do not push.

## Preparing a release with GitHub Actions

1. Merge release docs and fixes through a PR first, freezing a clean final
   commit and version. Manually trigger
   [Package checks](https://github.com/sunny0826/inferyard/actions/workflows/package-check.yml)
   on the `main` commit. The workflow runs the full regressions, build,
   three-platform installation and candidate generation; a regular PR run
   cannot serve as a release source.
2. Download that `release-candidate` artifact and inspect `manifest.json`.
   Record the `manifest_sha256` from the generation output, the full
   `source_commit` and the source `run_id`; you can verify and stage locally
   with the commands from the previous section.
3. Trigger the
   [publish workflow](https://github.com/sunny0826/inferyard/actions/workflows/publish.yml)
   with a dispatch ref matching `source_commit`, passing those three values,
   first using `destination=verify-only`. The verifier checks the manual-run
   provenance, the manifest, the same-batch build/installation bytes, and
   generates the attachment `SHA256SUMS`.
4. After the rehearsal passes, use the same inputs with
   `destination=github-release`. The workflow verifies again and creates the
   `v0.0.1` draft with the wheel, sdist, runtime constraints, the constraint
   verification file and `SHA256SUMS`. Add the version's features, legacy-data
   handling, installation methods, actual platform coverage, sources and
   digests, then publish the draft separately.

Before publishing, re-download the draft attachments and confirm their
digests match the verified candidate. Do not rebuild and replace uploaded
files after the candidate is prepared; rebuild the candidate and installation
results instead when package content must change. Actions artifacts have a
retention period, so release records should keep the sources and digests.
The source tag must point at the candidate's full `source_commit`; the
publish workflow refuses if it already exists and points elsewhere.

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
from inferyard.config.bundle import validate_bundle
from inferyard.config.bundle_review import case_content_hash
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
that no longer apply cannot be used for edited copies — handling rules are in
the [data contract](docs/data-contract.md#按用途分派).

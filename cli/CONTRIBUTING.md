# Contributing

Read [AGENTS.md](../AGENTS.md) for repository structure, pinned `mise` commands, engineering principles, and delivery rules. Licensed under [MIT](LICENSE).

## Development environment

Run CLI commands from `cli/` using the `mise`-pinned Python and `uv` environment:

```bash
mise exec -- uv sync --frozen
mise exec -- uv run --frozen pytest -n auto -q
mise exec -- uv run --frozen ruff check src tests scripts
mise exec -- uv run --frozen python scripts/export_schemas.py --check
```

Re-run `scripts/export_schemas.py` (without `--check`) when changing schemas in `src/inferyard/contracts/`. Catalogues in `src/inferyard/data/methods.json` and `src/inferyard/data/metrics.json` and community resources in `src/inferyard/data/community/` are maintained directly in-package.

## Benchmark bundles

Modified or new benchmark bundles are validated structurally and semantically by `validate_bundle` (`inferyard.contracts.bundle`). Optional `review_records` remain accepted for compatibility with existing bundles.

## Local candidate build and installation checks

Run on a clean commit (`BUILD`, `INPUTS`, `CANDIDATE`, `STAGE` are fresh directories; `OUTSIDE_CHECKOUT` is a fresh directory outside the repo):

```bash
mise exec -- uv run --frozen python scripts/build_distribution.py --out BUILD
mise exec -- uv run --frozen python -m tests.packaging.prepare_inputs --out INPUTS
mise exec -- uv run --frozen python tests/packaging/run_installed.py \
  --manifest BUILD/manifest.json \
  --wheel BUILD/packages/inferyard-*.whl \
  --constraints BUILD/attachments/runtime-constraints.txt \
  --inputs INPUTS --out OUTSIDE_CHECKOUT
mise exec -- uv run --frozen python scripts/prepare_release_candidate.py \
  --manifest BUILD/manifest.json \
  --installed OUTSIDE_CHECKOUT/result.json \
  --source-commit INTEGRATED_SHA --out CANDIDATE
mise exec -- uv run --frozen python scripts/verify_release_candidate.py \
  --manifest CANDIDATE/manifest.json \
  --manifest-sha256 PRODUCER_OUTPUT_SHA \
  --source-commit INTEGRATED_SHA --stage STAGE
```

## Preparing a release with GitHub Actions

Formal releases are built and published exclusively via GitHub Actions on the target `main` commit (`SOURCE_COMMIT`):

1. Dispatch [package-check.yml](https://github.com/sunny0826/inferyard/actions/workflows/package-check.yml) and [studio-release.yml](https://github.com/sunny0826/inferyard/actions/workflows/studio-release.yml) on `SOURCE_COMMIT` and record both candidate run IDs and `manifest_sha256` digests.
2. Dispatch [publish.yml](https://github.com/sunny0826/inferyard/actions/workflows/publish.yml) at `SOURCE_COMMIT` with `destination=verify-only` first, then `destination=github-release` (creates draft) and `destination=pypi` (after explicit approval).

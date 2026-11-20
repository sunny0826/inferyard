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

Re-run `scripts/export_schemas.py` (without `--check`) when changing any of the 6 exported external/cross-process schemas (`bundle`, `config_input`, `context_speed_result`, `engine_check`, `manifest`, `studio_report`) in `src/inferyard/contracts/`. Catalogues in `src/inferyard/data/methods.json` and `src/inferyard/data/metrics.json` and community resources in `src/inferyard/data/community/` are maintained directly in-package.

## Benchmark bundles

Modified or new benchmark bundles are validated structurally and semantically by `validate_bundle` (`inferyard.contracts.bundle`). Optional `review_records` remain accepted for compatibility with existing bundles.

## Releases

Formal releases are built and published via [release.yml](https://github.com/sunny0826/inferyard/actions/workflows/release.yml) when pushing a version tag (`vX.Y.Z`) matching `cli/pyproject.toml` and `studio/mygo.config.ts` (or via manual `workflow_dispatch`):

1. Builds and smoke-tests the CLI wheel/sdist (`uv build`), and builds/packages Studio release binaries for macOS (`darwin-arm64`), Windows (`windows-amd64`), and Linux (`linux-amd64`).
2. Creates a Draft GitHub Release with all artifacts and `SHA256SUMS`.
3. Publishes the CLI wheel and sdist to PyPI via Trusted Publishing (`pypi` environment).

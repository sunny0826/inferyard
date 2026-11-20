# Studio e2e suite

Boundary-level end-to-end tests executing the real Python CLI against a fake engine (the Go test binary re-executing itself) in isolated temporary host/workspace directories.

From `studio/`:

```sh
mise exec -- go test ./tests/e2e/ -count=1 -timeout 900s -v
```

Environment variables:

- `INFERYARD_E2E_CLI`: explicit `inferyard` executable path (otherwise resolved from `PATH`, `cli/.venv`, or `uv run`).
- `INFERYARD_E2E_ROOT`: fresh directory to retain workspace, host state, and `transcript.jsonl` after the run.
- `STUDIO_SPEED_E2E_ROOT`: fresh directory to retain `TestContextSpeedPublicCLI` fixtures and output.

Tests use `t.Setenv` for isolated `PYTHONPATH` startup injection (`cli_bootstrap.py`) and must not use `t.Parallel`.

# Studio e2e suite

Boundary-level end-to-end tests executing the real Python CLI against a fake engine (the Go test binary re-executing itself) in isolated temporary host/workspace directories.

From `studio/`:

```sh
mise exec -- go test ./tests/e2e/ -count=1 -timeout 900s -v
```

Environment variables:

- `INFERYARD_E2E_CLI`: explicit `inferyard` executable path (otherwise resolved from `cli/.venv`, `PATH`, or `uv run`).
- `INFERYARD_E2E_ROOT`: fresh directory to retain workspace, host state, and `transcript.jsonl` after the run.
- `STUDIO_SPEED_E2E_ROOT`: fresh directory to retain `TestContextSpeedPublicCLI` fixtures and output.
- `INFERYARD_POLYSTRATA_E2E`: set to `1` (or run `mise run test:studio:e2e:polystrata` from the repo root) to execute `TestPolystrataRealLifecycle` against an existing local PolyStrata installation and its smallest-spec model without downloading weights. Assets are staged into an isolated temporary workspace (APFS Copy-on-Write clone on macOS) and verified to leave host `~/.inferyard` untouched.
- `INFERYARD_POLYSTRATA_INSTALLATION`: optional explicit path to `installation.json` for `TestPolystrataRealLifecycle` (defaults to `~/.inferyard/engines/polystrata/installation.json` or `~/PolyStrata/installation.json`).

Tests use `t.Setenv` for isolated `PYTHONPATH` startup injection (`cli_bootstrap.py`) and must not use `t.Parallel`.

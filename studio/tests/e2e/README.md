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
- `INFERYARD_POLYSTRATA_INSTALLATION`: optional explicit path to `installation.json` for `TestPolystrataRealLifecycle` (auto-discovers `~/.inferyard/engines/polystrata*/installation.json`, Studio `settings.json`, or `~/PolyStrata/installation.json`).
- `INFERYARD_BONSAI_E2E`: set to `1` (or run `mise run test:studio:e2e:bonsai` from the repo root) to execute `TestBonsaiRealLifecycle` against an existing local PrismML `llama.cpp` build (`>= b12232`) and local Bonsai 2.5 27B (`PTQ1_0` / `PQ2_0`) GGUF weights without downloading assets. Assets are staged via APFS Copy-on-Write into an isolated workspace and verified to leave host `~/.inferyard` untouched.
- `INFERYARD_BONSAI_ENGINE`: optional explicit PrismML `llama-cpp` directory or `llama-server` executable for `TestBonsaiRealLifecycle` (auto-discovers `~/.inferyard/engines` and Studio `settings.json`).
- `INFERYARD_BONSAI_MODEL`: optional explicit Bonsai 2.5 GGUF file or directory for `TestBonsaiRealLifecycle` (auto-discovers `~/.inferyard/models` and Studio `settings.json`, preferring the smallest file for execution).

Tests use `t.Setenv` for isolated `PYTHONPATH` startup injection (`cli_bootstrap.py`) and must not use `t.Parallel`.

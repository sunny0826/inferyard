# Checkout development tools

Run from the repository root with [mise.toml](../mise.toml):

```sh
mise run dev
mise run dev:rebuild
mise run dev:stop
```

## What it does

- **`mise run dev`**: syncs the editable CLI environment (`cli/.venv`), builds Studio (`go build`) into `.inferyard-dev/bin/`, initializes a branch-scoped workspace if needed, stops any previously recorded dev Studio, and launches Studio bound to `.inferyard-dev/`.
  - Edits to Python CLI code take effect immediately through the editable `cli/.venv` without rebuilding wheels.
  - Settings, window state, and workspaces live under `.inferyard-dev/profiles/<branch>/` so user release settings are never touched.
  - Downloaded models and engines live under `.inferyard-dev/assets/` and are shared across branches and rebuilds.
- **`mise run dev:rebuild`**: creates a fresh profile directory for the current branch while preserving previous profile history and shared `.inferyard-dev/assets/`.
- **`mise run dev:stop`**: sends a graceful shutdown signal to the recorded dev Studio process (which drains active work and stops its owned engine).
- **`mise run dev -- --prepare-only`**: syncs the CLI, builds Studio, verifies `--help`, and initializes the workspace without opening a window.

Development flags (`--dev-profile`, `--dev-assets`, `--dev-cli`, `--dev-label`, `--dev-status`) are excluded from `-tags release` builds.

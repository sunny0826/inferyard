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

## Shared dev assets

Downloaded models and engines are resolved in this order:

1. `mise run dev -- --assets <dir>` or a non-empty `INFERYARD_DEV_ASSETS=<dir>`, with the flag winning.
2. When running from a linked `git worktree`, the main checkout's `.inferyard-dev/assets`
   is reused automatically if it already exists as a real directory.
3. Otherwise this worktree's own `.inferyard-dev/assets`.

Symlinked assets roots, or symlinked `models` / `engines` subdirectories, are rejected.
Profiles, `launcher.lock`, and `session.json` always stay inside the current worktree's
`.inferyard-dev/`, so parallel worktrees keep isolated state. `--assets` cannot be
combined with `stop`.

## Fast release

```sh
mise run release patch              # bump versions + CHANGELOG, open/merge release PR, tag, watch CI
mise run release patch --dry-run    # preview version bump, files, and changelog without changes
mise run release 0.1.0 --prepare-only  # commit release prep on chore/release-v0.1.0 locally
```

Use `patch`, `minor`, `major`, or an explicit `X.Y.Z` version. Pass `--notes "..."` to override generated commit bullets, `--no-check` to skip local preflight checks, `--wait-pr-checks` to wait for PR workflows before merging, or `--no-watch` to return immediately after pushing the tag.

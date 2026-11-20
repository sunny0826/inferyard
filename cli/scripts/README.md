# CLI Engineering Scripts

Run all commands below from `cli/` (`mise exec -- uv run --frozen python scripts/<name>.py`). Check `--help` before running scripts that write directories or launch services.

## Exporters

```bash
mise exec -- uv run --frozen python scripts/export_schemas.py        # --check to verify the 6 exported boundary schemas
mise exec -- uv run --frozen python scripts/export_community.py      # optional helper to sync bundles or refresh Windows ZIP profiles (--archives DIR)
```

## Corpus & external patches

- `build_context_speed_bundle.py --out-dir NEW_DIR`: generate deterministic `zh-context-speed` draft materials.
- `patches/*.patch`: zero-context patches for external macOS SGLang/vLLM/OpenCV virtual environments (`git apply --unidiff-zero --check`).

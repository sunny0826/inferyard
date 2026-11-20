# CLI Engineering Scripts

Run all commands below from `cli/` (`mise exec -- uv run --frozen python scripts/<name>.py`). Check `--help` before running scripts that write directories or launch services.

## Exporters

```bash
mise exec -- uv run --frozen python scripts/export_schemas.py        # --check to verify
mise exec -- uv run --frozen python scripts/export_catalogue.py      # --check to verify
mise exec -- uv run --frozen python scripts/export_community.py      # pass --archives DIR when refreshing Windows ZIP profiles
```

## Distribution & release checks

- `build_distribution.py` / `check_distribution.py` / `export_runtime_constraints.py`: build wheel & sdist in a fresh directory, rebuild wheel from sdist, and verify package resources.
- `prepare_release_candidate.py` / `verify_release_candidate.py`: bind build manifests with out-of-tree installation results (`tests/packaging/run_installed.py`) without uploading. See [CONTRIBUTING.md](../CONTRIBUTING.md#local-candidate-build-and-installation-checks).

## Corpus & external patches

- `build_context_speed_bundle.py --out-dir NEW_DIR`: generate deterministic `zh-context-speed` draft materials.
- `patches/*.patch`: zero-context patches for external macOS SGLang/vLLM/OpenCV virtual environments (`git apply --unidiff-zero --check`).

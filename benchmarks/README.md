# InferYard Benchmarks

This directory is the single source of truth for all benchmark bundles shipped with InferYard.
The CLI (`cli/`) and Studio (`studio/`) load bundle metadata and execution hints dynamically
from this catalog rather than hardcoding bundle IDs in source code.

## Layout

- `catalog.json`: Declares built-in bundle names, file paths, default `init` selection,
  display metadata, and default `execution_hints`.
- `bundles/*.json`: Individual benchmark bundle documents conforming to
  `cli/schemas/bundle.schema.json`.

## Testing a New Bundle Locally

Before adding a bundle to `benchmarks/bundles/`, you can validate and test any standalone JSON file:

```bash
inferyard bundles verify /path/to/my-bundle.json
inferyard init --out ./ws-test --bundle /path/to/my-bundle.json
```

Standalone bundles can also declare optional `metadata` and `execution_hints` fields directly
inside the bundle JSON.

## Adding or Updating a Built-in Bundle

1. Add or edit `benchmarks/bundles/<name>.json`.
2. Register (or update) its entry in `benchmarks/catalog.json`.
3. Run `mise run check:benchmarks` (or `inferyard bundles verify`) to verify schema, semantics,
   and catalog consistency.

During Python packaging (`wheel` and `sdist`), `cli/hatch_build.py` bundles this directory into
`inferyard/data/benchmarks` so installed packages remain self-contained and offline-ready.

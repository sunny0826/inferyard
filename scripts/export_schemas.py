"""Export the exact runtime contract as portable JSON Schema, or check for drift."""

import argparse
import json
from pathlib import Path

from inferyard.contracts.schemas import export_schema, schemas_for


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1] / "schemas"
    stale = []
    expected_paths = {root / f"{name}.schema.json" for name in schemas_for()}
    obsolete = set(root.rglob("*.schema.json")) - expected_paths
    if args.check:
        stale.extend(str(path.relative_to(root)) for path in sorted(obsolete))
    else:
        for path in sorted(obsolete):
            path.unlink()
        for path in sorted(root.iterdir()):
            if path.is_dir() and not any(path.iterdir()):
                path.rmdir()
    for name in sorted(schemas_for()):
        path = root / f"{name}.schema.json"
        expected = json.dumps(export_schema(name), ensure_ascii=False, indent=2) + "\n"
        if args.check:
            if not path.exists() or path.read_text(encoding="utf-8") != expected:
                stale.append(str(path.relative_to(root)))
        else:
            root.mkdir(exist_ok=True)
            path.write_text(expected, encoding="utf-8", newline="\n")
    if stale:
        raise SystemExit("stale schemas: " + ", ".join(stale))


if __name__ == "__main__":
    main()

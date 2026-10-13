"""Build synthetic journal inputs and editable identities; no host lock or requests."""

import argparse
import hashlib
import importlib.metadata
import json
import shutil
from pathlib import Path

from inferyard.analysis.scoring import scorer_hash
from inferyard.provenance import tool_source_hash
from tests.helpers import fixture_run


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.out.mkdir()
    run = fixture_run(args.out / "synthetic-runs")
    root = Path(__file__).parents[2] / "src/inferyard"
    resources = {
        p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in root.rglob("*")
        if p.is_file() and "__pycache__" not in p.parts
    }
    snapshot = {
        "tool_source_hash": tool_source_hash(),
        "scorer_hash": scorer_hash(),
        "resources": resources,
        "fixture_relative": run.relative_to(args.out).as_posix(),
        "synthetic": True,
        "pytest_version": importlib.metadata.version("pytest"),
    }
    # CI-only request harness reuses the established synthetic scenario, never source runtime code.
    test_root = Path(__file__).parents[1]
    target = args.out / "ci-tests/tests"
    for relative in (
        "__init__.py",
        "integration/test_runner.py",
        "host_state_helpers.py",
        "fixtures/config/valid.toml",
        "fixtures/contracts/bundle.valid.json",
    ):
        output = target / relative
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(test_root / relative, output)
    (args.out / "expected.json").write_text(json.dumps(snapshot, indent=2), encoding="utf-8")
    print(json.dumps({"out": str(args.out), "synthetic_run": str(run), "model_requests_sent": 0}))


if __name__ == "__main__":
    main()

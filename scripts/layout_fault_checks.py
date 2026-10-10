"""Run bounded mock-only producer fault regressions and retain all fresh receipts."""

import argparse
import json
import subprocess
import sys
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.tree, args.out = args.tree.resolve(), args.out.resolve()
    if not args.out.is_relative_to(Path("/tmp").resolve()):
        parser.error("--out must be a fresh /tmp directory")
    args.out.mkdir(parents=True, exist_ok=False)
    boot = (
        "import sys;sys.path[:0]=[sys.argv.pop(1),sys.argv.pop(1)];"
        "import pytest;raise SystemExit(pytest.main(sys.argv[1:]))"
    )
    argv = [
        sys.executable,
        "-I",
        "-B",
        "-c",
        boot,
        str(args.tree / "src"),
        str(args.tree),
        "-q",
        "tests/integration/test_layout_writer_faults.py",
        "tests/integration/test_trial_requests.py::test_duration_admission_rechecks_deadline_after_durable_preparation",
        "tests/unit/test_duration_send_admission.py",
        "tests/integration/test_duration_trial.py",
        "--basetemp",
        str(args.out / "cases"),
    ]
    done = subprocess.run(argv, cwd=args.tree, capture_output=True, timeout=120)
    (args.out / "stdout.txt").write_bytes(done.stdout)
    (args.out / "stderr.txt").write_bytes(done.stderr)
    receipts = sorted(
        str(path.relative_to(args.out))
        for pattern in ("fault-receipt.json", "runtime-receipt.json")
        for path in args.out.rglob(pattern)
    )
    (args.out / "process.json").write_text(
        json.dumps(
            {
                "argv": argv,
                "exit_code": done.returncode,
                "synthetic": True,
                "receipts": receipts,
                "boundary_mapping": {
                    "actual_cli": {
                        "tests": "test_layout_writer_faults",
                        "faults": [
                            "prepared_io",
                            "started",
                            "native_receipt",
                            "sampling",
                            "close",
                            "seal",
                            "crash",
                            "cancel",
                        ],
                    },
                    "runtime_only": {
                        "tests": [
                            "test_trial_requests",
                            "test_duration_send_admission",
                            "test_duration_trial",
                        ],
                        "faults": [
                            "prepared_after_deadline",
                            "no_send",
                            "terminal",
                            "drain",
                            "safety_stop",
                        ],
                        "entry": "actual TrialRequests/run_trial; no CLI mapping",
                    },
                },
                "not_covered": ["native acceptance", "real models"],
            },
            indent=2,
        )
    )
    return done.returncode


if __name__ == "__main__":
    raise SystemExit(main())

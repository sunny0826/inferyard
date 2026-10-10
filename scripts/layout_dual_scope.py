"""Two actual mock CLI scopes, one offline report and exactly nineteen evidence files."""

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

BOOT = (
    "import sys,runpy;sys.path[:0]=[sys.argv.pop(1),sys.argv.pop(1)];"
    "runpy.run_module('inferyard',run_name='__main__')"
)


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False))


def inventory(root):
    return {
        path.relative_to(root).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
        for path in sorted(root.rglob("*"))
        if path.is_file()
    }


def process(argv, out, *, cwd, expected=0):
    out.mkdir()
    done = subprocess.run(argv, cwd=cwd, capture_output=True, timeout=120)
    (out / "stdout.txt").write_bytes(done.stdout)
    (out / "stderr.txt").write_bytes(done.stderr)
    dump(out / "process.json", {"argv": argv, "exit_code": done.returncode, "expected": expected})
    if done.returncode != expected:
        raise ValueError("unexpected_process_exit:" + str(out))
    return done


def run(args):
    roots, receipts, snapshots = [], [], []
    for scope, requests, formal in (("zh-core", 125, 120), ("zh-svg-pelican", 6, 1)):
        output = args.out / scope
        argv = [
            sys.executable,
            "-I",
            "-B",
            str(args.tree / "scripts/layout_fixture_worker.py"),
            "--tree",
            str(args.tree),
            "--out",
            str(output),
            "--scope",
            scope,
        ]
        process(argv, args.out / (scope + "-process"), cwd=args.tree)
        receipt = json.loads((output / "receipt.json").read_text())
        root = Path(receipt["run"])
        if (
            receipt["layout_version"] != 2
            or receipt["requests_received"] != requests
            or receipt["counts"]["planned"] != formal
            or receipt["counts"]["completed"] != formal
            or receipt["terminal_states"] != ["completed"] * formal
            or len(inventory(root)) != 8
        ):
            raise ValueError("scope_body_counts_or_inventory_mismatch:" + scope)
        roots.append(root)
        receipts.append(receipt)
        snapshots.append(inventory(root))
    base = [sys.executable, "-I", "-B", "-c", BOOT, str(args.tree / "src"), str(args.tree)]
    for scope, root in zip(("core", "pelican"), roots, strict=True):
        process(
            [*base, "verify", "--path", str(root)], args.out / (scope + "-verify"), cwd=args.tree
        )
    report = args.out / "report"
    process(
        [*base, "report", "--runs", *map(str, roots), "--out", str(report)],
        args.out / "report-write",
        cwd=args.tree,
    )
    process(
        [*base, "verify", "--path", str(report), "--rerender"],
        args.out / "report-verify",
        cwd=args.tree,
    )
    report_files = inventory(report)
    if set(report_files) != {"index.json", "report.html", "artifact-manifest.json"}:
        raise ValueError("report_inventory_not_three")
    after = [inventory(root) for root in roots]
    if after != snapshots:
        raise ValueError("original_run_changed_by_offline_commands")
    index = json.loads((report / "index.json").read_text())
    if [len(run["requests"]) for run in index["runs"]] != [120, 1]:
        raise ValueError("combined_report_case_count_mismatch")
    dump(
        args.out / "receipt.json",
        {
            "synthetic": True,
            "transport": "httpx.MockTransport; no TCP or real model",
            "preflight": "injected fixture; not native qualification",
            "tree": str(args.tree),
            "scope_receipts": receipts,
            "run_files_before": snapshots,
            "run_files_after": after,
            "report": str(report),
            "report_files": report_files,
            "evidence_file_count": sum(len(item) for item in snapshots) + len(report_files),
            "not_covered": [
                "native platform acceptance",
                "real models",
                "formal cost measurements",
            ],
        },
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    args.tree, args.out = args.tree.resolve(), args.out.resolve()
    if not args.out.is_relative_to(Path("/tmp").resolve()):
        parser.error("--out must be a fresh /tmp directory")
    args.out.mkdir(parents=True, exist_ok=False)
    try:
        run(args)
    except Exception as exc:
        dump(args.out / "failure.json", {"type": type(exc).__name__, "reason": str(exc)})
        raise
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

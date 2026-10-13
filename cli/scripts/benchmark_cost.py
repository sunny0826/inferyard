"""Synthetic software costs: prepare, baseline-only, then fresh interleaved comparison.

Run with the shared pinned venv: mise exec -- uv run --frozen python scripts/benchmark_cost.py.
All --out directories must be new and under /tmp. No services, models, builds, or original evidence.
"""

import argparse
import json
import os
import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path

from benchmark_cost_compare import check_expected, compare_outputs, counts, digest, inventory

WORKER = Path(__file__).with_name("benchmark_cost_worker.py")
OPERATIONS = (
    "read_trial",
    "report",
    "verify",
    "rerender",
    "verify_multirun",
    "rerender_multirun",
    "resources",
    "redact",
    "redact_empty",
    "json",
    "redact_stream",
    "redact_stream_empty",
)


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False))


def source(tree):
    files = {
        p.relative_to(tree).as_posix(): digest(p)
        for p in sorted((tree / "src").rglob("*"))
        if p.is_file() and "__pycache__" not in p.parts
    }
    for name in (
        "tests/helpers.py",
        "tests/fixtures/config/valid.toml",
        "tests/fixtures/contracts/bundle.valid.json",
    ):
        files[name] = digest(tree / name)
    commit = subprocess.run(
        ["git", "-C", str(tree), "rev-parse", "HEAD"], capture_output=True, text=True, check=True
    ).stdout.strip()
    return {"tree": str(tree), "commit": commit, "files": files}


def run_child(tree, operation, out, corpus=None, expected=0):
    if out.exists():
        raise FileExistsError(out)
    args = [
        sys.executable,
        "-I",
        "-B",
        str(WORKER),
        "--tree",
        str(tree),
        "--out",
        str(out),
        operation,
    ]
    if corpus is not None:
        args += ["--corpus", str(corpus)]
    env = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1", "PYTHONHASHSEED": "0"}
    # Worker owns its new directory. Logs are retained even if it fails before mkdir.
    out.parent.mkdir(parents=True, exist_ok=True)
    started = time.perf_counter_ns()
    try:
        done = subprocess.run(args, capture_output=True, env=env, timeout=120)
        code, stdout, stderr = done.returncode, done.stdout, done.stderr
    except subprocess.TimeoutExpired as exc:
        code, stdout, stderr = 124, exc.stdout or b"", exc.stderr or b""
    wall = (time.perf_counter_ns() - started) / 1e9
    out.mkdir(exist_ok=True)
    (out / "stdout.txt").write_bytes(stdout)
    (out / "stderr.txt").write_bytes(stderr)
    record = {
        "argv": args,
        "exit_code": code,
        "expected_exit_code": expected,
        "process_wall_seconds": wall,
        "process_boundary": "before spawn through exit and stdout/stderr capture",
        "timeout_seconds": 120,
    }
    if (out / "timing.json").exists():
        record.update(json.loads((out / "timing.json").read_text()))
    dump(out / "process.json", record)
    if code != expected:
        raise ValueError(f"unexpected_exit:{operation}:{code}:expected:{expected}:logs:{out}")
    return record


def rejection_matrix(tree, corpus, out):
    roots = json.loads((corpus / "corpus.json").read_text())["runs"]
    cases = (
        ("sealed_event_mutation", 4),
        ("missing_memory", 4),
        ("duplicate_manifest_key", 4),
        ("legacy_requests", 2),
    )
    rows = []
    for name, expected in cases:
        target = out / "damaged" / name
        shutil.copytree(roots[0], target)
        if name == "sealed_event_mutation":
            with (target / "events.jsonl").open("ab") as stream:
                stream.write(b"{}\n")
        elif name == "missing_memory":
            (target / "memory.jsonl").unlink()
        elif name == "duplicate_manifest_key":
            p = target / "manifest.json"
            p.write_text(p.read_text().replace("{", '{"schema_version":3,', 1))
        else:
            (target / "requests.jsonl").write_text("{}\n")
        record = run_child(tree, "reject", out / "checks" / name, target, expected)
        rows.append(
            {
                "case": name,
                "expected_exit_code": expected,
                "observed": record,
                "modified_copy": str(target),
                "original_unchanged": True,
            }
        )
    dump(out / "rejection-matrix.json", rows)


def measure(args):
    before = inventory(args.corpus)
    dump(args.out / "input-inventory.json", before)
    trees = {"baseline": args.baseline}
    if args.candidate:
        trees["candidate"] = args.candidate
    identities = {label: source(tree) for label, tree in trees.items()}
    dump(args.out / "sources.json", identities)
    records, exceptions = [], []
    for iteration in range(args.repeats):
        # AB then BA prevents one tree always inheriting the first/warm cache position.
        labels = list(trees) if iteration % 2 == 0 else list(reversed(trees))
        for operation in OPERATIONS:
            pair = {}
            for label in labels:
                out = args.out / "measurements" / f"{iteration:02d}" / label / operation
                record = run_child(trees[label], operation, out, args.corpus)
                check_expected(out, args.corpus, operation)
                record.update(
                    iteration=iteration,
                    label=label,
                    operation=operation,
                    out=str(out),
                    counts=counts(out),
                    artifacts=inventory(out),
                )
                records.append(record)
                dump(args.out / "measurements.json", records)
                pair[label] = out
                if iteration:
                    first = args.out / "measurements/00" / label / operation
                    exceptions.extend(compare_outputs(first, out, operation))
            if args.candidate:
                exceptions.extend(compare_outputs(pair["baseline"], pair["candidate"], operation))
    for label, tree in trees.items():
        rejection_matrix(tree, args.corpus, args.out / "rejections" / label)
        if source(tree) != identities[label]:
            raise ValueError(f"source_changed_during_measurement:{label}")
    if inventory(args.corpus) != before:
        raise ValueError("input_changed_during_measurement")
    dump(args.out / "input-inventory.json", before)
    dump(args.out / "equivalence-exceptions.json", exceptions)
    summary = []
    for label in trees:
        for operation in OPERATIONS:
            selected = [r for r in records if r["label"] == label and r["operation"] == operation]
            row = {"label": label, "operation": operation, "n": len(selected)}
            for metric in ("process_wall_seconds", "operation_wall_seconds", "peak_rss_bytes"):
                values = [r[metric] for r in selected]
                row[metric] = {
                    "values": values,
                    "median": statistics.median(values),
                    "min": min(values),
                    "max": max(values),
                }
            summary.append(row)
    dump(
        args.out / "summary.json",
        {
            "synthetic_software_only": True,
            "comparison_complete": bool(args.candidate),
            "measurements": summary,
            "limitations": [
                "not_model_or_device_acceptance",
                "warm_OS_cache_not_flushed",
                "peak_RSS_includes_worker_setup",
                "no_browser_visual_acceptance",
            ],
        },
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "measure"))
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--candidate", type=Path)
    parser.add_argument("--corpus", type=Path)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--repeats", type=int, default=5)
    args = parser.parse_args()
    args.out = args.out.resolve()
    args.baseline = args.baseline.resolve()
    if args.candidate:
        args.candidate = args.candidate.resolve()
    if args.corpus:
        args.corpus = args.corpus.resolve()
    if not args.out.is_relative_to(Path("/tmp").resolve()):
        parser.error("--out must be a new directory under /tmp")
    if args.repeats < 5:
        parser.error("at least five repetitions required")
    if args.mode == "measure" and not args.corpus:
        parser.error("measure requires --corpus")
    args.out.mkdir(parents=True, exist_ok=False)
    dump(
        args.out / "harness.json",
        {
            "python": sys.executable,
            "version": sys.version,
            "scripts": {p.name: digest(p) for p in WORKER.parent.glob("benchmark_cost*.py")},
        },
    )
    try:
        if args.mode == "prepare":
            identity = source(args.baseline)
            dump(args.out / "source.json", identity)
            run_child(args.baseline, "prepare", args.out / "corpus")
            if source(args.baseline) != identity:
                raise ValueError("source_changed_during_prepare")
            dump(args.out / "input-inventory.json", inventory(args.out / "corpus"))
        else:
            measure(args)
    except Exception as exc:
        dump(args.out / "failure.json", {"error": str(exc)})
        print(json.dumps({"error": str(exc), "out": str(args.out)}), file=sys.stderr)
        return 1
    print(
        json.dumps(
            {"out": str(args.out), "status": "complete", "candidate_measured": bool(args.candidate)}
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

"""Bounded reader experiment with 120/12000 prepared records; no model or workload execution."""

import argparse
import copy
import hashlib
import json
import resource
import shutil
import subprocess
import sys
import time
from pathlib import Path


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False))


def worker(args):
    from unittest.mock import patch

    from inferyard.evidence.storage import json_bytes, read_jsonl
    from inferyard.evidence.trial_evidence import TrialEvidence
    from tests.evidence_layout_helpers import digest, seal_layout

    target = args.out / "synthetic-index-input"
    shutil.copytree(args.source, target)
    source = TrialEvidence(target)
    run = source.input("run")
    original, _ = read_jsonl(target / "request-bodies.jsonl")
    queries = [
        (
            {key: row[key] for key in ("run_id", "trial_id", "request_id", "phase")},
            row["body_sha256"],
        )
        for row in original
    ]
    body = copy.deepcopy(original[-1]["body"])
    body["messages"][-1]["content"] = "x" * 8192
    body_hash = digest(body)
    ordinal = max(row["ordinal"] for row in original)
    template = {**original[-1], "body": body, "body_sha256": body_hash, "phase": "formal"}
    with (target / "request-bodies.jsonl").open("ab") as stream:
        for index in range(args.count - len(original)):
            row = {**template, "ordinal": ordinal + index + 1}
            row["request_id"] = run["run_id"] + "-formal-" + str(row["ordinal"])
            stream.write(json_bytes(row))
            queries.append(
                (
                    {key: row[key] for key in ("run_id", "trial_id", "request_id", "phase")},
                    body_hash,
                )
            )
    seal_layout(target, run)
    del source, original, body, template
    original_open = Path.open
    scans, scanned_bytes = 0, 0

    class TracedFile:
        def __init__(self, stream):
            self.stream = stream

        def __enter__(self):
            self.stream.__enter__()
            return self

        def __exit__(self, *values):
            return self.stream.__exit__(*values)

        def __iter__(self):
            nonlocal scans, scanned_bytes
            scans += 1
            for line in self.stream:
                scanned_bytes += len(line)
                yield line

        def __getattr__(self, name):
            return getattr(self.stream, name)

    def observed_open(path, *values, **options):
        stream = original_open(path, *values, **options)
        if path == target / "request-bodies.jsonl" and values == ("rb",):
            return TracedFile(stream)
        return stream

    checksum = hashlib.sha256()
    started = time.perf_counter_ns()
    with patch.object(Path, "open", observed_open):
        evidence = TrialEvidence(target)
        for query, expected in queries:
            actual = digest(evidence.request_body(query))
            if actual != expected:
                raise ValueError("index_body_content_mismatch")
            checksum.update(actual.encode())
    elapsed = (time.perf_counter_ns() - started) / 1e9
    size = (target / "request-bodies.jsonl").stat().st_size
    if scans != 1 or scanned_bytes != size or len(evidence._prepared.entries) != args.count:
        raise ValueError("index_work_not_complete_or_repeated")
    dump(
        args.out / "receipt.json",
        {
            "synthetic_component_experiment": True,
            "executed_model_requests": 0,
            "prepared_records": args.count,
            "full_scans": scans,
            "scanned_bytes": scanned_bytes,
            "index_entries": len(evidence._prepared.entries),
            "body_readback_checksum": checksum.hexdigest(),
            "reader_wall_seconds": elapsed,
            "peak_rss_bytes": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            * (1 if sys.platform == "darwin" else 1024),
            "timing_scope": (
                "reader construction, one full scan, all body range reads and digest checks"
            ),
            "limitations": [
                "instrumented_scan_count",
                "RSS_includes_setup",
                "not_native_acceptance",
                "not_a_benchmark_score",
            ],
        },
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--count", type=int, choices=(120, 12000))
    args = parser.parse_args()
    args.tree, args.source, args.out = (p.resolve() for p in (args.tree, args.source, args.out))
    if not args.out.is_relative_to(Path("/tmp").resolve()):
        parser.error("--out must be a fresh /tmp directory")
    args.out.mkdir(parents=True, exist_ok=False)
    sys.path[:0] = [str(args.tree / "src"), str(args.tree)]
    if args.count is not None:
        worker(args)
        return 0
    rows = []
    for count in (120, 12000):
        argv = [
            sys.executable,
            "-I",
            "-B",
            str(Path(__file__).resolve()),
            "--tree",
            str(args.tree),
            "--source",
            str(args.source),
            "--out",
            str(args.out / str(count)),
            "--count",
            str(count),
        ]
        done = subprocess.run(argv, capture_output=True, timeout=120)
        (args.out / f"{count}.stdout.txt").write_bytes(done.stdout)
        (args.out / f"{count}.stderr.txt").write_bytes(done.stderr)
        rows.append({"argv": argv, "exit_code": done.returncode})
        dump(args.out / "processes.json", rows)
        if done.returncode:
            raise ValueError("index_worker_failed:" + str(count))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

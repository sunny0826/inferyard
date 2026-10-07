"""Validate observer logs and optionally link sealed benchmark evidence read-only."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import sys
from pathlib import Path

from inferyard.contracts.validation import strict_json_loads
from inferyard.evidence.storage import EvidenceError

if __package__:
    from .observer_benchmark import analysis_source_hash, link_benchmark, output_outside_run
    from .observer_stream import (
        ENVELOPE,
        MAX_BYTES,
        MAX_LINE,
        ObserverError,
        Series,
        fields,
        header,
        health,
        integer,
        metric,
        require,
    )
else:
    from observer_benchmark import analysis_source_hash, link_benchmark, output_outside_run
    from observer_stream import (
        ENVELOPE,
        MAX_BYTES,
        MAX_LINE,
        ObserverError,
        Series,
        fields,
        header,
        health,
        integer,
        metric,
        require,
    )

SUPPORTED = {"lab_observer.v1", "lab_observer.v2"}
SUMMARY_DEFINITION = "lab_observer_summary.v2"


def analyze(path: Path, *, allow_incomplete: bool = False) -> dict:
    # Refuse pipes/devices before opening; recheck the actual descriptor below.
    require(stat.S_ISREG(path.stat().st_mode), "observer_log_not_regular")
    digest = hashlib.sha256()
    h = end = process = observer = None
    samples = previous_finish = total = 0
    minima: dict[str, int | None] = {"memory_available_bytes": None, "disk_available_bytes": None}
    host_sources: dict[str, str] = {}
    with path.open("rb") as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode), "observer_log_not_regular")
        require(0 < before.st_size <= MAX_BYTES, "observer_log_size_invalid")
        while raw := stream.readline(MAX_LINE + 1):
            total += len(raw)
            require(
                total <= MAX_BYTES and len(raw) <= MAX_LINE and raw.endswith(b"\n"),
                "observer_line_size_or_termination_invalid",
            )
            digest.update(raw)
            try:
                item = strict_json_loads(raw.decode("utf-8"))
            except (ValueError, UnicodeError) as exc:
                raise ObserverError("observer_json_invalid") from exc
            require(
                type(item) is dict
                and item.get("schema_version") == 3
                and type(item["schema_version"]) is int
                and type(item.get("definition")) is str
                and item.get("definition") in SUPPORTED
            )
            require(
                type(item.get("session_id")) is str
                and re.fullmatch(r"[0-9a-f]{32}", item["session_id"]) is not None
            )
            if h is None:
                header(item)
                h = item
                process, observer = Series(h["targets"][0]), Series(h["observer_target"])
                continue
            require(
                item["session_id"] == h["session_id"]
                and item["definition"] == h["definition"]
                and end is None
            )
            if item.get("kind") == "observer_end":
                end = fields(
                    item,
                    ENVELOPE
                    | {
                        "samples",
                        "elapsed_ns",
                        "skipped_intervals",
                        "stop_reason",
                        "performance_comparison_qualified",
                    },
                )
                require(integer(end["samples"]) == samples and samples > 0)
                reason = end["stop_reason"]
                require(
                    type(reason) is str
                    and reason
                    in {
                        "duration_reached",
                        "cancelled",
                        "target_unavailable",
                        "observer_unavailable",
                    }
                )
                require(allow_incomplete or reason == "duration_reached", "observer_run_incomplete")
                integer(
                    end["elapsed_ns"],
                    max(previous_finish, h["duration_ns"])
                    if reason == "duration_reached"
                    else previous_finish,
                )
                if process.state != "running":
                    require(reason == "target_unavailable")
                elif observer.state != "running":
                    require(reason == "observer_unavailable")
                else:
                    require(reason in {"duration_reached", "cancelled"})
                integer(end["skipped_intervals"])
                require(end["performance_comparison_qualified"] is False)
                continue
            fields(
                item,
                ENVELOPE
                | {
                    "seq",
                    "read_started_ns",
                    "read_finished_ns",
                    "host",
                    "processes",
                    "observer",
                    "endpoint_health",
                },
            )
            require(item["kind"] == "observer_sample")
            require(
                process.state == observer.state == "running", "observer_samples_after_target_lost"
            )
            samples += 1
            require(integer(item["seq"], 1) == samples and samples <= 864_001)
            start = integer(item["read_started_ns"], previous_finish)
            require(start < h["duration_ns"])
            finish = integer(item["read_finished_ns"], start)
            previous_finish = finish
            host = fields(item["host"], set(minima))
            for key in minima:
                value = metric(host[key], "int")
                source = host[key]["source"]
                require(
                    key not in host_sources or source == host_sources[key],
                    "observer_source_changed",
                )
                host_sources[key] = source
                if value is not None:
                    minima[key] = value if minima[key] is None else min(minima[key], value)
            require(type(item["processes"]) is list and len(item["processes"]) == 1)
            process.read(item["processes"][0], start, finish, allow_incomplete=allow_incomplete)
            observer.read(item["observer"], start, finish, allow_incomplete=allow_incomplete)
            health(item["endpoint_health"])
        after = os.fstat(stream.fileno())
        current = path.stat()
        require(
            (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
            == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns)
            == (current.st_dev, current.st_ino, current.st_size, current.st_mtime_ns),
            "observer_log_changed_during_read",
        )
    require(
        h is not None and samples > 0 and (end is not None or allow_incomplete),
        "observer_run_incomplete",
    )
    complete = end is not None and end["stop_reason"] == "duration_reached"
    return {
        "schema_version": 3,
        "kind": "observer_diagnostic_summary",
        "definition": SUMMARY_DEFINITION,
        "input_definition": h["definition"],
        "disk_scope": h.get("disk_scope", "observer_cwd_filesystem_legacy_v1"),
        "completeness": "complete" if complete else "incomplete",
        "stop_reason": end["stop_reason"] if end is not None else "end_record_missing",
        "end_record_present": end is not None,
        "session_id": h["session_id"],
        "input_sha256": digest.hexdigest(),
        "analysis_source_sha256": analysis_source_hash(),
        "tool_source_sha256": h["tool_source_sha256"],
        "binary_sha256": h["binary_sha256"],
        "system": h["system"],
        "benchmark_binding": h["benchmark_binding"],
        "benchmark_evidence": None,
        "samples": samples,
        "elapsed_ns": end["elapsed_ns"] if end is not None else None,
        "elapsed_missing_reason": None if end is not None else "end_record_missing",
        "last_sample_finished_ns": previous_finish,
        "skipped_intervals": end["skipped_intervals"] if end is not None else None,
        "skipped_missing_reason": None if end is not None else "end_record_missing",
        "process": process.summary(),
        "observer": observer.summary(),
        "host_minimum_bytes": minima,
        "host_sources": host_sources,
        "host_missing_reasons": {
            key: "no_valid_samples" for key, value in minima.items() if value is None
        },
        "performance_comparison_qualified": False,
        "limitations": [
            "diagnostic_only",
            "no_generation_or_model_loading_proof",
            "cpu_summary_excludes_cold_start_and_final_sync",
            "session_clock_not_aligned_to_benchmark_requests",
        ],
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--log", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True, help="new diagnostic JSON")
    parser.add_argument("--bench-run", type=Path, help="associate sealed v3 run, read-only")
    parser.add_argument(
        "--allow-incomplete", action="store_true", help="retain explicit partial observations"
    )
    args = parser.parse_args(argv)
    try:
        result = analyze(args.log, allow_incomplete=args.allow_incomplete)
        if args.bench_run is not None:
            output_outside_run(args.bench_run, args.out)
            result["benchmark_evidence"] = link_benchmark(result, args.bench_run)
        with args.out.open("x", encoding="utf-8") as stream:
            json.dump(result, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
    except OSError, ValueError, KeyError, TypeError, OverflowError, EvidenceError:
        print(
            "observer_analysis_failed; input invalid, incomplete or output already exists",
            file=sys.stderr,
        )
        return 4
    complete = result["completeness"] == "complete"
    print(json.dumps({"completed": complete, "performance_comparison_qualified": False}))
    return 0 if complete else 3


if __name__ == "__main__":
    raise SystemExit(main())

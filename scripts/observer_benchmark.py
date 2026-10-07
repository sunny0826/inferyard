"""Read-only association with sealed v3 benchmark evidence; no clock alignment."""

from __future__ import annotations

import hashlib
from pathlib import Path

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import local_file, sha256_file, verify_manifest

if __package__:
    from .observer_stream import require
else:
    from observer_stream import require


def analysis_source_hash() -> str:
    digest = hashlib.sha256()
    for name in ("analyze_observer.py", "observer_benchmark.py", "observer_stream.py"):
        data = Path(__file__).with_name(name).read_bytes()
        digest.update(name.encode() + b"\0" + len(data).to_bytes(8, "big") + data)
    return digest.hexdigest()


def output_outside_run(root: Path, output: Path) -> None:
    require(
        not output.resolve().is_relative_to(root.resolve()),
        "observer_output_must_be_outside_benchmark_run",
    )


def link_benchmark(summary: dict, root: Path) -> dict:
    binding = summary["benchmark_binding"]
    require(binding is not None, "observer_benchmark_binding_missing")
    root = root.resolve(strict=True)
    manifest_path = local_file(root, "manifest.json")
    require(manifest_path.is_file(), "observer_benchmark_not_sealed")
    before = sha256_file(manifest_path)
    require(not verify_manifest(root), "observer_benchmark_manifest_incomplete")
    trial = read_trial(root)
    run, config = trial["run"], trial["config"]
    require(run["origin"] == "measured", "observer_benchmark_origin_not_measured")
    require(
        binding["run_id"] == run["run_id"]
        and binding["run_file_sha256"] == sha256_file(local_file(root, "run.json"))
        and binding["config_file_sha256"] == sha256_file(local_file(root, "config.frozen.json")),
        "observer_benchmark_binding_mismatch",
    )
    endpoint = config["endpoint"]
    require(
        summary["process"]["pid"] == endpoint["server_pid"]
        and summary["process"]["process_start_ticks"] == endpoint["process_start_ticks"],
        "observer_benchmark_process_mismatch",
    )
    require(
        binding["model_label_declared"] == config["model"]["display_name"]
        and binding["backend_declared"] == config["engine"]["backend"],
        "observer_benchmark_declared_label_mismatch",
    )
    require(
        not verify_manifest(root) and before == sha256_file(manifest_path),
        "observer_benchmark_changed_during_read",
    )
    return {
        "run_id": run["run_id"],
        "experiment_id": run["experiment_id"],
        "trial_id": run["trial_id"],
        "manifest_sha256": before,
        "events_sha256": trial["events_sha256"],
        "benchmark_tool_source_sha256": run["tool_source_sha256"],
        "diagnostic": run["diagnostic"],
        "completeness": trial["summary"]["completeness"],
        "counts": trial["summary"]["counts"],
        "source_binding_verified": True,
        "whole_run_coverage_verified": False,
        "request_alignment": "not_performed_no_shared_clock",
        "performance_comparison_qualified": False,
    }

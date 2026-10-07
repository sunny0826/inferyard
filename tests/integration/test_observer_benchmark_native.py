"""Native TCP fixture with the real Go observer; no inference weights or model launch."""

import asyncio
import json
import os
import shutil
import subprocess
import sys
import time
from collections import Counter
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import sha256_file, verify_manifest
from inferyard.reporting.report import verify_report, write_report
from inferyard.runtime.runner import execute_async
from scripts.analyze_observer import ObserverError, analyze, link_benchmark, main
from scripts.build_observer import build
from tests.integration.test_macos_runtime import native_service as macos_service

native_service = macos_service
pytestmark = pytest.mark.skipif(sys.platform != "darwin", reason="Native macOS observer workflow")
ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture(scope="module")
def observer_binary(tmp_path_factory):
    out = tmp_path_factory.mktemp("observer-build") / "release"
    arch = "arm64" if os.uname().machine == "arm64" else "amd64"
    receipt = build(out, [f"darwin/{arch}"])
    assert receipt["completed"]
    return out / receipt["artifacts"][0]["file"]


def start_observer(binary, root, log, duration="2s"):
    return subprocess.Popen(
        [
            str(binary),
            "watch",
            "--bench-run",
            str(root),
            "--duration",
            duration,
            "--interval",
            "100ms",
            "--out",
            str(log),
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )


def await_samples(process, log, count=2):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if log.exists() and log.read_bytes().count(b"\n") >= count + 1:
            return
        assert process.poll() is None, process.communicate(timeout=1)
        time.sleep(0.01)
    raise AssertionError("observer did not emit bounded startup samples")


def finish_observer(process, expected):
    out, err = process.communicate(timeout=5)
    assert process.returncode == expected, (out, err)
    return process.returncode


async def parallel_run(loaded, binary, log):
    output_root = Path(loaded.config.to_dict()["output"]["root"])
    old = set(output_root.iterdir())
    task = asyncio.create_task(execute_async(CommandRequest("run", config=loaded, diagnostic=True)))
    deadline = time.monotonic() + 10
    observer = None
    try:
        while time.monotonic() < deadline:
            candidates = set(output_root.iterdir()) - old
            ready = [
                p
                for p in candidates
                if (p / "run.json").is_file() and (p / "config.frozen.json").is_file()
            ]
            if ready:
                assert len(ready) == 1
                observer = start_observer(binary, ready[0], log)
                break
            assert not task.done(), task.result()
            await asyncio.sleep(0.01)
        assert observer is not None, "benchmark did not publish immutable binding files"
        result = await task
        await asyncio.to_thread(finish_observer, observer, 0)
        return result
    finally:
        if observer is not None and observer.poll() is None:
            observer.kill()
            observer.communicate(timeout=5)
        if not task.done():
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass


def phase_counts(root):
    events = [json.loads(line) for line in (root / "events.jsonl").read_text().splitlines()]
    return dict(Counter(e["phase"] for e in events if e["event_type"] == "request_started"))


def proof_counts(data):
    counts = data["summary"]["counts"]
    return {
        key: counts[key]
        for key in ("planned", "completed", "failed", "cancelled", "invalid", "not_executed")
    }


def test_native_benchmark_parallel_cancel_exit_and_offline_link(
    native_service, observer_binary, tmp_path
):
    loaded, service = native_service
    code, baseline_result = asyncio.run(
        execute_async(CommandRequest("run", config=loaded, diagnostic=True))
    )
    assert code == 3 and "diagnostic_run_not_formal" in baseline_result.limitations
    baseline = Path(baseline_result.evidence_dir)
    log = tmp_path / "parallel.jsonl"
    code, result = asyncio.run(parallel_run(loaded, observer_binary, log))
    assert code == 3 and "diagnostic_run_not_formal" in result.limitations
    root = Path(result.evidence_dir)
    a, b = read_trial(baseline), read_trial(root)
    assert (
        proof_counts(a)
        == proof_counts(b)
        == {
            "planned": 2,
            "completed": 2,
            "failed": 0,
            "cancelled": 0,
            "invalid": 0,
            "not_executed": 0,
        }
    )
    assert [(r["case_id"], r["content"], r["execution_state"]) for r in a["requests"]] == [
        (r["case_id"], r["content"], r["execution_state"]) for r in b["requests"]
    ]
    assert phase_counts(baseline) == phase_counts(root)
    assert phase_counts(root)["formal"] == 2
    assert verify_manifest(baseline) == verify_manifest(root) == []
    original = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}

    cancellation = tmp_path / "cancelled.jsonl"
    observer = start_observer(observer_binary, root, cancellation, "30s")
    try:
        await_samples(observer, cancellation)
        observer.terminate()  # Only the child started by this test.
        finish_observer(observer, 130)
    finally:
        if observer.poll() is None:
            observer.kill()
            observer.communicate(timeout=5)
    with pytest.raises(ObserverError):
        analyze(cancellation)
    partial = analyze(cancellation, allow_incomplete=True)
    assert partial["stop_reason"] == "cancelled"

    exited = tmp_path / "exited.jsonl"
    observer = start_observer(observer_binary, root, exited, "30s")
    try:
        await_samples(observer, exited)
        service.terminate()  # Synthetic fixture, after both benchmark runs finished.
        service.wait(timeout=5)
        finish_observer(observer, 3)
    finally:
        if observer.poll() is None:
            observer.kill()
            observer.communicate(timeout=5)
    lost = analyze(exited, allow_incomplete=True)
    assert lost["process"]["last_state"] == "exited"
    assert lost["process"]["stop_reason"] == "process_exited"
    assert lost["completeness"] == partial["completeness"] == "incomplete"

    summary_path = tmp_path / "linked.json"
    assert main(["--log", str(log), "--bench-run", str(root), "--out", str(summary_path)]) == 0
    summary = json.loads(summary_path.read_text())
    assert summary["disk_scope"] == "benchmark_run_filesystem"
    assert summary["tool_source_sha256"]["value"] is not None
    assert summary["benchmark_evidence"]["source_binding_verified"] is True
    assert summary["benchmark_evidence"]["diagnostic"] is True
    assert summary["benchmark_evidence"]["completeness"] == "incomplete"
    for data in (partial, lost):
        assert link_benchmark(data, root)["counts"]["completed"] == 2
    report = tmp_path / "offline-report"
    write_report([baseline, root], report)
    assert verify_report(report)["verified"]
    assert original == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}

    evidence = {
        "kind": "observer_benchmark_native_fixture_feedback",
        "platform": sys.platform,
        "synthetic_service": True,
        "real_model_inference": False,
        "baseline_counts": proof_counts(a),
        "parallel_counts": proof_counts(b),
        "baseline_phase_requests": phase_counts(baseline),
        "parallel_phase_requests": phase_counts(root),
        "same_answers_and_terminal_states": True,
        "original_files_unchanged": True,
        "offline_report_verified": True,
        "observer_end_codes": {"normal": 0, "cancelled": 130, "target_exited": 3},
        "partial_stop_reasons": [partial["stop_reason"], lost["stop_reason"]],
        "analysis_source_sha256": summary["analysis_source_sha256"],
        "observer_source_sha256": summary["tool_source_sha256"]["value"],
        "observer_binary_sha256": summary["binary_sha256"]["value"],
        "observer_log_sha256": sha256_file(log),
        "performance_comparison_qualified": False,
    }
    if proof_dir := os.environ.get("LAB_OBSERVER_ADAPTATION_PROOF"):
        proof = Path(proof_dir)
        proof.mkdir(parents=True, exist_ok=False)
        for name, source in (("baseline", baseline), ("parallel", root), ("report", report)):
            shutil.copytree(source, proof / name)
        for source in (log, cancellation, exited, summary_path):
            shutil.copy2(source, proof / source.name)
        # A report binds paths as well as bytes. Rebuild after archiving rather
        # than changing the original index or pretending its paths still match.
        archived_report = proof / "replay-report"
        write_report([proof / "baseline", proof / "parallel"], archived_report)
        evidence["archived_report_verified"] = verify_report(archived_report)["verified"]
        (proof / "feedback.json").write_text(json.dumps(evidence, indent=2) + "\n")

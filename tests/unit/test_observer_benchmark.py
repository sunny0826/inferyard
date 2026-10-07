"""Associate sealed raw evidence without trusting live PID or editing originals."""

import json

import pytest

from inferyard.evidence.storage import EvidenceError, sha256_file
from tests.helpers import fixture_run
from tests.unit.test_observer_analysis import bridge as observer_bridge
from tests.unit.test_observer_analysis import records, save

bridge = observer_bridge


def bound_log(tmp_path, root):
    values = records()
    run = json.loads((root / "run.json").read_text())
    config = json.loads((root / "config.frozen.json").read_text())
    for item in values:
        item["definition"] = "lab_observer.v2"
    values[0]["disk_scope"] = "benchmark_run_filesystem"
    values[0]["benchmark_binding"] = {
        "run_id": run["run_id"],
        "run_file_sha256": sha256_file(root / "run.json"),
        "config_file_sha256": sha256_file(root / "config.frozen.json"),
        "model_label_declared": config["model"]["display_name"],
        "backend_declared": config["engine"]["backend"],
    }
    for target in (
        values[0]["targets"][0],
        values[1]["processes"][0],
        values[2]["processes"][0],
    ):
        target["pid"] = config["endpoint"]["server_pid"]
        target["process_start_ticks"] = config["endpoint"]["process_start_ticks"]
    return save(tmp_path, values)


def test_link_sealed_trial_is_read_only_and_preserves_benchmark_counts(bridge, tmp_path):
    root = fixture_run(tmp_path / "benchmark")
    log = bound_log(tmp_path, root)
    before = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    out = tmp_path / "linked.json"
    assert bridge.main(["--log", str(log), "--bench-run", str(root), "--out", str(out)]) == 0
    result = json.loads(out.read_text())
    evidence = result["benchmark_evidence"]
    assert evidence["counts"]["planned"] == 3
    assert evidence["counts"]["completed"] == 2
    assert evidence["counts"]["failed"] == 1
    assert evidence["source_binding_verified"] is True
    assert evidence["whole_run_coverage_verified"] is False
    assert evidence["request_alignment"] == "not_performed_no_shared_clock"
    assert evidence["performance_comparison_qualified"] is False
    assert len(result["analysis_source_sha256"]) == 64
    assert before == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}


@pytest.mark.parametrize(
    "case", ["unbound", "run_id", "run_hash", "config_hash", "pid", "start", "label", "backend"]
)
def test_wrong_binding_rejected_without_output(bridge, tmp_path, case):
    root = fixture_run(tmp_path / "benchmark")
    log = bound_log(tmp_path, root)
    summary = bridge.analyze(log)
    binding = summary["benchmark_binding"]
    if case == "unbound":
        summary["benchmark_binding"] = None
    elif case == "pid":
        summary["process"]["pid"] += 1
    elif case == "start":
        summary["process"]["process_start_ticks"] += 1
    else:
        key = {
            "run_id": "run_id",
            "run_hash": "run_file_sha256",
            "config_hash": "config_file_sha256",
            "label": "model_label_declared",
            "backend": "backend_declared",
        }[case]
        binding[key] = "different"
    with pytest.raises(bridge.ObserverError):
        bridge.link_benchmark(summary, root)


@pytest.mark.parametrize("case", ["unsealed", "raw_changed", "derived_changed", "traversal"])
def test_invalid_evidence_rejected(bridge, tmp_path, case):
    root = fixture_run(tmp_path / "benchmark")
    log = bound_log(tmp_path, root)
    if case == "unsealed":
        (root / "manifest.json").unlink()
    elif case == "raw_changed":
        with (root / "events.jsonl").open("ab") as stream:
            stream.write(b"\n")
    elif case == "derived_changed":
        derived = root / "summary.json"
        derived.write_text("{}")
        manifest = json.loads((root / "manifest.json").read_text())
        manifest["files"]["summary.json"] = {
            "bytes": derived.stat().st_size,
            "sha256": sha256_file(derived),
            "derived": True,
        }
        (root / "manifest.json").write_text(json.dumps(manifest))
        derived.write_text("[]")
    else:
        manifest = json.loads((root / "manifest.json").read_text())
        manifest["files"]["../outside.json"] = manifest["files"]["run.json"]
        (root / "manifest.json").write_text(json.dumps(manifest))
    out = tmp_path / "new.json"
    assert bridge.main(["--log", str(log), "--bench-run", str(root), "--out", str(out)]) == 4
    assert not out.exists()


def test_cannot_add_summary_to_source_run_or_symlink(bridge, tmp_path):
    root = fixture_run(tmp_path / "benchmark")
    log = bound_log(tmp_path, root)
    alias = tmp_path / "alias"
    alias.symlink_to(root, target_is_directory=True)
    for parent in (root, alias):
        out = parent / "observer.json"
        assert bridge.main(["--log", str(log), "--bench-run", str(root), "--out", str(out)]) == 4
        assert not out.exists()


def test_manifest_changed_while_reducing_is_rejected(bridge, tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "benchmark")
    summary = bridge.analyze(bound_log(tmp_path, root))
    original = bridge.link_benchmark.__globals__["read_trial"]

    def changed(path):
        result = original(path)
        with (path / "manifest.json").open("ab") as stream:
            stream.write(b"\n")
        return result

    # The CLI can import this script both as a package and directly.
    monkeypatch.setitem(bridge.link_benchmark.__globals__, "read_trial", changed)
    with pytest.raises((bridge.ObserverError, EvidenceError)):
        bridge.link_benchmark(summary, root)

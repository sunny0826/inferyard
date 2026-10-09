"""Same-byte sidecar hashing preserves strict parsing and manifest verification."""

import hashlib
from collections import Counter
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, read_jsonl
from inferyard.evidence.trial_reads import TrialReads
from tests.helpers import fixture_run


def reseal(root, name, raw):
    (root / name).write_bytes(raw)
    manifest = read_json(root / "manifest.json")
    manifest["files"][name] = {
        "bytes": len(raw),
        "sha256": hashlib.sha256(raw).hexdigest(),
        "derived": False,
    }
    (root / "manifest.json").write_bytes(json_bytes(manifest))


def test_sidecars_are_parsed_and_hashed_once_per_read(tmp_path, monkeypatch):
    root = fixture_run(tmp_path)
    reseal(root, "external-cpu.jsonl", b"")
    counts = Counter()
    original = Path.open

    def opened(path, *args, **kwargs):
        if path.parent == root:
            counts[path.name] += 1
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", opened)
    first = read_trial(root)
    for name in (
        "environment.start.json",
        "environment.end.json",
        "environment.jsonl",
        "schedule.jsonl",
        "external-cpu.jsonl",
    ):
        assert counts[name] == 1, (name, counts)
    assert read_trial(root) == first
    assert counts["environment.jsonl"] == 2
    (root / "environment.jsonl").write_bytes(b"changed")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        read_trial(root)


@pytest.mark.parametrize(
    "raw",
    [
        b'{}\n{"unfinished":',
        b'{"x":1,"x":2}\n',
        b'{"x":NaN}\n',
        b'{"x":1e999}\n',
        b"[]\n",
        b"{}\nbad\n",
        b"\xff\n",
        b"{}\n",
    ],
)
def test_checked_jsonl_matches_storage_rejections_and_tails(tmp_path, raw):
    path = tmp_path / "environment.jsonl"
    path.write_bytes(raw)
    reads = TrialReads(tmp_path)
    try:
        expected = read_jsonl(path)
    except EvidenceError as exc:
        with pytest.raises(EvidenceError, match=str(exc)):
            reads.checked_jsonl(path.name)
    else:
        assert reads.checked_jsonl(path.name) == expected
    assert reads.observed()[path.name] == (len(raw), hashlib.sha256(raw).hexdigest())


@pytest.mark.parametrize("name", ["environment.jsonl", "schedule.jsonl", "external-cpu.jsonl"])
@pytest.mark.parametrize("raw", [b"[]\n", b'{"x":1,"x":2}\n', b"{}"])
def test_sealed_sidecar_damage_is_never_ignored(tmp_path, name, raw):
    root = fixture_run(tmp_path)
    reseal(root, name, raw)
    if raw == b"{}" and name != "external-cpu.jsonl":
        context = read_trial(root)["summary"]["measurement_context"]
        assert name + ":truncated_tail_at_byte:0" in context["environment"]["reasons"]
    else:
        with pytest.raises(EvidenceError):
            read_trial(root)


def test_missing_sealed_sidecar_is_hash_failure(tmp_path):
    root = fixture_run(tmp_path)
    (root / "environment.end.json").unlink()
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        read_trial(root)


def boundary_record_fixture(root, monkeypatch):
    from inferyard.analysis import boundary_environment
    from inferyard.analysis.overhead_environment import environment_record
    from tests.unit.test_boundary_environment import fixture

    captured = {}
    original = boundary_environment.qualify_boundary_environment

    def capture(path, data, sealed):
        captured.update(data)
        return original(path, data, sealed)

    with monkeypatch.context() as patch:
        patch.setattr("tests.unit.test_boundary_environment.qualify_boundary_environment", capture)
        fixture(root)
    captured.update(
        run={"run_id": "r"},
        summary={
            "measurement_context": {"environment_qualification": {"eligible": True, "reasons": []}}
        },
    )
    files = [path.name for path in root.iterdir()]
    (root / "manifest.json").write_bytes(json_bytes({"files": dict.fromkeys(files, {})}))
    return captured, environment_record


def test_overhead_boundary_shares_local_reads_and_stays_json(tmp_path, monkeypatch):
    data, environment_record = boundary_record_fixture(tmp_path, monkeypatch)
    counts = Counter()
    original = Path.open

    def opened(path, *args, **kwargs):
        if path.parent == tmp_path:
            counts[path.name] += 1
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", opened)
    result = environment_record(tmp_path, data, require_boundary=True)
    assert result["qualification"]["eligible"]
    assert json_bytes(result)
    for name in (
        "manifest.json",
        "environment.start.json",
        "environment.end.json",
        "boundary-observer.json",
        "request-environment.jsonl",
    ):
        assert counts[name] == 1
    environment_record(tmp_path, data, require_boundary=True)
    assert counts["environment.start.json"] == 2
    (tmp_path / "environment.start.json").write_bytes(b"broken")
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        environment_record(tmp_path, data, require_boundary=True)


def test_overhead_preserves_endpoint_before_boundary_error(tmp_path, monkeypatch):
    data, environment_record = boundary_record_fixture(tmp_path, monkeypatch)
    (tmp_path / "environment.end.json").write_bytes(b"broken")
    (tmp_path / "boundary-observer.json").write_bytes(json_bytes({"collector": "unknown"}))
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        environment_record(tmp_path, data, require_boundary=True)
    (tmp_path / "environment.end.json").write_bytes(
        (tmp_path / "environment.start.json").read_bytes()
    )
    with pytest.raises(EvidenceError, match="boundary_observer_contract_mismatch"):
        environment_record(tmp_path, data, require_boundary=True)

"""Sealed sidecars must not hide damage across successive command reads."""

import hashlib
import shutil

import pytest

from inferyard.errors import EvidenceError
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes, read_json
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


def test_sidecars_are_parsed_and_hashed_once_per_read(tmp_path):
    root = fixture_run(tmp_path)
    reseal(root, "external-cpu.jsonl", b"")
    read_trial(root)
    (root / "environment.jsonl").write_bytes(b"changed")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        read_trial(root)


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
    boundary_files = {path.name: path.read_bytes() for path in root.iterdir() if path.is_file()}
    original_run = fixture_run(root / "seed")
    for path in original_run.iterdir():
        if path.is_file():
            shutil.copyfile(path, root / path.name)
    for name, raw in boundary_files.items():
        reseal(root, name, raw)
    captured.update(
        run=read_json(root / "run.json"),
        summary={
            "measurement_context": {"environment_qualification": {"eligible": True, "reasons": []}}
        },
    )
    return captured, environment_record


def test_overhead_boundary_shares_local_reads_and_stays_json(tmp_path, monkeypatch):
    data, environment_record = boundary_record_fixture(tmp_path, monkeypatch)
    assert environment_record(tmp_path, data, require_boundary=True)["qualification"]["eligible"]
    (tmp_path / "environment.start.json").write_bytes(b"broken")
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        environment_record(tmp_path, data, require_boundary=True)

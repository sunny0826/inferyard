"""Synthetic v2 producer/consumer and immutable staging regressions."""

import json
import sys

import pytest

from scripts import prepare_release_candidate as producer
from scripts import verify_release_candidate as gate
from tests.unit.test_release_fixture import COMMIT, build_fixture


def candidate(root):
    manifest, _, installed, _ = build_fixture(root / "original")
    producer.prepare(manifest, [installed], root / "candidate", COMMIT)
    path = root / "candidate/manifest.json"
    return path, json.loads(path.read_bytes())


def run_main(monkeypatch, manifest, stage):
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "verify_release_candidate",
            "--manifest",
            str(manifest),
            "--manifest-sha256",
            gate.sha(manifest.read_bytes()),
            "--source-commit",
            COMMIT,
            "--stage",
            str(stage),
        ],
    )
    gate.main()


@pytest.mark.parametrize("swap", [False, True])
def test_complete_staging_uses_approved_snapshot(tmp_path, monkeypatch, capsys, swap):
    manifest, data = candidate(tmp_path)
    original = gate.verify
    snapshots = original(manifest, gate.sha(manifest.read_bytes()), COMMIT)

    def verify(*args):
        files = original(*args)
        if swap:
            for record in data["artifacts"]:
                (manifest.parent / record["path"]).write_bytes(b"changed after verification")
        return files

    monkeypatch.setattr(gate, "verify", verify)
    stage = tmp_path / "staged"
    run_main(monkeypatch, manifest, stage)
    assert json.loads(capsys.readouterr().out)["verified"] is True
    for file in snapshots.values():
        assert (stage / file.name).read_bytes() == file.raw
    monkeypatch.setattr(gate, "verify", lambda *args: snapshots)
    with pytest.raises(FileExistsError):
        run_main(monkeypatch, manifest, stage)


def test_acceptance_swap_after_hash_uses_approved_bytes(tmp_path, monkeypatch, capsys):
    manifest, data = candidate(tmp_path)
    original = gate.checked_file

    def checked(root, record):
        approved = original(root, record)
        if record in data["installation_results"]:
            (root / record["path"]).write_bytes(b'{"status": "failed"}')
        return approved

    monkeypatch.setattr(gate, "checked_file", checked)
    run_main(monkeypatch, manifest, tmp_path / "stage")
    assert json.loads(capsys.readouterr().out)["verified"]


def test_acceptance_swap_before_hash_refuses_staging(tmp_path, monkeypatch):
    manifest, data = candidate(tmp_path)
    record = data["installation_results"][0]
    (manifest.parent / record["path"]).write_bytes(b'{"status": "failed"}')
    with pytest.raises(ValueError, match="candidate_hash_mismatch"):
        run_main(monkeypatch, manifest, tmp_path / "stage")
    assert not (tmp_path / "stage").exists()


@pytest.mark.parametrize("value", ["not_performed", None, [], 0, {}, "missing"])
def test_missing_installation_has_clear_classification(tmp_path, monkeypatch, value):
    manifest, data = candidate(tmp_path)
    if value == "missing":
        del data["installation_results"]
    else:
        data["installation_results"] = value
    manifest.write_text(json.dumps(data), encoding="utf-8")
    with pytest.raises(ValueError, match="installation_evidence_missing"):
        run_main(monkeypatch, manifest, tmp_path / "stage")
    assert not (tmp_path / "stage").exists()


def test_producer_copies_verified_snapshot_and_keeps_inputs(tmp_path, monkeypatch):
    manifest, build, installed, evidence = build_fixture(tmp_path / "original")
    before = manifest.read_bytes(), installed.read_bytes()
    original = producer.validate_build

    def validate(data, files, commit):
        original(data, files, commit)
        for record in build["artifacts"]:
            (manifest.parent / record["path"]).write_bytes(b"swapped")

    monkeypatch.setattr(producer, "validate_build", validate)
    result = producer.prepare(manifest, [installed], tmp_path / "candidate", COMMIT)
    path = tmp_path / "candidate/manifest.json"
    assert gate.verify(path, result["manifest_sha256"], COMMIT)
    assert (manifest.read_bytes(), installed.read_bytes()) == before
    assert result["platform_coverage"] == {
        "macos-arm64": "passed",
        "linux-x64": "not_verified",
        "windows-x64": "not_verified",
    }
    with pytest.raises(FileExistsError):
        # Valid retained inputs must still refuse an existing destination.
        producer.prepare(path.parent / "build/manifest.json", [installed], path.parent, COMMIT)


def test_artifact_swap_after_hash_does_not_reopen_for_metadata(tmp_path, monkeypatch, capsys):
    manifest, data = candidate(tmp_path)
    original = gate.checked_file
    reads = []

    def checked(root, record):
        approved = original(root, record)
        if record.get("role") in {"wheel", "sdist"}:
            (root / record["path"]).write_bytes(b"not even an archive")
            reads.append(record["role"])
        return approved

    monkeypatch.setattr(gate, "checked_file", checked)
    run_main(monkeypatch, manifest, tmp_path / "stage")
    assert reads == ["wheel", "sdist"]
    assert json.loads(capsys.readouterr().out)["verified"]
    for record in data["artifacts"][:2]:
        assert (
            gate.sha((tmp_path / "stage" / record["path"].split("/")[-1]).read_bytes())
            == record["sha256"]
        )

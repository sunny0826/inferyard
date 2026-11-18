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


def test_acceptance_swap_before_hash_refuses_staging(tmp_path, monkeypatch):
    manifest, data = candidate(tmp_path)
    record = data["installation_results"][0]
    (manifest.parent / record["path"]).write_bytes(b'{"status": "failed"}')
    with pytest.raises(ValueError, match="candidate_hash_mismatch"):
        run_main(monkeypatch, manifest, tmp_path / "stage")
    assert not (tmp_path / "stage").exists()

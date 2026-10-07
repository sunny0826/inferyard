import hashlib
import json

import pytest

from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.reporting.public_package import payloads, verify_public


def package(root, candidate, change_payload=None):
    candidate["candidate_id"] = (
        "candidate-" + hashlib.sha256(json_bytes(candidate)).hexdigest()[:32]
    )
    files = payloads(candidate)
    if change_payload:
        files[change_payload] = b"{}\n"
    for name, content in files.items():
        (root / name).write_bytes(content)
    (root / "manifest.json").write_bytes(
        json_bytes(
            {
                "policy": "public-summary.v5",
                "candidate_id": candidate["candidate_id"],
                "files": {
                    name: hashlib.sha256(content).hexdigest() for name, content in files.items()
                },
            }
        )
    )


@pytest.mark.parametrize("filename", ["redactions.json", "REPRODUCE.md"])
def test_rehashed_policy_payload_tampering_is_rejected(tmp_path, filename):
    package(tmp_path, {"policy": "public-summary.v5", "format_version": 5}, filename)
    with pytest.raises(EvidenceError, match="policy_payload"):
        verify_public(tmp_path)


@pytest.mark.parametrize("version", [2, True, "1"])
def test_rehashed_inconsistent_version_is_rejected(tmp_path, version):
    package(tmp_path, {"policy": "public-summary.v5", "format_version": version})
    with pytest.raises(EvidenceError, match="policy_mismatch"):
        verify_public(tmp_path)


@pytest.mark.parametrize("target", ["candidate", "manifest"])
@pytest.mark.parametrize("identity", ["missing", None, True])
def test_old_policy_cannot_hide_missing_or_mistyped_candidate_identity(tmp_path, target, identity):
    package(tmp_path, {"policy": "public-summary.v5", "format_version": 5})
    candidate = json.loads((tmp_path / "candidate.json").read_bytes())
    manifest = json.loads((tmp_path / "manifest.json").read_bytes())
    candidate.update(policy="public-summary.v4", format_version=4)
    manifest["policy"] = candidate["policy"]
    body = {key: value for key, value in candidate.items() if key != "candidate_id"}
    candidate["candidate_id"] = "candidate-" + hashlib.sha256(json_bytes(body)).hexdigest()[:32]
    manifest["candidate_id"] = candidate["candidate_id"]
    document = candidate if target == "candidate" else manifest
    if identity == "missing":
        document.pop("candidate_id")
    else:
        document["candidate_id"] = identity
    raw = json_bytes(candidate)
    (tmp_path / "candidate.json").write_bytes(raw)
    manifest["files"]["candidate.json"] = hashlib.sha256(raw).hexdigest()
    (tmp_path / "manifest.json").write_bytes(json_bytes(manifest))
    before = {p.name: p.read_bytes() for p in tmp_path.iterdir()}
    with pytest.raises(EvidenceError, match="public_candidate_identity_mismatch"):
        verify_public(tmp_path)
    assert {p.name: p.read_bytes() for p in tmp_path.iterdir()} == before

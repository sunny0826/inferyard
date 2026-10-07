import hashlib

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
                "policy": "public-summary.v1",
                "candidate_id": candidate["candidate_id"],
                "files": {
                    name: hashlib.sha256(content).hexdigest() for name, content in files.items()
                },
            }
        )
    )


@pytest.mark.parametrize("filename", ["redactions.json", "REPRODUCE.md"])
def test_rehashed_policy_payload_tampering_is_rejected(tmp_path, filename):
    package(tmp_path, {"policy": "public-summary.v1", "format_version": 1}, filename)
    with pytest.raises(EvidenceError, match="policy_payload"):
        verify_public(tmp_path)


@pytest.mark.parametrize("version", [2, True, "1"])
def test_rehashed_inconsistent_version_is_rejected(tmp_path, version):
    package(tmp_path, {"policy": "public-summary.v1", "format_version": version})
    with pytest.raises(EvidenceError, match="policy_mismatch"):
        verify_public(tmp_path)

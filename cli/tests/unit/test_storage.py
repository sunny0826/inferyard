import json

import pytest

from inferyard.contracts.redaction import Redactor
from inferyard.errors import EvidenceError
from inferyard.evidence.storage import EvidenceStore, verify_manifest


@pytest.mark.parametrize(
    "parts",
    [
        ["fixture-secret-7f39"],
        ["fixture-", "secret-", "7f39"],
        list("fixture-secret-7f39"),
        ["prefix fixture-sec", "ret-7f39 suffix"],
    ],
)
def test_secret_redaction_across_arbitrary_chunks(parts):
    redactor = Redactor(["fixture-secret-7f39"])
    stream = redactor.stream()
    persisted = "".join(stream.feed(part) for part in parts) + stream.feed("", final=True)
    assert "fixture-secret-7f39" not in persisted
    assert "[REDACTED]" in persisted
    assert stream.changed


def test_no_overwrite_and_manifest_classification(tmp_path):
    store = EvidenceStore(tmp_path, Redactor(["fixture-secret-7f39"]), run_id="first")
    store.snapshot("plan.json", {"content": "fixture-secret-7f39"})
    with pytest.raises(EvidenceError, match="atomic_write_failed"):
        store.snapshot("plan.json", {"overwritten": True})
    store.snapshot("summary.json", {"derived": True})
    store.seal()
    store.close()
    assert verify_manifest(store.path) == []
    for path in store.path.iterdir():
        assert b"fixture-secret-7f39" not in path.read_bytes()
    manifest = json.loads((store.path / "manifest.json").read_text())
    assert "manifest.json" not in manifest["files"]
    (store.path / "summary.json").write_text("tampered")
    assert verify_manifest(store.path) == ["derived_evidence_damaged:summary.json"]
    manifest["files"]["plan.json"]["derived"] = True
    (store.path / "manifest.json").write_text(json.dumps(manifest))
    (store.path / "plan.json").write_text("tampered")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        verify_manifest(store.path)
    with pytest.raises(FileExistsError):
        EvidenceStore(tmp_path, run_id="first")

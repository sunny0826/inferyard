import errno
import json
from pathlib import Path

import pytest

from inferyard.errors import EvidenceError, UnsupportedFormat
from inferyard.contracts.redaction import Redactor
from inferyard.evidence.storage import (
    EvidenceStore,
    atomic_bytes,
    local_file,
    read_jsonl,
    verify_manifest,
)


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


def make_journal(root):
    from inferyard.analysis.scoring import ScoringContext
    from inferyard.config.loader import load_config
    from inferyard.config.single_plan import compile_single_plan
    from inferyard.evidence.journal import TrialJournal

    loaded = load_config(Path(__file__).parents[1] / "fixtures/config/valid.toml")
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    return TrialJournal(
        root, plan, plan["trials"][0]["trial_id"], config, bundle,
        scoring_context=ScoringContext(),
    )

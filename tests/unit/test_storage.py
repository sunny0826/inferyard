import errno
import json
from pathlib import Path

import pytest

from inferyard.evidence.storage import (
    EvidenceError,
    EvidenceStore,
    Redactor,
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


def test_nonsecret_partial_prefix_is_preserved():
    stream = Redactor(["fixture-secret-7f39"]).stream()
    assert stream.feed("fixture-sec") == ""
    assert stream.feed("tion") == "fixture-section"
    assert stream.feed("fixture-", final=True) == "fixture-"
    assert not stream.changed


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
    # A malicious derived flag must not turn original damage into repairable damage.
    manifest["files"]["plan.json"]["derived"] = True
    (store.path / "manifest.json").write_text(json.dumps(manifest))
    (store.path / "plan.json").write_text("tampered")
    with pytest.raises(EvidenceError, match="invalid_manifest"):
        verify_manifest(store.path)
    with pytest.raises(FileExistsError):
        EvidenceStore(tmp_path, run_id="first")


def test_tail_is_recoverable_but_middle_corruption_is_not(tmp_path):
    path = tmp_path / "events.jsonl"
    path.write_bytes(b'{"seq":1}\n{"seq":2')
    rows, limits = read_jsonl(path)
    assert rows == [{"seq": 1}]
    assert limits == ["truncated_tail_at_byte:10"]
    path.write_bytes(b'{"seq":1}\nbroken\n{"seq":3}\n')
    with pytest.raises(EvidenceError, match="corrupt_jsonl"):
        read_jsonl(path)


def test_request_started_is_fsynced_before_return(tmp_path, monkeypatch):
    store = make_journal(tmp_path)
    seen = []
    original = store.flush

    def flush(*, sync=False):
        seen.append(sync)
        original(sync=sync)

    monkeypatch.setattr(store, "flush", flush)
    from inferyard.config.loader import load_config

    generation = load_config(
        Path(__file__).parents[1] / "fixtures/config/valid.toml"
    ).config.to_dict()["generation"]
    store.event(
        "request_started",
        "formal",
        "r1",
        {
            "case_id": "c1",
            "plan_index": 0,
            "attempt": 1,
            "body": {
                "model": "fixture",
                "messages": [{"role": "user", "content": "hello"}],
                "stream": True,
                "generation": generation,
            },
        },
    )
    assert seen == [True]
    assert read_jsonl(store.path / "events.jsonl")[0][0]["event_type"] == "request_started"
    store.close()


def test_write_failure_propagates_without_sealing(tmp_path, monkeypatch):
    store = make_journal(tmp_path)

    class FullDisk:
        closed = False

        def write(self, data):
            raise OSError(errno.ENOSPC, "disk full")

        def flush(self):
            pass

        def fileno(self):
            return -1

        def close(self):
            self.closed = True

    original = store._logs["events.jsonl"]
    store._logs["events.jsonl"] = FullDisk()
    with pytest.raises(EvidenceError, match="append_failed"):
        store.event("run_stopped", "finalizing", None, {"reason": "test"})
    assert not (store.path / "manifest.json").exists()
    store._logs["events.jsonl"] = original
    store.close()


def test_offline_reader_rejects_traversal_and_outside_symlinks(tmp_path):
    root = tmp_path / "run"
    root.mkdir()
    outside = tmp_path / "outside.json"
    outside.write_text("{}")
    from tests.helpers import symlink_or_skip

    symlink_or_skip(root / "link.json", outside)
    for name in ("../outside.json", str(outside), "link.json"):
        with pytest.raises(EvidenceError):
            local_file(root, name)
    target = root / "original.json"
    atomic_bytes(target, b"original")
    with pytest.raises(EvidenceError):
        atomic_bytes(target, b"replacement")
    assert target.read_bytes() == b"original"


def make_journal(root):
    from inferyard.config.loader import load_config
    from inferyard.config.single_plan import compile_single_plan
    from inferyard.evidence.journal import TrialJournal

    loaded = load_config(Path(__file__).parents[1] / "fixtures/config/valid.toml")
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    return TrialJournal(root, plan, plan["trials"][0]["trial_id"], config, bundle)


def test_no_new_request_copy_and_legacy_copy_remains_derived(tmp_path):
    store = EvidenceStore(tmp_path, run_id="modern")
    assert "requests.jsonl" not in store._logs
    assert not (store.path / "requests.jsonl").exists()
    store.seal()
    store.close()
    assert "requests.jsonl" not in json.loads((store.path / "manifest.json").read_text())["files"]
    legacy = EvidenceStore(tmp_path, run_id="legacy")
    (legacy.path / "requests.jsonl").write_bytes(b"{}\n")
    legacy.seal()
    legacy.close()
    (legacy.path / "requests.jsonl").write_bytes(b"damaged")
    assert verify_manifest(legacy.path) == ["derived_evidence_damaged:requests.jsonl"]

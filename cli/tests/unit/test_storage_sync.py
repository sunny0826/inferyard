"""A failed durable write cannot later become sealed evidence."""

import pytest

from inferyard.evidence import storage


def test_failed_fsync_preserves_prefix_and_blocks_same_writer_retry(tmp_path, monkeypatch):
    store = storage.EvidenceStore(tmp_path)
    store.flush(sync=True)
    store.observation("environment.jsonl", {"sample": 1})
    with monkeypatch.context() as patch:

        def failure(_fd):
            raise OSError("injected sync failure")

        patch.setattr(storage.os, "fsync", failure)
        with pytest.raises(storage.EvidenceError, match="evidence_flush_failed"):
            store.flush(sync=True)
    prefix = (store.path / "environment.jsonl").read_bytes()
    assert b'"sample":1' in prefix
    for operation in (
        lambda: store.flush(sync=True),
        lambda: store.observation("environment.jsonl", {"sample": 2}),
        store.seal,
    ):
        with pytest.raises(storage.EvidenceError, match="evidence_flush_failed"):
            operation()
    store.close()
    assert (store.path / "environment.jsonl").read_bytes() == prefix
    assert not (store.path / "manifest.json").exists()

"""Sync only changed streams, without weakening request durability or failure recovery."""

from types import SimpleNamespace

import pytest

from inferyard.evidence import storage
from inferyard.extensions.extension_evidence import ExtensionJournal
from inferyard.extensions.total_control_runtime import Observer


def test_empty_logs_initially_sync_but_unchanged_logs_do_not(tmp_path, monkeypatch):
    store = storage.EvidenceStore(tmp_path)
    seen = []
    monkeypatch.setattr(storage.os, "fsync", seen.append)
    store.flush(sync=True)
    assert len(seen) == 4
    assert not (store.path / "requests.jsonl").exists()
    seen.clear()
    store.flush(sync=True)
    assert seen == []
    store.close()


def test_buffer_flush_keeps_pending_fsync_and_only_changed_log_syncs(tmp_path, monkeypatch):
    store = storage.EvidenceStore(tmp_path)
    store.flush(sync=True)
    seen = []
    monkeypatch.setattr(storage.os, "fsync", seen.append)
    store.observation("environment.jsonl", {"sample": 1})
    store.flush(sync=False)
    assert seen == []
    store.flush(sync=True)
    assert seen == [store._logs["environment.jsonl"].fileno()]
    assert not store._pending_sync
    store.close()


def test_failed_fsync_keeps_dirty_state_for_recovery(tmp_path, monkeypatch):
    store = storage.EvidenceStore(tmp_path)
    store.flush(sync=True)
    store.observation("environment.jsonl", {"sample": 1})
    with monkeypatch.context() as patch:

        def failure(_fd):
            raise OSError("injected sync failure")

        patch.setattr(storage.os, "fsync", failure)
        with pytest.raises(storage.EvidenceError, match="evidence_flush_failed"):
            store.flush(sync=True)
    assert "environment.jsonl" in store._pending_sync
    store.flush(sync=True)
    assert not store._pending_sync
    store.close()


@pytest.mark.parametrize("boundary", ["event", "flush_due", "seal"])
def test_extension_terminal_sync_and_later_samples_wait_for_existing_boundary(
    tmp_path, monkeypatch, boundary
):
    clock = [1.0]
    monkeypatch.setattr(storage.time, "monotonic", lambda: clock[0])
    store = ExtensionJournal(tmp_path)
    store.flush(sync=True)
    seen = []
    real_fsync = storage.os.fsync

    def sync(fd):
        seen.append(fd)
        real_fsync(fd)

    monkeypatch.setattr(storage.os, "fsync", sync)
    observer = Observer("on", tmp_path, {}, {}, {})
    observer.store = store
    observer.sampler = SimpleNamespace(
        boundary=lambda capture: store.observation("schedule.jsonl", {"capture": capture})
    )
    observer.safety = SimpleNamespace(check=lambda: None)
    observer.trace = SimpleNamespace(check=lambda check, _: check())
    try:
        observer.after({"request_id": "request-1", "terminal_state": "completed"})
        assert seen == [
            store._logs["events.jsonl"].fileno(),
            store._logs["schedule.jsonl"].fileno(),
        ]
        assert b'"request_finished"' in (store.path / "events.jsonl").read_bytes()
        assert not (store.path / "requests.jsonl").exists()
        seen.clear()
        store.observation("environment.jsonl", {"sample": "after_terminal"})
        assert seen == []
        assert store._pending_sync == {"environment.jsonl"}
        if boundary == "event":
            store.event("request_started", "formal", "request-2", {})
        elif boundary == "flush_due":
            clock[0] += 0.5
            store.flush_due()
        else:
            store.seal()
        assert store._logs["environment.jsonl"].fileno() in seen
        assert not store._pending_sync
        assert b'"after_terminal"' in (store.path / "environment.jsonl").read_bytes()
    finally:
        store.close()

"""Mac rejection tests for deadline-aware Windows file manifests."""

import hashlib
import json
import time
from pathlib import Path

import pytest

from inferyard.contracts.validation import strict_json_loads
from inferyard.platforms import windows_lab_checks as checks
from inferyard.platforms import windows_lab_files as files
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_lab_api import override_windows_lab_api

ROOT = Path(__file__).parents[1] / "fixtures" / "windows_lab"
CONTENT = b"model-bytes"
STAMP = (2**64 - 1, bytes(range(16)), 32, len(CONTENT), 100, 200, 300)


class Store:
    def __init__(self):
        self.content = CONTENT
        self.stamp = STAMP
        self.reparse = set()
        self.checked = []
        self.closed = []
        self.path_reads = 0
        self.fd_reads = 0
        self.change_path_at = None
        self.change_fd_at = None
        self.offset = 0
        self.reads = 0
        self.clock = None
        self.expire_after_read = None

    def reject_reparse(self, path):
        self.checked.append(path)
        if path in self.reparse:
            raise OSError("reparse_point_rejected")

    def path_stamp(self, path):
        self.path_reads += 1
        return self._changed(self.path_reads, self.change_path_at)

    def open_read(self, path):
        self.handle = object()
        self.offset = 0
        return self.handle

    def descriptor_stamp(self, handle):
        assert handle is self.handle
        self.fd_reads += 1
        return self._changed(self.fd_reads, self.change_fd_at)

    def read_file(self, handle, size):
        assert handle is self.handle
        self.reads += 1
        if self.expire_after_read is not None and self.reads >= self.expire_after_read:
            self.clock["expire"] = True
        chunk = self.content[self.offset : self.offset + size]
        self.offset += len(chunk)
        return chunk

    def close_file(self, handle):
        if handle in self.closed:
            raise AssertionError("double close")
        self.closed.append(handle)

    def _changed(self, reads, at):
        if at is not None and reads >= at:
            return (self.stamp[0], b"\xff" + self.stamp[1][1:], *self.stamp[2:])
        return self.stamp


def _entry():
    return strict_json_loads(ROOT.joinpath("file_manifest.json").read_text(encoding="utf-8"))


def _soon():
    return time.monotonic() + 30


def test_manifest_fixture_matches_content_digest():
    assert _entry()[0]["sha256"] == hashlib.sha256(CONTENT).hexdigest()
    assert _entry()[0]["bytes"] == len(CONTENT)


def test_invalid_manifest_rejects_before_api():
    with pytest.raises(PreflightError, match="lab_windows_manifest_invalid"):
        files.verify_file_manifest([], deadline=_soon())
    with pytest.raises(PreflightError, match="lab_windows_manifest_invalid"):
        files.verify_file_manifest(
            [{"path": r"C:\a.bin", "role": "model", "bytes": True, "sha256": "ab" * 32}],
            deadline=_soon(),
        )
    with pytest.raises(PreflightError, match="lab_windows_manifest_invalid"):
        files.verify_file_manifest(
            [{"path": r"models\a.bin", "role": "model", "bytes": 1, "sha256": "ab" * 32}],
            deadline=_soon(),
        )
    huge = [{"path": r"C:\a.bin", "role": "model", "bytes": 64 * 1024**3, "sha256": "ab" * 32}]
    huge.append({"path": r"C:\b.bin", "role": "engine", "bytes": 1, "sha256": "cd" * 32})
    with pytest.raises(PreflightError, match="lab_windows_manifest_invalid"):
        files.verify_file_manifest(huge, deadline=_soon())


def test_windows_case_duplicate_is_rejected():
    first = _entry()[0]
    second = {**first, "path": r"c:/models/model.bin"}
    with pytest.raises(PreflightError, match="lab_windows_manifest_duplicate"):
        files.verify_file_manifest([first, second], deadline=_soon())


def test_success_returns_native_stamp_without_calling_file_hash(monkeypatch):
    def forbidden(*_args, **_kwargs):
        raise AssertionError("deadline-free file_hash")

    monkeypatch.setattr(
        "inferyard.platforms.engine_fit_windows_files.file_hash", forbidden, raising=False
    )
    store = Store()
    original = _entry()
    snapshot = json.loads(json.dumps(original))
    with override_windows_lab_api(store):
        verified = files.verify_file_manifest(original, deadline=_soon())
    assert original == snapshot
    assert verified[0]["native_stamp"] == [
        2**64 - 1,
        bytes(range(16)).hex(),
        32,
        11,
        100,
        200,
        300,
    ]
    assert verified[0]["path"] == original[0]["path"]
    assert store.closed == [store.handle]
    assert store.checked[0] == "C:\\"
    assert store.checked[-1] == r"C:\models\model.bin"


def test_reparse_on_ancestor_stops_before_the_leaf():
    store = Store()
    store.reparse.add(r"C:\models")
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=_soon())
    assert caught.value.args == ("lab_windows_reparse_rejected",)
    assert store.checked == ["C:\\", r"C:\models"]
    assert store.closed == []
    assert r"C:\models\model.bin" not in str(caught.value)


def test_same_size_and_mtime_replacement_is_rejected():
    store = Store()
    store.change_path_at = 2
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=_soon())
    assert caught.value.args == ("lab_windows_file_changed",)
    assert store.stamp[3] == STAMP[3] and store.stamp[5] == STAMP[5]
    assert store.closed == [store.handle]


def test_change_during_read_is_rejected():
    store = Store()
    store.change_fd_at = 2
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=_soon())
    assert caught.value.args == ("lab_windows_file_changed",)
    assert store.closed == [store.handle]


def _hold_clock(store, monkeypatch):
    store.clock = {"expire": False}

    def monotonic():
        return 100.0 if store.clock["expire"] else 1.0

    monkeypatch.setattr(checks.time, "monotonic", monotonic)


def test_chunk_deadline_rejects_without_success(monkeypatch):
    store = Store()
    store.expire_after_read = 1
    store.clock = {"expire": False}
    monkeypatch.setattr(files, "READ_CHUNK_BYTES", 1)

    def monotonic():
        return 100.0 if store.clock["expire"] else 1.0

    monkeypatch.setattr(checks.time, "monotonic", monotonic)
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=50.0)
    assert caught.value.args == ("lab_windows_deadline_exceeded",)
    assert store.closed == [store.handle]
    assert store.reads == 1


def test_hash_mismatch_and_size_mismatch():
    store = Store()
    entry = _entry()
    entry[0] = {**entry[0], "sha256": "ab" * 32}
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(entry, deadline=_soon())
    assert caught.value.args == ("lab_windows_hash_mismatch",)

    store = Store()
    entry = _entry()
    entry[0] = {**entry[0], "bytes": len(CONTENT) + 1}
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(entry, deadline=_soon())
    assert caught.value.args == ("lab_windows_file_identity_mismatch",)


def test_eof_read_past_deadline_is_rejected(monkeypatch):
    store = Store()
    store.expire_after_read = 2
    _hold_clock(store, monkeypatch)
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=50.0)
    assert caught.value.args == ("lab_windows_deadline_exceeded",)
    assert store.reads == 2 and store.offset == len(CONTENT)
    assert files.READ_CHUNK_BYTES > len(CONTENT)
    assert store.closed == [store.handle]


def test_final_path_stamp_past_deadline_is_rejected(monkeypatch):
    store = Store()
    _hold_clock(store, monkeypatch)
    original = store.path_stamp

    def path_stamp(path):
        stamp = original(path)
        if store.path_reads >= 2:
            store.clock["expire"] = True
        return stamp

    store.path_stamp = path_stamp
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=50.0)
    assert caught.value.args == ("lab_windows_deadline_exceeded",)
    assert store.path_reads == 2
    assert store.closed == [store.handle]


def test_close_past_deadline_is_rejected(monkeypatch):
    store = Store()
    _hold_clock(store, monkeypatch)
    original = store.close_file

    def close_file(handle):
        original(handle)
        store.clock["expire"] = True

    store.close_file = close_file
    with override_windows_lab_api(store), pytest.raises(PreflightError) as caught:
        files.verify_file_manifest(_entry(), deadline=50.0)
    assert caught.value.args == ("lab_windows_deadline_exceeded",)
    assert store.closed == [store.handle]


@pytest.mark.parametrize(
    "size,seconds", [(1, 181), (32 * 1024**2, 181), (2 * 1024**3, 244), (64 * 1024**3, 2228)]
)
def test_hash_budget_scales_and_stays_bounded(size, seconds):
    assert files.manifest_hash_seconds([{**_entry()[0], "bytes": size}]) == seconds


@pytest.mark.parametrize("size", [True, 0, 64 * 1024**3 + 1])
def test_scaled_hash_budget_does_not_accept_invalid_manifest(size):
    with pytest.raises(PreflightError, match="lab_windows_manifest_invalid"):
        files.manifest_hash_seconds([{**_entry()[0], "bytes": size}])


def test_scaled_budget_still_rejects_read_past_deadline(monkeypatch):
    store = Store()
    clock = [10.0]
    monkeypatch.setattr(checks.time, "monotonic", lambda: clock[0])
    deadline = clock[0] + files.manifest_hash_seconds(_entry())
    original = store.read_file

    def read(handle, size):
        value = original(handle, size)
        clock[0] = deadline
        return value

    store.read_file = read
    with override_windows_lab_api(store), pytest.raises(PreflightError, match="deadline_exceeded"):
        files.verify_file_manifest(_entry(), deadline=deadline)
    assert store.reads == 1 and store.closed == [store.handle]

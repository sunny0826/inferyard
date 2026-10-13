"""文件 worker 注入回归。不打开真实文件，不调用 execute_io 的 CLI 许可。"""

import ast
import base64
import builtins
import copy
import hashlib
import json
from pathlib import Path

import pytest

import scripts.ninfer_source_host.file_io as file_io
from scripts.ninfer_source_host.file_io import execute_io, main, validate_io_request
from scripts.ninfer_source_host.file_io_codec import HASH_LIMIT, IoError, loads_strict

ROOT = Path(__file__).resolve().parents[2]
HOST = "01234567-89ab-4cde-8f01-23456789abcd"
JOB = "abcdef01-2345-4678-9abc-def012345678"
PATH = "C:\\lab\\note.txt"
EMPTY_SHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
ANCESTORS = ("C:\\", "C:\\lab", "C:\\lab\\note.txt")


class Clock:
    def __init__(self, start=1, deadline=50):
        self.t = start
        self.deadline = deadline
        self.mode = "run"

    def __call__(self):
        if self.mode == "jump":
            self.t = self.deadline
        elif self.mode == "arm":
            self.mode = "jump"
        return self.t


class Mem:
    def __init__(
        self,
        files=None,
        reparse=(),
        clock=None,
        expire=None,
        fail_read=False,
        fail_close=False,
        arm_final=False,
    ):
        self.files = {key: bytearray(value) for key, value in (files or {}).items()}
        self.reparse = set(reparse)
        self.clock = clock
        self.expire = expire
        self.fail_read = fail_read
        self.fail_close = fail_close
        self.arm_final = arm_final
        self.calls = []
        self.closed = []
        self.handles = {}
        self.next_id = 1

    def _hit(self, name):
        self.calls.append(name)
        if self.expire == name and self.clock is not None:
            self.clock.t = self.clock.deadline

    def is_reparse(self, path):
        self.calls.append(("reparse", path))
        return path in self.reparse

    def open_read(self, path):
        self._hit("open")
        if path not in self.files:
            raise OSError("missing")
        return self._handle(bytes(self.files[path]))

    def read(self, handle, size):
        self._hit("read")
        if self.fail_read:
            raise OSError("read")
        state = self.handles[handle]
        chunk = state["data"][state["off"] : state["off"] + size]
        state["off"] += len(chunk)
        return chunk

    def create_new(self, path):
        self._hit("create")
        if path in self.files:
            error = FileExistsError("exists")
            raise error
        self.files[path] = bytearray()
        return self._handle(self.files[path])

    def write(self, handle, data):
        self._hit("write")
        self.handles[handle]["data"].extend(data)
        return len(data)

    def flush(self, handle):
        self._hit("flush")

    def fsync(self, handle):
        self._hit("fsync")

    def close(self, handle):
        self._hit("close")
        self.closed.append(handle)
        if self.arm_final and self.clock is not None:
            self.clock.mode = "arm"
        if self.fail_close:
            raise OSError("close")

    def _handle(self, data):
        handle = self.next_id
        self.next_id += 1
        self.handles[handle] = {"data": data, "off": 0}
        return handle


def digest(payload):
    return hashlib.sha256(payload).hexdigest()


def request(operation, payload=b"hi", **over):
    body = {
        "protocol": "ninfer-source-host/1",
        "host_id": HOST,
        "execution_sha256": "ab" * 32,
        "scripts_sha256": "cd" * 32,
        "stage": 45,
        "job_id": JOB,
        "task_end_ticks": 50,
        "total_end_ticks": 60,
        "frequency": 1,
        "operation": operation,
        "path": PATH,
        "expected_bytes": len(payload),
        "expected_sha256": digest(payload),
        "data_b64": None,
    }
    if operation == "write_new":
        body["data_b64"] = base64.b64encode(payload).decode("ascii")
    body.update(over)
    return body


def names(api):
    return [item for item in api.calls if isinstance(item, str)]


def reparse_paths(api):
    return [item[1] for item in api.calls if isinstance(item, tuple)]


def run(operation, payload=b"hi", files=None, **kw):
    if files is None:
        files = {} if operation == "write_new" else {PATH: payload}
    clock = kw.pop("clock", None) or Clock()
    api = Mem(files, clock=clock, **kw)
    result = execute_io(request(operation, payload), io_api=api, now_ticks=clock)
    return api, result


def test_worker_sources_do_not_touch_the_filesystem():
    folder = ROOT / "scripts" / "ninfer_source_host"
    for name in ("file_io.py", "file_io_codec.py"):
        source = (folder / name).read_text()
        tree = ast.parse(source)
        imported = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported.extend(alias.name.split(".")[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom) and node.module:
                imported.append(node.module.split(".")[0])
        assert {"os", "pathlib", "subprocess", "ctypes"}.isdisjoint(imported)
        assert "open(" not in source


def test_type_and_identity_errors_do_not_touch_io():
    api = Mem()
    clock = Clock()
    cases = (
        ([], "io_request"),
        (request("read") | {"stage": True}, "io_request"),
        (request("read") | {"protocol": "other"}, "io_identity"),
        (request("read") | {"stage": 44}, "io_identity"),
        (request("read") | {"expected_bytes": True}, "io_request"),
        (request("read") | {"frequency": False}, "io_request"),
        (request("read") | {"total_end_ticks": 50}, "io_request"),
        (request("hash", expected_bytes=HASH_LIMIT + 1), "io_request"),
        (request("write_new", expected_bytes=8193), "io_request"),
    )
    for value, code in cases:
        before = list(api.calls)
        result = execute_io(value, io_api=api, now_ticks=clock)
        assert result["primary_error"] == code
        assert result["bytes"] is None
        assert result["sha256"] is None
        assert api.calls == before


def test_validate_copies_and_does_not_mutate():
    raw = request("read")
    snapshot = copy.deepcopy(raw)
    copied = validate_io_request(raw)
    assert copied == snapshot
    assert copied is not raw
    raw["stage"] = True
    assert copied["stage"] == 45


@pytest.mark.parametrize(
    ("operation", "step", "absent"),
    [
        ("read", "open", "read"),
        ("read", "read", "flush"),
        ("read", "flush", "fsync"),
        ("read", "fsync", None),
        ("read", "close", None),
        ("write_new", "create", "write"),
        ("write_new", "write", "flush"),
        ("write_new", "flush", "fsync"),
        ("write_new", "fsync", None),
        ("write_new", "close", None),
    ],
)
def test_deadline_on_each_file_step_still_closes(operation, step, absent):
    api, result = run(operation, expire=step)
    assert result["primary_error"] == "io_deadline"
    assert result["data_b64"] is None
    assert names(api).count("close") == 1
    assert step in names(api)
    if absent is not None:
        assert absent not in names(api)
    if step == "fsync":
        assert names(api)[-2:] == ["fsync", "close"]


def test_final_sample_after_close_rejects_success_and_keeps_known_bytes():
    api, result = run("read", arm_final=True)
    assert result["primary_error"] == "io_deadline"
    assert result["data_b64"] is None
    assert result["bytes"] == 2
    assert result["sha256"] == digest(b"hi")
    assert result["cleanup_errors"] == []
    assert names(api).count("close") == 1
    written, created = run("write_new", arm_final=True)
    assert created["primary_error"] == "io_deadline"
    assert created["data_b64"] is None
    assert created["bytes"] == 2
    assert bytes(written.files[PATH]) == b"hi"


def test_primary_error_and_close_error_are_both_kept():
    api, result = run("read", fail_read=True, fail_close=True)
    assert result["primary_error"] == "io_failure"
    assert result["cleanup_errors"] == ["io_cleanup"]
    assert result["bytes"] is None
    assert result["sha256"] is None
    assert names(api).count("close") == 1
    assert "write" not in names(api)


def test_create_new_refuses_existing_file_without_writing():
    api = Mem({PATH: bytearray(b"keep")}, clock=Clock())
    result = execute_io(request("write_new", b"new"), io_api=api, now_ticks=api.clock)
    assert result["primary_error"] == "io_path"
    assert result["bytes"] is None
    assert "write" not in names(api)
    assert "create" in names(api)
    assert api.files[PATH] == b"keep"
    assert names(api).count("close") == 0


def test_reparse_ancestor_blocks_open_and_create():
    api, result = run("read", reparse={"C:\\lab"})
    assert result["primary_error"] == "io_path"
    assert "open" not in names(api)
    assert reparse_paths(api) == ["C:\\", "C:\\lab"]
    leaf, blocked = run("read", reparse={PATH})
    assert blocked["primary_error"] == "io_path"
    assert reparse_paths(leaf) == list(ANCESTORS)
    assert "open" not in names(leaf)
    unc = "\\\\server\\share\\dir\\note.txt"
    raw = request("write_new", path=unc)
    store = Mem(reparse={"\\\\server\\share"}, clock=Clock())
    refused = execute_io(raw, io_api=store, now_ticks=store.clock)
    assert refused["primary_error"] == "io_path"
    assert reparse_paths(store) == ["\\\\server\\share"]
    assert "create" not in names(store)


@pytest.mark.parametrize(
    "kind",
    ["size", "sha", "b64", "device", "relative", "parent", "duplicate"],
)
def test_size_sha_base64_path_and_duplicate_keys_do_nothing(kind):
    api = Mem(clock=Clock())
    if kind == "size":
        payload = request("read", expected_bytes=8193)
    elif kind == "sha":
        payload = request("write_new", b"hi", expected_sha256="ab" * 32)
    elif kind == "b64":
        payload = request("write_new", b"hi", data_b64="!!!!")
    elif kind == "device":
        payload = request("read", path="\\\\?\\C:\\lab\\note.txt")
    elif kind == "relative":
        payload = request("read", path="lab\\note.txt")
    elif kind == "parent":
        payload = request("read", path="C:\\lab\\..\\note.txt")
    else:
        payload = request("write_new", b"hi", data_b64="A" * 10925)
    result = execute_io(payload, io_api=api, now_ticks=api.clock)
    assert result["primary_error"] in {"io_request", "io_path"}
    assert result["bytes"] is None
    assert api.calls == []


def test_duplicate_json_keys_and_non_finite_numbers_are_rejected():
    for text in (
        '{"a": 1, "a": 2}',
        '{"outer": {"a": 1, "a": 2}}',
        '{"a": NaN}',
        '{"a": Infinity}',
        '{"a": -Infinity}',
    ):
        with pytest.raises(IoError) as caught:
            loads_strict(text)
        assert caught.value.code == "io_request"


def test_unknown_byte_count_is_null_and_partial_is_not_zeroed():
    missing, failed = run("read", files={})
    assert failed["bytes"] is None
    assert failed["sha256"] is None
    assert failed["bytes"] != 0
    assert names(missing).count("close") == 0

    class Partial(Mem):
        def __init__(self):
            super().__init__({PATH: b"hi"}, clock=Clock())
            self.reads = 0

        def read(self, handle, size):
            self._hit("read")
            self.reads += 1
            if self.reads == 1:
                return b"h"
            raise OSError("read")

    api = Partial()
    result = execute_io(request("read"), io_api=api, now_ticks=api.clock)
    assert result["primary_error"] == "io_failure"
    assert result["bytes"] == 1
    assert result["sha256"] is None
    assert result["bytes"] != 0
    assert names(api).count("close") == 1

    api, result = run("read", b"", files={PATH: b""})
    assert result["primary_error"] is None
    assert result["bytes"] == 0
    assert result["sha256"] == EMPTY_SHA
    assert result["data_b64"] == ""


@pytest.mark.parametrize("operation", ["read", "hash", "write_new"])
def test_legal_injected_operations(operation):
    payload = b"hi"
    api, result = run(operation, payload)
    assert result["primary_error"] is None
    assert result["cleanup_errors"] == []
    assert result["bytes"] == len(payload)
    assert result["sha256"] == digest(payload)
    assert set(result) == {
        "protocol",
        "host_id",
        "execution_sha256",
        "scripts_sha256",
        "stage",
        "job_id",
        "operation",
        "bytes",
        "sha256",
        "data_b64",
        "primary_error",
        "cleanup_errors",
    }
    assert names(api).count("close") == 1
    assert reparse_paths(api) == list(ANCESTORS)
    if operation == "read":
        assert base64.b64decode(result["data_b64"]) == payload
        assert names(api) == ["open", "read", "read", "flush", "fsync", "close"]
    elif operation == "hash":
        assert result["data_b64"] is None
        assert names(api) == ["open", "read", "read", "flush", "fsync", "close"]
    else:
        assert result["data_b64"] is None
        assert bytes(api.files[PATH]) == payload
        assert names(api) == ["create", "write", "flush", "fsync", "close"]


def test_main_always_refuses_without_real_io(monkeypatch, tmp_path):
    def refused(*_args, **_kwargs):
        raise AssertionError("executed")

    def opened(*_args, **_kwargs):
        raise AssertionError("open")

    monkeypatch.setattr(file_io, "execute_io", refused)
    monkeypatch.setattr(builtins, "open", opened)
    payload = base64.b64encode(json.dumps(request("read")).encode()).decode()
    assert main(["worker", payload]) == 2
    duplicate = base64.b64encode(b'{"a":1,"a":2}').decode()
    assert main(["worker", duplicate]) == 2
    assert main(["worker", base64.b64encode(b"x" * 12289).decode()]) == 2
    assert main(["worker"]) == 2
    assert main(None) == 2
    assert list(tmp_path.iterdir()) == []


def test_clock_regression_does_not_open():
    class Backward:
        def __init__(self):
            self.values = iter((5, 4))

        def __call__(self):
            return next(self.values)

    api = Mem({PATH: b"hi"})
    result = execute_io(request("read"), io_api=api, now_ticks=Backward())
    assert result["primary_error"] == "io_request"
    assert api.calls == []


def test_input_object_is_not_mutated():
    raw = request("hash")
    snapshot = copy.deepcopy(raw)
    execute_io(raw, io_api=Mem({PATH: b"hi"}), now_ticks=Clock())
    assert raw == snapshot

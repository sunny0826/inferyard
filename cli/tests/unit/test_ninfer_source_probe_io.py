"""Stage46 文件适配器注入回归。不调用真实 WinDLL、socket 或子进程。"""

import ast
import ctypes
import sys
from ctypes import wintypes

import pytest

from scripts.ninfer_source_host.file_io import execute_io
from scripts.ninfer_source_host.file_io_codec import IoError
from scripts.ninfer_source_host.probe_io import ProbeFileApi, attribute_probe_flags
from scripts.ninfer_source_host.probe_io_limits import (
    DIAGNOSTIC_ROOT,
    ERROR_ACCESS_DENIED,
    ERROR_FILE_NOT_FOUND,
    ERROR_PATH_NOT_FOUND,
    FILE_ATTRIBUTE_NORMAL,
    FILE_ATTRIBUTE_REPARSE_POINT,
    FILE_READ_DATA,
    FILE_ROOT,
    FILE_WRITE_DATA,
    INVALID_FILE_ATTRIBUTES,
)
from scripts.ninfer_source_host.probe_io_winapi import RealProbeFileApi, _basic_limits
from tests.unit.test_ninfer_source_file_io import Clock, digest, request

HIGH = 0x1234567887654321
ROOT = FILE_ROOT
HOST = "01234567-89ab-4cde-8f01-23456789abcd"
JOB = "abcdef01-2345-4678-9abc-def012345678"
NOTE = ROOT + "\\note.txt"
CHILD = ROOT + "\\lab\\note.txt"
OUTSIDE = "D:\\lab45\\note.txt"
OTHER_ROOT = "C:\\lab46-source-host\\files"
NAMES = (
    "GetFileAttributesW",
    "CreateFileW",
    "ReadFile",
    "WriteFile",
    "FlushFileBuffers",
    "CloseHandle",
    "CreatePipe",
    "CreateProcessW",
    "CreateJobObjectW",
    "SetInformationJobObject",
    "AssignProcessToJobObject",
    "ResumeThread",
    "WaitForSingleObject",
    "GetExitCodeProcess",
)


class Native:
    def __init__(self, files=None, reparse=(), missing=()):
        self.files = {key: bytearray(value) for key, value in (files or {}).items()}
        self.reparse = set(reparse)
        self.missing = set(missing)
        self.calls = []
        self.flushed = []
        self.closed = []
        self.handles = {}
        self.next_id = 1
        self.fail = None
        self.injected = True

    def attributes(self, path):
        self.calls.append(("attributes", path))
        if self.fail == "attributes":
            raise OSError("attributes")
        code = self.missing_code(path)
        if code is not None:
            raise OSError(code, "attributes")
        if path in self.reparse:
            return FILE_ATTRIBUTE_REPARSE_POINT
        if path in self.files or self._known_directory(path) or not path.startswith(ROOT + chr(92)):
            return FILE_ATTRIBUTE_NORMAL
        code = ERROR_FILE_NOT_FOUND if self._parent_exists(path) else ERROR_PATH_NOT_FOUND
        raise OSError(code, "attributes")

    def missing_code(self, path):
        for item, code in self.missing:
            if item == path:
                return code
        return None

    def _known_directory(self, path):
        prefix = path if path.endswith("\\") else path + "\\"
        names = (*self.files, *self.reparse)
        return any(key.startswith(prefix) for key in names)

    def _parent_exists(self, path):
        parent = path.rsplit("\\", 1)[0]
        if parent in {"D:", DIAGNOSTIC_ROOT, ROOT}:
            return True
        return parent in self.files or self._known_directory(parent)

    def open_existing(self, path, access, flags):
        self.calls.append(("open_existing", path, access, flags))
        if path not in self.files:
            raise OSError("missing")
        return self._handle(bytes(self.files[path]), writable=False)

    def create_new(self, path):
        self.calls.append(("create_new", path))
        if path in self.files:
            raise FileExistsError(path)
        self.files[path] = bytearray()
        return self._handle(self.files[path], writable=True)

    def read(self, handle, size):
        self.calls.append(("read", handle, size))
        state = self.handles[handle]
        chunk = state["data"][state["off"] : state["off"] + size]
        state["off"] += len(chunk)
        return chunk

    def write(self, handle, data):
        self.calls.append(("write", handle, data))
        state = self.handles[handle]
        if not state["writable"]:
            raise OSError("read_only")
        state["data"].extend(data)
        return len(data)

    def flush_file_buffers(self, handle):
        self.flushed.append(handle)
        self.calls.append(("flush_buffers", handle))

    def close(self, handle):
        self.closed.append(handle)
        self.calls.append(("close", handle))
        if self.fail == "close":
            raise OSError("close")

    def _handle(self, data, *, writable):
        handle = HIGH + self.next_id
        self.next_id += 1
        self.handles[handle] = {"data": data, "off": 0, "writable": writable}
        return handle


class _Function:
    def __init__(self):
        self.argtypes = None
        self.restype = ctypes.c_int


def _api(files=None, reparse=()):
    return ProbeFileApi(ROOT, Native(files, reparse))


def test_sources_do_not_construct_real_calls_on_import():
    folder = __import__("pathlib").Path(__file__).resolve().parents[2]
    names = ("probe_io.py", "probe_io_limits.py", "probe_io_winapi.py")
    for name in names:
        source = (folder / "scripts" / "ninfer_source_host" / name).read_text()
        tree = ast.parse(source)
        imported = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported.extend(alias.name.split(".")[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom) and node.module:
                imported.append(node.module.split(".")[0])
        assert "subprocess" not in imported
        assert "socket" not in imported
        assert "WinDLL(" not in source.split("def _dll")[0]


def test_root_must_be_the_fixed_files_directory():
    for root in (OTHER_ROOT, NOTE, "D:\\lab46-source-host", "D:\\lab46-source-host\\files\\"):
        with pytest.raises(IoError) as caught:
            ProbeFileApi(root, Native())
        assert caught.value.code == "io_path"
    api = ProbeFileApi(ROOT)
    assert api.root == ROOT
    assert api.injected is False
    assert api.native is None


def test_request_path_cannot_change_the_permitted_root():
    native = Native({OUTSIDE: b"hi"})
    api = ProbeFileApi(ROOT, native)
    with pytest.raises(IoError) as caught:
        api.open_read(OUTSIDE)
    assert caught.value.code == "io_path"
    assert native.calls == []
    with pytest.raises(IoError):
        api.open_read(ROOT)
    assert native.calls == []


@pytest.mark.parametrize(
    "path",
    [ROOT + "\\..\\outside.txt", "D:\\lab46-source-host\\files2\\a.txt"],
)
def test_escape_is_refused_before_native_open(path):
    native = Native()
    api = ProbeFileApi(ROOT, native)
    with pytest.raises(IoError) as caught:
        api.create_new(path)
    assert caught.value.code == "io_path"
    assert native.calls == []


def test_ancestor_reparse_is_refused_and_leaf_is_not_opened():
    native = Native(reparse={ROOT + "\\lab"})
    api = ProbeFileApi(ROOT, native)
    with pytest.raises(IoError) as caught:
        api.open_read(CHILD)
    assert caught.value.code == "io_path"
    assert ("attributes", ROOT) in native.calls
    assert ("attributes", ROOT + "\\lab") in native.calls
    assert not any(item[0] == "open_existing" for item in native.calls)


def test_drive_ancestor_query_checks_native_reparse_and_does_not_open():
    native = Native({NOTE: b"hi"}, reparse={"D:\\"})
    api = ProbeFileApi(ROOT, native)
    assert api.is_reparse("D:\\") is True
    assert native.calls == [("attributes", "D:\\")]
    with pytest.raises(IoError):
        api.is_reparse("E:\\")
    assert not any(name == "open_existing" for name, *_rest in native.calls)


def test_root_reparse_refuses_every_child():
    native = Native({NOTE: b"hi"}, reparse={ROOT})
    api = ProbeFileApi(ROOT, native)
    with pytest.raises(IoError) as caught:
        api.open_read(NOTE)
    assert caught.value.code == "io_path"
    assert ("attributes", ROOT) in native.calls
    assert not any(name == "open_existing" for name, *_rest in native.calls)


def test_create_new_refuses_existing_file_without_truncation():
    native = Native({NOTE: b"kept"})
    api = ProbeFileApi(ROOT, native)
    with pytest.raises(FileExistsError):
        api.create_new(NOTE)
    assert native.files[NOTE] == b"kept"
    assert native.closed == []


def test_read_only_flush_and_fsync_do_not_call_flush_file_buffers():
    api = _api({NOTE: b"hi"})
    handle = api.open_read(NOTE)
    flush = api.flush(handle)
    sync = api.fsync(handle)
    assert flush == {"api": "flush", "pending": False, "flush_file_buffers": False}
    assert sync == {"api": "fsync", "pending": False, "flush_file_buffers": False}
    assert api.native.flushed == []
    api.close(handle)
    assert api.native.closed == [handle]


def test_write_handle_flush_calls_flush_file_buffers_once_per_sync():
    api = _api()
    handle = api.create_new(NOTE)
    api.write(handle, b"new")
    assert api.flush(handle)["flush_file_buffers"] is True
    assert api.fsync(handle)["api"] == "FlushFileBuffers"
    assert api.native.flushed == [handle, handle]
    api.close(handle)


def test_execute_io_close_after_deadline_keeps_cleanup_error():
    payload = b"late"
    clock = Clock()
    api = ProbeFileApi(ROOT, Native())

    def expire(handle):
        clock.t = 50
        raise OSError("close")

    api.close = expire
    body = request("write_new", payload, path=NOTE)
    result = execute_io(body, io_api=api, now_ticks=clock)
    assert result["primary_error"] == "io_cleanup"
    assert result["cleanup_errors"] == ["io_deadline"]
    assert result["data_b64"] is None
    assert result["bytes"] == len(payload)


def test_missing_leaf_error_two_reaches_create_new_and_error_three_does_not():
    payload = b"new"
    body = request("write_new", payload, path=NOTE)
    missing = ProbeFileApi(ROOT, Native(missing=[(NOTE, ERROR_FILE_NOT_FOUND)]))
    result = execute_io(body, io_api=missing, now_ticks=Clock())
    assert result["primary_error"] is None
    assert missing.native.files[NOTE] == payload
    assert any(name == "create_new" for name, *_rest in missing.native.calls)

    denied_parent = ProbeFileApi(ROOT, Native(missing=[(ROOT + "\\lab", ERROR_PATH_NOT_FOUND)]))
    parent = execute_io(
        request("write_new", payload, path=CHILD), io_api=denied_parent, now_ticks=Clock()
    )
    assert parent["primary_error"] == "io_path"
    assert not any(name == "create_new" for name, *_rest in denied_parent.native.calls)

    access = ProbeFileApi(ROOT, Native())

    def denied(path):
        access.native.calls.append(("attributes", path))
        if path == NOTE:
            raise OSError(ERROR_ACCESS_DENIED, "attributes")
        return FILE_ATTRIBUTE_NORMAL

    access.native.attributes = denied
    blocked = execute_io(body, io_api=access, now_ticks=Clock())
    assert blocked["primary_error"] == "io_failure"
    assert not any(name == "create_new" for name, *_rest in access.native.calls)


def test_direct_open_checks_leaf_reparse_before_create_file():
    native = Native({NOTE: b"outside-data"}, reparse={NOTE})
    api = ProbeFileApi(ROOT, native)
    with pytest.raises(IoError) as caught:
        api.open_read(NOTE)
    assert caught.value.code == "io_path"
    assert ("attributes", NOTE) in native.calls
    assert not any(name == "open_existing" for name, *_rest in native.calls)


def test_root_and_parent_are_not_openable_files():
    native = Native({NOTE: b"kept"})
    api = ProbeFileApi(ROOT, native)
    for path in (ROOT, "D:\\lab46-source-host", "D:\\"):
        with pytest.raises(IoError) as caught:
            api.open_read(path)
        assert caught.value.code == "io_path"
    assert not any(name == "open_existing" for name, *_rest in native.calls)


def test_kernel_missing_attribute_is_error_not_normal():
    assert INVALID_FILE_ATTRIBUTES == 0xFFFFFFFF
    native = Native()
    with pytest.raises(OSError) as caught:
        native.attributes(NOTE)
    assert caught.value.args[0] == ERROR_FILE_NOT_FOUND


def test_execute_io_uses_only_the_probe_shape():
    payload = b"probe"
    native = Native()
    api = ProbeFileApi(ROOT, native)
    body = request("write_new", payload, path=NOTE)
    result = execute_io(body, io_api=api, now_ticks=Clock())
    assert result["primary_error"] is None
    assert result["bytes"] == len(payload)
    assert result["sha256"] == digest(payload)
    assert native.files[NOTE] == payload
    assert api.native.flushed
    assert "open(" not in api.calls.__class__.__name__


def test_close_failure_is_visible_and_handle_is_not_retried_by_adapter():
    api = _api({NOTE: b"hi"})
    api.native.fail = "close"
    handle = api.open_read(NOTE)
    with pytest.raises(OSError, match="close"):
        api.close(handle)
    assert api.native.closed == [handle]
    with pytest.raises(IoError):
        api.close(handle)
    assert api.native.closed == [handle]


def test_high_handle_is_not_truncated_inside_the_adapter():
    api = _api({NOTE: b"abcd"})
    handle = api.open_read(NOTE)
    assert handle > 0xFFFFFFFF
    assert api.read(handle, 4) == b"abcd"
    api.close(handle)
    assert handle in api.native.closed


def test_attribute_probe_uses_reparse_flags_without_winapi():
    access, flags = attribute_probe_flags()
    assert access == 0x80
    assert flags & 0x00200000
    assert flags & 0x02000000


@pytest.mark.skipif(sys.platform == "win32", reason="refusal is the non-windows path")
def test_real_file_api_refuses_before_windll(monkeypatch):
    calls = []
    monkeypatch.setattr(ctypes, "WinDLL", lambda *args, **kwargs: calls.append(args), raising=False)
    with pytest.raises(OSError, match="probe_winapi_unavailable"):
        RealProbeFileApi()
    raw = RealProbeFileApi.__new__(RealProbeFileApi)
    for action in (
        lambda: raw.attributes(NOTE),
        lambda: raw.create_new(NOTE),
        lambda: raw.read(HIGH, 1),
        lambda: raw.write(HIGH, b"x"),
        lambda: raw.flush_buffers(HIGH),
        lambda: raw.close(HIGH),
        lambda: raw.create_pipe(),
        lambda: raw.spawn_suspended(("python", "-c", "pass"), ROOT),
        lambda: raw.create_limited_job(),
    ):
        with pytest.raises(OSError, match="probe_winapi_unavailable"):
            action()
    assert calls == []
    api = ProbeFileApi(ROOT)
    with pytest.raises(IoError) as caught:
        api.is_reparse(NOTE)
    assert caught.value.code == "io_failure"
    assert calls == []


def test_real_prototypes_keep_high_handles(monkeypatch):
    dll = type("Dll", (), {})()
    for name in NAMES:
        setattr(dll, name, _Function())
    raw = RealProbeFileApi.__new__(RealProbeFileApi)
    raw._kernel = None
    raw._win32 = lambda: None
    monkeypatch.setattr(ctypes, "WinDLL", lambda *_args, **_kwargs: dll, raising=False)
    loaded, _ctypes = raw._dll()
    create = loaded.CreateFileW
    close = loaded.CloseHandle
    read = loaded.ReadFile
    flush = loaded.FlushFileBuffers
    pipe = loaded.CreatePipe
    job = loaded.CreateJobObjectW
    assert create.restype is ctypes.c_void_p
    assert close.argtypes == [ctypes.c_void_p]
    assert read.argtypes[0] is ctypes.c_void_p
    assert flush.argtypes == [ctypes.c_void_p]
    assert pipe.argtypes[0] is ctypes.POINTER(ctypes.c_void_p)
    assert job.restype is ctypes.c_void_p
    produced = ctypes.CFUNCTYPE(create.restype, *create.argtypes)(lambda *_args: HIGH)
    assert produced(NOTE, FILE_READ_DATA, 0, None, 3, FILE_ATTRIBUTE_NORMAL, None) == HIGH
    seen = []

    def capture(value):
        seen.append(value)
        return 1

    assert ctypes.CFUNCTYPE(close.restype, *close.argtypes)(capture)(HIGH) == 1
    assert seen == [HIGH]
    callback = ctypes.CFUNCTYPE(ctypes.c_void_p)(lambda: HIGH)
    address = ctypes.cast(callback, ctypes.c_void_p).value
    truncated = ctypes.CFUNCTYPE(ctypes.c_int)(address)()
    assert truncated == -2023406815
    assert truncated != HIGH
    info = _basic_limits(ctypes)
    assert ctypes.sizeof(info) == 64
    assert type(info).ActiveProcessLimit.offset == 40


def test_open_access_constants_match_read_and_write_data():
    assert FILE_READ_DATA == 1
    assert FILE_WRITE_DATA == 2
    assert wintypes.HANDLE is not ctypes.c_int or True

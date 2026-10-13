"""Windows hashing uses one native metadata API and rejects actual mutations."""

import ctypes
import hashlib
import os
import sys
from types import SimpleNamespace

import pytest

from inferyard.platforms import engine_fit as identity
from inferyard.platforms import engine_fit_windows_files as files
from inferyard.platforms.identity import PreflightError

CONTENT = b"synthetic bytes, never executed"
STAMP = (2**64 - 1, bytes(range(16)), 32, len(CONTENT), 100, 200, 300)


@pytest.fixture
def hashed(tmp_path, monkeypatch):
    path = tmp_path / "synthetic.exe"
    path.write_bytes(CONTENT)
    box = {"path_reads": 0, "fd_reads": 0, "path_change": None, "fd_change": None}

    def read(kind):
        box[f"{kind}_reads"] += 1
        change = box[f"{kind}_change"]
        stamp = list(STAMP)
        if change is not None and box[f"{kind}_reads"] > 1:
            stamp[change] = b"different id" if change == 1 else stamp[change] + 1
        return tuple(stamp)

    monkeypatch.setattr(files, "path_stamp", lambda _: read("path"))
    monkeypatch.setattr(files, "descriptor_stamp", lambda _: read("fd"))
    monkeypatch.setattr(files, "open_file", lambda value, flags: os.open(value, flags))
    return path, box


def test_stable_executable_does_not_mix_python_stat_and_fstat(hashed, monkeypatch):
    def forbidden(_):
        raise AssertionError("Python fstat has different Windows ctime/mode semantics")

    monkeypatch.setattr(files.os, "fstat", forbidden)
    assert files.file_hash(hashed[0]) == (hashlib.sha256(CONTENT).hexdigest(), len(CONTENT))


def test_shared_hash_dispatches_to_native_windows_metadata(hashed, monkeypatch):
    monkeypatch.setattr(identity, "os", SimpleNamespace(name="nt"))
    assert identity._file_hash(hashed[0]) == (hashlib.sha256(CONTENT).hexdigest(), len(CONTENT))


@pytest.mark.parametrize("kind", ["path", "fd"])
@pytest.mark.parametrize("index", range(7))
def test_native_identity_attributes_size_and_all_change_times_are_checked(hashed, kind, index):
    path, box = hashed
    box[f"{kind}_change"] = index
    with pytest.raises(PreflightError, match="file_changed"):
        files.file_hash(path)


def test_replacement_between_path_query_and_hash_open_is_rejected(hashed, monkeypatch):
    monkeypatch.setattr(files, "descriptor_stamp", lambda _: (0, *STAMP[1:]))
    with pytest.raises(PreflightError, match="file_changed"):
        files.file_hash(hashed[0])


def test_private_query_descriptor_is_closed_on_native_failure(tmp_path, monkeypatch):
    path = tmp_path / "file"
    path.write_bytes(b"x")
    descriptor = os.open(path, os.O_RDONLY)
    monkeypatch.setattr(files, "open_file", lambda *_: descriptor)

    def failed(_):
        raise OSError("native_query_unavailable")

    monkeypatch.setattr(files, "descriptor_stamp", failed)
    with pytest.raises(OSError, match="native_query_unavailable"):
        files.path_stamp(path)
    with pytest.raises(OSError):
        os.fstat(descriptor)


@pytest.fixture
def api(monkeypatch):
    box = {"type": 1, "attributes": 32, "pending": 0, "directory": 0, "fail": None, "access": 9}
    structures = {0: files._BasicInfo, 1: files._StandardInfo, 18: files._IdInfo}

    def query(handle, kind, pointer, size):
        assert handle == 42
        structure = structures[kind]
        assert size == ctypes.sizeof(structure)
        info = ctypes.cast(pointer, ctypes.POINTER(structure)).contents
        if kind == 0:
            info.creation, info.access, info.write, info.change = 100, box["access"], 200, 300
            info.attributes = box["attributes"]
        elif kind == 1:
            info.size, info.delete_pending, info.directory = (
                len(CONTENT),
                box["pending"],
                box["directory"],
            )
        else:
            info.volume = 2**64 - 1
            info.identifier[:] = range(16)
        return kind != box["fail"]

    def function(_library, name, _arguments, _result=None):
        return (lambda _: box["type"]) if name == "GetFileType" else query

    monkeypatch.setitem(sys.modules, "msvcrt", SimpleNamespace(get_osfhandle=lambda _: 42))
    monkeypatch.setattr(files, "_kernel", lambda: None)
    monkeypatch.setattr(files, "_function", function)
    monkeypatch.setattr(files, "_error", lambda: OSError("native_query_unavailable"))
    return box


def test_native_abi_sizes_and_full_unsigned_identity(api):
    assert [
        ctypes.sizeof(kind) for kind in (files._BasicInfo, files._StandardInfo, files._IdInfo)
    ] == [
        40,
        24,
        24,
    ]
    assert files.descriptor_stamp(7) == STAMP


def test_access_time_is_excluded_but_change_time_is_retained(api):
    before = files.descriptor_stamp(7)
    api["access"] += 1
    assert files.descriptor_stamp(7) == before
    assert before[-1] == 300


@pytest.mark.parametrize("kind", [0, 1, 18])
def test_native_query_failure_has_no_truncated_or_creation_time_fallback(api, kind):
    api["fail"] = kind
    with pytest.raises(OSError, match="native_query_unavailable"):
        files.descriptor_stamp(7)


@pytest.mark.parametrize(
    ("field", "value", "reason"),
    [
        ("type", 2, "not_regular_file"),
        ("attributes", 0x400, "not_regular_file"),
        ("attributes", 0x10, "not_regular_file"),
        ("directory", 1, "not_regular_file"),
        ("pending", 1, "file_changed"),
    ],
)
def test_nonregular_reparse_and_delete_pending_files_are_rejected(api, field, value, reason):
    api[field] = value
    with pytest.raises(PreflightError, match=reason):
        files.descriptor_stamp(7)
